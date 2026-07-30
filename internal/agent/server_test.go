package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/engine"
)

func TestServerInitializationInventoryAndShutdown(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	server := &Server{
		Conn: NewConn(serverSide, serverSide), Runs: NewRunRegistry(),
		Build:      BuildInfo{Version: "1.2.3", Commit: "abc", Date: "today"},
		WorkingDir: "/project",
	}
	client := NewConn(clientSide, clientSide)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(ctx) }()
	clientErr := make(chan error, 1)
	go func() { clientErr <- client.Serve(ctx, nil) }()

	var beforeInit map[string]any
	err := client.Call(ctx, "run.cancel", map[string]string{"run_id": "missing"}, &beforeInit)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeNotInitialized {
		t.Fatalf("expected not-initialized error, got %T %v", err, err)
	}
	var initialized initializeResult
	if err := client.Call(ctx, "session.initialize", initializeParams{ProtocolVersion: ProtocolVersion}, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.ProtocolVersion != ProtocolVersion || initialized.Build.Version != "1.2.3" || len(initialized.Methods) != len(protocolMethods) {
		t.Fatalf("unexpected initialize result: %+v", initialized)
	}
	var shutdown map[string]bool
	if err := client.Call(ctx, "session.shutdown", nil, &shutdown); err != nil {
		t.Fatal(err)
	}
	if !shutdown["shutdown"] {
		t.Fatalf("unexpected shutdown result: %+v", shutdown)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server shutdown: %v", err)
	}
	_ = clientSide.Close()
	<-clientErr
}

func TestServerRunLifecycleEmitsExactlyOneFinishedNotification(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	server := &Server{Conn: NewConn(serverSide, serverSide), Runs: NewRunRegistry(), WorkingDir: "/project"}
	release := make(chan struct{})
	server.ExtraMethods = map[string]Handler{
		"fixture.run": func(string, json.RawMessage) (any, *RPCError) {
			runID, err := server.StartRun(func(ctx context.Context, _ string) (any, error, int) {
				select {
				case <-release:
					return map[string]bool{"ok": true}, nil, 0
				case <-ctx.Done():
					return nil, ctx.Err(), 130
				}
			})
			if err != nil {
				return nil, NewRPCError(CodeInternalError, err.Error(), nil)
			}
			return map[string]string{"run_id": runID}, nil
		},
	}
	client := NewConn(clientSide, clientSide)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx) }()
	finished := make(chan runFinishedParams, 2)
	go func() {
		_ = client.Serve(ctx, func(method string, params json.RawMessage) (any, *RPCError) {
			if method == "run.finished" {
				var event runFinishedParams
				if err := json.Unmarshal(params, &event); err != nil {
					t.Errorf("decode run.finished: %v", err)
				} else {
					finished <- event
				}
			}
			return nil, nil
		})
	}()
	if err := client.Call(ctx, "session.initialize", initializeParams{ProtocolVersion: ProtocolVersion}, nil); err != nil {
		t.Fatal(err)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	if err := client.Call(ctx, "fixture.run", nil, &started); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case event := <-finished:
		if event.RunID != started.RunID || event.Error != "" || event.ExitCode != 0 {
			t.Fatalf("unexpected terminal event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing run.finished")
	}
	if server.Runs.Finish(started.RunID) {
		t.Fatal("completed run remained registered")
	}
	select {
	case duplicate := <-finished:
		t.Fatalf("duplicate terminal event: %+v", duplicate)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestServerRunsProceedIndependently(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := &Server{
		Conn: NewConn(serverSide, serverSide), Runs: NewRunRegistry(),
		runContext: ctx,
	}
	client := NewConn(clientSide, clientSide)
	finished := make(chan runFinishedParams, 2)
	go func() {
		_ = client.Serve(ctx, func(method string, params json.RawMessage) (any, *RPCError) {
			if method == "run.finished" {
				var event runFinishedParams
				if err := json.Unmarshal(params, &event); err != nil {
					t.Errorf("decode run.finished: %v", err)
				} else {
					finished <- event
				}
			}
			return nil, nil
		})
	}()

	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	firstID, err := server.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		select {
		case <-firstRelease:
			return "first", nil, 0
		case <-ctx.Done():
			return nil, ctx.Err(), 130
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := server.StartRun(func(ctx context.Context, _ string) (any, error, int) {
		select {
		case <-secondRelease:
			return "second", nil, 0
		case <-ctx.Done():
			return nil, ctx.Err(), 130
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstID == secondID || server.Runs.Len() != 2 {
		t.Fatalf("runs were not registered independently: %q %q len=%d", firstID, secondID, server.Runs.Len())
	}

	close(secondRelease)
	select {
	case event := <-finished:
		if event.RunID != secondID || event.Result != "second" {
			t.Fatalf("first completion was not independent: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("second run did not finish")
	}
	if server.Runs.Len() != 1 {
		t.Fatalf("first run should remain active, len=%d", server.Runs.Len())
	}
	close(firstRelease)
	select {
	case event := <-finished:
		if event.RunID != firstID || event.Result != "first" {
			t.Fatalf("unexpected first completion: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("first run did not finish")
	}
}

func TestServerCancelRunEmitsCanceledTerminalNotification(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	server := &Server{Conn: NewConn(serverSide, serverSide), Runs: NewRunRegistry()}
	server.ExtraMethods = map[string]Handler{
		"fixture.run": func(string, json.RawMessage) (any, *RPCError) {
			runID, err := server.StartRun(func(ctx context.Context, _ string) (any, error, int) {
				<-ctx.Done()
				return nil, ctx.Err(), 130
			})
			if err != nil {
				return nil, NewRPCError(CodeInternalError, err.Error(), nil)
			}
			return map[string]string{"run_id": runID}, nil
		},
	}
	client := NewConn(clientSide, clientSide)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx) }()
	finished := make(chan runFinishedParams, 1)
	go func() {
		_ = client.Serve(ctx, func(method string, params json.RawMessage) (any, *RPCError) {
			if method == "run.finished" {
				var event runFinishedParams
				_ = json.Unmarshal(params, &event)
				finished <- event
			}
			return nil, nil
		})
	}()
	if err := client.Call(ctx, "session.initialize", initializeParams{ProtocolVersion: ProtocolVersion}, nil); err != nil {
		t.Fatal(err)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	if err := client.Call(ctx, "fixture.run", nil, &started); err != nil {
		t.Fatal(err)
	}
	if err := client.Call(ctx, "run.cancel", map[string]string{"run_id": started.RunID}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-finished:
		if event.RunID != started.RunID || event.Error != context.Canceled.Error() || event.ExitCode != 130 {
			t.Fatalf("unexpected canceled event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing canceled run.finished")
	}
}

func TestScriptedClientRunInteractsCancelsAndShutsDown(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()
	server := &Server{Conn: NewConn(serverSide, serverSide), Runs: NewRunRegistry(), WorkingDir: "/project"}
	interactionDone := make(chan struct{})
	server.ExtraMethods = map[string]Handler{
		"fixture.run": func(string, json.RawMessage) (any, *RPCError) {
			runID, err := server.StartRun(func(ctx context.Context, activeRunID string) (any, error, int) {
				window := engine.PlanWindowFirst
				interaction := &Interaction{
					Conn: server.Conn, Runs: server.Runs,
					PlanWindow: func(string) engine.PlanWindow { return window },
				}
				interaction.RunID = activeRunID
				rows := []engine.PlanRow{{Index: 1, RemoteID: "one", Title: "One", Toggleable: true}}
				selection, err := interaction.SelectRows("source-a", rows)
				if err != nil {
					return nil, err, 1
				}
				if selection.Rebuild {
					window = selection.Window
					selection, err = interaction.SelectRows("source-a", rows)
					if err != nil {
						return nil, err, 1
					}
				}
				confirmed, err := interaction.Confirm("[source-a] Continue?", false)
				if err != nil || !confirmed {
					return nil, err, 1
				}
				close(interactionDone)
				<-ctx.Done()
				return selection.Manifest, ctx.Err(), 130
			})
			if err != nil {
				return nil, NewRPCError(CodeInternalError, err.Error(), nil)
			}
			return map[string]string{"run_id": runID}, nil
		},
	}
	client := NewConn(clientSide, clientSide)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Serve(ctx) }()
	finished := make(chan runFinishedParams, 1)
	selectCalls := 0
	go func() {
		_ = client.Serve(ctx, func(method string, params json.RawMessage) (any, *RPCError) {
			switch method {
			case "ui.selectRows":
				var request selectRowsParams
				_ = json.Unmarshal(params, &request)
				selectCalls++
				if selectCalls == 1 {
					return selectRowsResult{Rebuild: true, PlanWindow: engine.PlanWindowLatest}, nil
				}
				if request.PlanWindow != engine.PlanWindowLatest {
					t.Errorf("rebuilt request lost plan window: %+v", request)
				}
				return selectRowsResult{SelectedIndices: []int{1}, PlanWindow: engine.PlanWindowLatest}, nil
			case "ui.confirm":
				return confirmResult{Confirmed: true}, nil
			case "run.finished":
				var event runFinishedParams
				_ = json.Unmarshal(params, &event)
				finished <- event
			}
			return nil, nil
		})
	}()
	if err := client.Call(ctx, "session.initialize", initializeParams{ProtocolVersion: ProtocolVersion}, nil); err != nil {
		t.Fatal(err)
	}
	var started struct {
		RunID string `json:"run_id"`
	}
	if err := client.Call(ctx, "fixture.run", nil, &started); err != nil {
		t.Fatal(err)
	}
	<-interactionDone
	if err := client.Call(ctx, "run.cancel", map[string]string{"run_id": started.RunID}, nil); err != nil {
		t.Fatal(err)
	}
	event := <-finished
	if event.RunID != started.RunID || event.ExitCode != 130 {
		t.Fatalf("unexpected terminal event: %+v", event)
	}
	if err := client.Call(ctx, "session.shutdown", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := <-serverErr; err != nil {
		t.Fatal(err)
	}
}

func TestServerRejectsUnsupportedProtocol(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	server := &Server{Conn: NewConn(serverSide, serverSide)}
	client := NewConn(clientSide, clientSide)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx) }()
	go func() { _ = client.Serve(ctx, nil) }()
	err := client.Call(ctx, "session.initialize", initializeParams{ProtocolVersion: 99}, nil)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != CodeInvalidParams {
		t.Fatalf("expected unsupported-version error, got %T %v", err, err)
	}
}

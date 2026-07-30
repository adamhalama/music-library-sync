package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/engine"
)

func TestInteractionRoundTripsAndValidatesSelection(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	server := NewConn(serverSide, serverSide)
	client := NewConn(clientSide, clientSide)
	registry := NewRunRegistry()
	runID, _, err := registry.Register(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx, nil) }()
	go func() {
		_ = client.Serve(ctx, func(method string, params json.RawMessage) (any, *RPCError) {
			switch method {
			case "ui.confirm":
				got, err := decodeParams[confirmParams](params)
				if err != nil || got.RunID != runID || got.SourceID != "source-a" || !got.DefaultYes {
					t.Errorf("unexpected confirm params: %+v err=%v", got, err)
				}
				return confirmResult{Confirmed: true}, nil
			case "ui.input":
				got, err := decodeParams[inputParams](params)
				if err != nil || !got.Mask || got.SourceID != "source-a" {
					t.Errorf("unexpected input params: %+v err=%v", got, err)
				}
				return inputResult{Value: "secret"}, nil
			case "ui.selectRows":
				got, err := decodeParams[selectRowsParams](params)
				if err != nil || got.Details.TargetDir != "/music" || got.PlanWindow != engine.PlanWindowLatest {
					t.Errorf("unexpected selection params: %+v err=%v", got, err)
				}
				return selectRowsResult{
					SelectedIndices: []int{2},
					DownloadOrder:   engine.DownloadOrderOldestFirst,
					PlanWindow:      engine.PlanWindowLatest,
				}, nil
			default:
				return nil, NewRPCError(CodeMethodNotFound, "method not found", nil)
			}
		})
	}()
	interaction := &Interaction{
		Conn: server, Runs: registry, RunID: runID,
		SourceDetails: func(string) app.PlanSourceDetails {
			return app.PlanSourceDetails{SourceID: "source-a", TargetDir: "/music"}
		},
		PlanWindow: func(string) engine.PlanWindow { return engine.PlanWindowLatest },
	}
	confirmed, err := interaction.Confirm("[source-a] Continue?", true)
	if err != nil || !confirmed {
		t.Fatalf("confirm: confirmed=%v err=%v", confirmed, err)
	}
	value, err := interaction.Input("[source-a] Enter ARL")
	if err != nil || value != "secret" {
		t.Fatalf("input: value=%q err=%v", value, err)
	}
	result, err := interaction.SelectRows("source-a", []engine.PlanRow{
		{Index: 1, RemoteID: "one", Title: "One", Toggleable: true},
		{Index: 2, RemoteID: "two", Title: "Two", Toggleable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Manifest.SelectedIndices) != 1 || result.Manifest.SelectedIndices[0] != 2 {
		t.Fatalf("unexpected manifest: %+v", result.Manifest)
	}
	if result.Manifest.DownloadOrder != engine.DownloadOrderOldestFirst {
		t.Fatalf("download order was not returned: %+v", result.Manifest)
	}
}

func TestInteractionPlanWindowRebuild(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	server := NewConn(serverSide, serverSide)
	client := NewConn(clientSide, clientSide)
	registry := NewRunRegistry()
	runID, _, _ := registry.Register(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx, nil) }()
	selectCalls := 0
	go func() {
		_ = client.Serve(ctx, func(method string, _ json.RawMessage) (any, *RPCError) {
			switch method {
			case "ui.selectRows":
				selectCalls++
				if selectCalls == 1 {
					return selectRowsResult{Rebuild: true, PlanWindow: engine.PlanWindowLatest}, nil
				}
				return selectRowsResult{SelectedIndices: []int{1}, PlanWindow: engine.PlanWindowLatest}, nil
			case "ui.confirm":
				return confirmResult{Confirmed: true}, nil
			default:
				return nil, NewRPCError(CodeMethodNotFound, "method not found", nil)
			}
		})
	}()
	window := engine.PlanWindowFirst
	interaction := &Interaction{
		Conn: server, Runs: registry, RunID: runID,
		PlanWindow: func(string) engine.PlanWindow { return window },
	}
	rows := []engine.PlanRow{{Index: 1, Toggleable: true}}
	result, err := interaction.SelectRows("source-a", rows)
	if err != nil || !result.Rebuild || result.Window != engine.PlanWindowLatest {
		t.Fatalf("unexpected rebuild result: %+v err=%v", result, err)
	}
	window = result.Window
	result, err = interaction.SelectRows("source-a", rows)
	if err != nil || result.Rebuild || len(result.Manifest.SelectedIndices) != 1 || result.Manifest.SelectedIndices[0] != 1 {
		t.Fatalf("unexpected rebuilt selection: %+v err=%v", result, err)
	}
	confirmed, err := interaction.Confirm("[source-a] Continue?", false)
	if err != nil || !confirmed {
		t.Fatalf("confirm after rebuild: confirmed=%v err=%v", confirmed, err)
	}
}

func TestInteractionCancellationUnblocksPendingCallBeforeRunContext(t *testing.T) {
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()
	server := NewConn(serverSide, serverSide)
	client := NewConn(clientSide, clientSide)
	registry := NewRunRegistry()
	runID, runCtx, _ := registry.Register(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx, nil) }()
	requestSeen := make(chan struct{})
	block := make(chan struct{})
	go func() {
		_ = client.Serve(ctx, func(string, json.RawMessage) (any, *RPCError) {
			close(requestSeen)
			<-block
			return confirmResult{Canceled: true}, nil
		})
	}()
	interaction := &Interaction{Conn: server, Runs: registry, RunID: runID}
	errs := make(chan error, 1)
	go func() {
		_, err := interaction.Confirm("[source-a] Continue?", false)
		errs <- err
	}()
	<-requestSeen
	if !registry.Cancel(runID) {
		t.Fatal("expected cancellation")
	}
	select {
	case err := <-errs:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected canceled interaction, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending interaction did not unblock")
	}
	if !errors.Is(runCtx.Err(), context.Canceled) {
		t.Fatalf("run context not canceled: %v", runCtx.Err())
	}
	close(block)
}

func TestInteractionKindsFailWhenClientDisconnects(t *testing.T) {
	tests := []struct {
		name string
		call func(*Interaction) error
	}{
		{"confirm", func(i *Interaction) error { _, err := i.Confirm("[s] Confirm?", false); return err }},
		{"input", func(i *Interaction) error { _, err := i.Input("[s] Input"); return err }},
		{"select rows", func(i *Interaction) error {
			_, err := i.SelectRows("s", []engine.PlanRow{{Index: 1, Toggleable: true}})
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverSide, clientSide := net.Pipe()
			server := NewConn(serverSide, serverSide)
			client := NewConn(clientSide, clientSide)
			registry := NewRunRegistry()
			runID, _, _ := registry.Register(context.Background())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() { _ = server.Serve(ctx, nil) }()
			go func() {
				_ = client.Serve(ctx, func(string, json.RawMessage) (any, *RPCError) {
					_ = clientSide.Close()
					return nil, nil
				})
			}()
			err := tt.call(&Interaction{Conn: server, Runs: registry, RunID: runID})
			var rpcErr *RPCError
			if !errors.As(err, &rpcErr) || rpcErr.Code != CodeDisconnected {
				t.Fatalf("expected disconnect error, got %T %v", err, err)
			}
			_ = serverSide.Close()
		})
	}
}

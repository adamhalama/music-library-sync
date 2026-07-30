package agent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

type blockingSyncAdapter struct{}

func (blockingSyncAdapter) Kind() string                       { return "scdl" }
func (blockingSyncAdapter) Binary() string                     { return "blocking-adapter" }
func (blockingSyncAdapter) MinVersion() string                 { return "" }
func (blockingSyncAdapter) Validate(config.Source) error       { return nil }
func (blockingSyncAdapter) RequiredEnv(config.Source) []string { return nil }
func (blockingSyncAdapter) BuildExecSpec(config.Source, config.Defaults, time.Duration) (engine.ExecSpec, error) {
	return engine.ExecSpec{Bin: "blocking-adapter"}, nil
}

type blockingSyncRunner struct {
	started chan struct{}
}

func (r *blockingSyncRunner) Run(ctx context.Context, _ engine.ExecSpec) engine.ExecResult {
	select {
	case r.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return engine.ExecResult{ExitCode: 130, Interrupted: true, Err: ctx.Err()}
}

func TestSyncStartCancellationInterruptsAdapterAndKeepsProtocolStructured(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "udl.yaml")
	for _, name := range []string{"music", "state"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	payload := `version: 1
defaults:
  state_dir: ` + filepath.Join(dir, "state") + `
sources:
  - id: source-a
    type: soundcloud
    enabled: true
    target_dir: ` + filepath.Join(dir, "music") + `
    url: https://soundcloud.com/user/likes
    state_file: source-a.state
    adapter:
      kind: scdl
`
	if err := os.WriteFile(configPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()
	runner := &blockingSyncRunner{started: make(chan struct{}, 1)}
	server := &Server{
		Conn: NewConn(serverSide, serverSide), Runs: NewRunRegistry(),
		WorkingDir: dir, ConfigPath: configPath, ErrOut: io.Discard,
		SyncRegistry: map[string]engine.Adapter{"scdl": blockingSyncAdapter{}},
		SyncRunner:   runner,
	}
	client := NewConn(clientSide, clientSide)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = server.Serve(ctx) }()
	finished := make(chan runFinishedParams, 1)
	events := make(chan syncEventParams, 16)
	go func() {
		_ = client.Serve(ctx, func(method string, params json.RawMessage) (any, *RPCError) {
			switch method {
			case "sync.event":
				var event syncEventParams
				if err := json.Unmarshal(params, &event); err != nil {
					t.Errorf("decode sync event: %v", err)
				} else {
					events <- event
				}
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
	if err := client.Call(ctx, "sync.start", syncStartParams{
		SourceIDs: []string{"source-a"}, NoPreflight: true,
	}, &started); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case terminal := <-finished:
		time.Sleep(20 * time.Millisecond)
		messages := []string{}
		for {
			select {
			case event := <-events:
				messages = append(messages, event.Event.Message)
			default:
				t.Fatalf("run finished before adapter execution: %+v events=%v", terminal, messages)
			}
		}
	case <-ctx.Done():
		t.Fatalf("adapter execution did not start; queued events=%d", len(events))
	}
	sawStructuredEvent := false
	select {
	case event := <-events:
		sawStructuredEvent = event.RunID == started.RunID && event.Event.Event != ""
	case <-ctx.Done():
		t.Fatal("missing structured sync.event notification")
	}
	if err := client.Call(ctx, "sync.cancel", map[string]string{"run_id": started.RunID}, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case terminal := <-finished:
		if terminal.RunID != started.RunID || terminal.ExitCode != 130 {
			t.Fatalf("unexpected terminal result: %+v", terminal)
		}
		if terminal.Error == "" {
			t.Fatal("expected interrupted terminal error")
		}
	case <-ctx.Done():
		t.Fatal("missing run.finished after adapter cancellation")
	}
	for {
		select {
		case event := <-events:
			if event.RunID == started.RunID && event.Event.Event != "" {
				sawStructuredEvent = true
			}
		default:
			if !sawStructuredEvent {
				t.Fatal("expected structured sync.event notification")
			}
			return
		}
	}
}

func TestSyncStartRejectsInvalidConfigBeforeRegisteringRun(t *testing.T) {
	server := &Server{
		Conn: NewConn(nil, io.Discard), Runs: NewRunRegistry(),
		WorkingDir: t.TempDir(), ConfigPath: filepath.Join(t.TempDir(), "missing.yaml"),
	}
	_, rpcErr := server.startSync(json.RawMessage(`{}`))
	if rpcErr == nil || rpcErr.Code != CodeInvalidParams {
		t.Fatalf("expected invalid params, got %+v", rpcErr)
	}
	if server.Runs.Len() != 0 {
		t.Fatalf("invalid request registered a run")
	}
}

var _ engine.Adapter = blockingSyncAdapter{}
var _ engine.ExecRunner = (*blockingSyncRunner)(nil)

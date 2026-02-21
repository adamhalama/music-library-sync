package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

type fakeAdapter struct{}

func (a fakeAdapter) Kind() string                              { return "fake" }
func (a fakeAdapter) Binary() string                            { return "fakebin" }
func (a fakeAdapter) MinVersion() string                        { return "1.0.0" }
func (a fakeAdapter) RequiredEnv(source config.Source) []string { return nil }
func (a fakeAdapter) Validate(source config.Source) error       { return nil }
func (a fakeAdapter) BuildExecSpec(source config.Source, defaults config.Defaults, timeout time.Duration) (ExecSpec, error) {
	return ExecSpec{
		Bin:            "fakebin",
		Args:           []string{"run", source.ID},
		Dir:            source.TargetDir,
		Timeout:        timeout,
		DisplayCommand: "fakebin run " + source.ID,
	}, nil
}

type noOpRunner struct{}

func (noOpRunner) Run(ctx context.Context, spec ExecSpec) ExecResult {
	return ExecResult{ExitCode: 0}
}

type interruptedRunnerWithArtifacts struct{}

func (interruptedRunnerWithArtifacts) Run(ctx context.Context, spec ExecSpec) ExecResult {
	_ = os.WriteFile(filepath.Join(spec.Dir, "track.m4a.part"), []byte("partial"), 0o644)
	_ = os.WriteFile(filepath.Join(spec.Dir, "track.m4a.ytdl"), []byte("state"), 0o644)
	_ = os.WriteFile(filepath.Join(spec.Dir, "123456.scdl.lock"), []byte("lock"), 0o644)
	return ExecResult{ExitCode: 130, Interrupted: true}
}

func TestSyncerDryRunDeterministicJSON(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:              stateDir,
			ArchiveFile:           "archive.txt",
			Threads:               1,
			ContinueOnError:       true,
			CommandTimeoutSeconds: 900,
		},
		Sources: []config.Source{
			{
				ID:        "fake-source",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://example.com",
				StateFile: "fake.sync.spotdl",
				Adapter:   config.AdapterSpec{Kind: "fake"},
			},
		},
	}

	buf := &bytes.Buffer{}
	emitter := output.NewJSONEmitter(buf)
	syncer := NewSyncer(map[string]Adapter{"fake": fakeAdapter{}}, noOpRunner{}, emitter)
	fixedTime := time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC)
	syncer.Now = func() time.Time { return fixedTime }

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("unexpected sync result: %+v", result)
	}

	if !bytes.Contains(buf.Bytes(), []byte(`"event":"sync_started"`)) {
		t.Fatalf("expected sync_started event, got %s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"event":"sync_finished"`)) {
		t.Fatalf("expected sync_finished event, got %s", buf.String())
	}
}

func TestSyncerInterruptedCleansNewPartialArtifacts(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	preexistingPart := filepath.Join(targetDir, "preexisting.part")
	if err := os.WriteFile(preexistingPart, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write preexisting part: %v", err)
	}

	cfg := config.Config{
		Version: 1,
		Defaults: config.Defaults{
			StateDir:              filepath.Join(tmp, "state"),
			ArchiveFile:           "archive.txt",
			Threads:               1,
			ContinueOnError:       true,
			CommandTimeoutSeconds: 900,
		},
		Sources: []config.Source{
			{
				ID:        "sc-source",
				Type:      config.SourceTypeSoundCloud,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://soundcloud.com/user",
				Adapter:   config.AdapterSpec{Kind: "scdl"},
			},
		},
	}

	buf := &bytes.Buffer{}
	syncer := NewSyncer(
		map[string]Adapter{"scdl": fakeAdapter{}},
		interruptedRunnerWithArtifacts{},
		output.NewHumanEmitter(buf, buf, false, true),
	)
	_, err := syncer.Sync(context.Background(), cfg, SyncOptions{})
	if err == nil || err != ErrInterrupted {
		t.Fatalf("expected ErrInterrupted, got %v", err)
	}

	if _, statErr := os.Stat(filepath.Join(targetDir, "track.m4a.part")); !os.IsNotExist(statErr) {
		t.Fatalf("expected track.m4a.part to be removed, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(targetDir, "track.m4a.ytdl")); !os.IsNotExist(statErr) {
		t.Fatalf("expected track.m4a.ytdl to be removed, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(targetDir, "123456.scdl.lock")); !os.IsNotExist(statErr) {
		t.Fatalf("expected .scdl.lock to be removed, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(preexistingPart); statErr != nil {
		t.Fatalf("expected preexisting artifact to be preserved, stat err=%v", statErr)
	}
	if !strings.Contains(buf.String(), "cleaned") {
		t.Fatalf("expected cleanup message in output, got %s", buf.String())
	}
}

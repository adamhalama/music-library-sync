package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/auth"
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
	_ = os.WriteFile(filepath.Join(spec.Dir, "track.jpg"), []byte("thumb"), 0o644)
	return ExecResult{ExitCode: 130, Interrupted: true}
}

type sequenceRunner struct {
	results []ExecResult
	specs   []ExecSpec
}

func (r *sequenceRunner) Run(ctx context.Context, spec ExecSpec) ExecResult {
	r.specs = append(r.specs, spec)
	if len(r.results) == 0 {
		return ExecResult{ExitCode: 0}
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result
}

type cacheInspectRunner struct {
	t             *testing.T
	expectSnippet string
	specs         []ExecSpec
}

func (r *cacheInspectRunner) Run(ctx context.Context, spec ExecSpec) ExecResult {
	r.specs = append(r.specs, spec)
	cachePath := filepath.Join(spec.Dir, "config", "spotify", "cache.json")
	payload, err := os.ReadFile(cachePath)
	if err != nil {
		r.t.Fatalf("read primed cache %s: %v", cachePath, err)
	}
	if !strings.Contains(string(payload), r.expectSnippet) {
		r.t.Fatalf("expected cache %s to include %q, got %q", cachePath, r.expectSnippet, string(payload))
	}
	return ExecResult{ExitCode: 0}
}

type fakeSpotifyAdapter struct{}

func (a fakeSpotifyAdapter) Kind() string                              { return "spotdl" }
func (a fakeSpotifyAdapter) Binary() string                            { return "spotdl" }
func (a fakeSpotifyAdapter) MinVersion() string                        { return "4.0.0" }
func (a fakeSpotifyAdapter) RequiredEnv(source config.Source) []string { return nil }
func (a fakeSpotifyAdapter) Validate(source config.Source) error       { return nil }
func (a fakeSpotifyAdapter) BuildExecSpec(source config.Source, defaults config.Defaults, timeout time.Duration) (ExecSpec, error) {
	args := []string{"sync", source.URL}
	args = append(args, source.Adapter.ExtraArgs...)
	return ExecSpec{
		Bin:            "spotdl",
		Args:           args,
		Dir:            source.TargetDir,
		Timeout:        timeout,
		DisplayCommand: "spotdl " + strings.Join(args, " "),
	}, nil
}

type fakeDeemixAdapter struct{}

func (a fakeDeemixAdapter) Kind() string                              { return "deemix" }
func (a fakeDeemixAdapter) Binary() string                            { return "deemix" }
func (a fakeDeemixAdapter) MinVersion() string                        { return "0.1.0" }
func (a fakeDeemixAdapter) RequiredEnv(source config.Source) []string { return nil }
func (a fakeDeemixAdapter) Validate(source config.Source) error       { return nil }
func (a fakeDeemixAdapter) BuildExecSpec(source config.Source, defaults config.Defaults, timeout time.Duration) (ExecSpec, error) {
	args := []string{source.URL}
	return ExecSpec{
		Bin:            "deemix",
		Args:           args,
		Dir:            source.TargetDir,
		Timeout:        timeout,
		DisplayCommand: "deemix " + strings.Join(args, " "),
	}, nil
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
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	preexistingPart := filepath.Join(targetDir, "preexisting.part")
	if err := os.WriteFile(preexistingPart, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write preexisting part: %v", err)
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
				ID:        "sc-source",
				Type:      config.SourceTypeSoundCloud,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://soundcloud.com/user",
				StateFile: "sc-source.sync.scdl",
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
	_, err := syncer.Sync(context.Background(), cfg, SyncOptions{NoPreflight: true})
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
	if _, statErr := os.Stat(filepath.Join(targetDir, "track.jpg")); !os.IsNotExist(statErr) {
		t.Fatalf("expected track.jpg to be removed, stat err=%v", statErr)
	}
	if _, statErr := os.Stat(preexistingPart); statErr != nil {
		t.Fatalf("expected preexisting artifact to be preserved, stat err=%v", statErr)
	}
	if !strings.Contains(buf.String(), "cleaned") {
		t.Fatalf("expected cleanup message in output, got %s", buf.String())
	}
}

func TestResolveAskOnExisting_UsesConfigWhenFlagUnset(t *testing.T) {
	source := config.Source{
		Sync: config.SyncPolicy{
			AskOnExisting: boolPtrSyncer(true),
		},
	}
	got := resolveAskOnExisting(source, SyncOptions{})
	if !got {
		t.Fatalf("expected config ask_on_existing to be respected when flag is unset")
	}
}

func TestResolveAskOnExisting_FlagOverridesConfig(t *testing.T) {
	source := config.Source{
		Sync: config.SyncPolicy{
			AskOnExisting: boolPtrSyncer(true),
		},
	}
	got := resolveAskOnExisting(source, SyncOptions{
		AskOnExisting:    false,
		AskOnExistingSet: true,
	})
	if got {
		t.Fatalf("expected explicit --ask-on-existing=false to override config true")
	}
}

func boolPtrSyncer(v bool) *bool {
	return &v
}

func TestIsGracefulBreakOnExistingStopRecognizesKnownMarker(t *testing.T) {
	source := config.Source{
		Type: config.SourceTypeSoundCloud,
		Sync: config.SyncPolicy{
			BreakOnExisting: boolPtrSyncer(true),
		},
	}
	execResult := ExecResult{
		ExitCode:   1,
		StderrTail: "yt_dlp.utils.ExistingVideoReached: Encountered a video that is already in the archive, stopping due to --break-on-existing",
	}

	if !isGracefulBreakOnExistingStop(source, nil, execResult) {
		t.Fatalf("expected graceful break-on-existing detection")
	}
}

func TestIsGracefulBreakOnExistingStopFalseWhenDisabled(t *testing.T) {
	source := config.Source{
		Type: config.SourceTypeSoundCloud,
		Sync: config.SyncPolicy{
			BreakOnExisting: boolPtrSyncer(false),
		},
	}
	execResult := ExecResult{
		ExitCode:   1,
		StderrTail: "yt_dlp.utils.ExistingVideoReached: ...",
	}

	if isGracefulBreakOnExistingStop(source, nil, execResult) {
		t.Fatalf("expected no graceful break detection when break mode is disabled")
	}
}

func TestSyncerRetriesSpotifyWithUserAuthWhenRequired(t *testing.T) {
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
				ID:        "spotify-source",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-source.sync.spotdl",
				Adapter:   config.AdapterSpec{Kind: "spotdl", ExtraArgs: []string{"--headless"}},
			},
		},
	}

	runner := &sequenceRunner{
		results: []ExecResult{
			{
				ExitCode:   1,
				StderrTail: "HTTP Error ... returned 401 due to Valid user authentication required",
			},
			{
				ExitCode: 0,
			},
		},
	}
	syncer := NewSyncer(map[string]Adapter{"spotdl": fakeSpotifyAdapter{}}, runner, output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true))

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{AllowPrompt: true})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("expected successful retry result, got %+v", result)
	}
	if len(runner.specs) != 2 {
		t.Fatalf("expected two runner calls, got %d", len(runner.specs))
	}
	if strings.Contains(strings.Join(runner.specs[0].Args, " "), "--user-auth") {
		t.Fatalf("did not expect first run to include --user-auth")
	}
	if !strings.Contains(strings.Join(runner.specs[1].Args, " "), "--user-auth") {
		t.Fatalf("expected retry run to include --user-auth, got %v", runner.specs[1].Args)
	}
}

func TestSyncerDoesNotRetrySpotifyAuthWithoutPrompt(t *testing.T) {
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
				ID:        "spotify-source",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-source.sync.spotdl",
				Adapter:   config.AdapterSpec{Kind: "spotdl", ExtraArgs: []string{"--headless"}},
			},
		},
	}

	runner := &sequenceRunner{
		results: []ExecResult{
			{
				ExitCode:   1,
				StderrTail: "HTTP Error ... returned 401 due to Valid user authentication required",
			},
		},
	}
	syncer := NewSyncer(map[string]Adapter{"spotdl": fakeSpotifyAdapter{}}, runner, output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true))

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{AllowPrompt: false})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("expected failed source without retry, got %+v", result)
	}
	if len(runner.specs) != 1 {
		t.Fatalf("expected one runner call, got %d", len(runner.specs))
	}
}

func TestSyncerSpotifyRetryDropsHeadlessWhenBrowserChosen(t *testing.T) {
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
				ID:        "spotify-source",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-source.sync.spotdl",
				Adapter:   config.AdapterSpec{Kind: "spotdl", ExtraArgs: []string{"--headless", "--print-errors"}},
			},
		},
	}

	runner := &sequenceRunner{
		results: []ExecResult{
			{
				ExitCode:   1,
				StderrTail: "HTTP Error ... returned 401 due to Valid user authentication required",
			},
			{
				ExitCode: 0,
			},
		},
	}
	syncer := NewSyncer(map[string]Adapter{"spotdl": fakeSpotifyAdapter{}}, runner, output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true))

	prompted := false
	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{
		AllowPrompt: true,
		PromptOnSpotifyAuth: func(sourceID string) (bool, error) {
			prompted = true
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("expected successful retry result, got %+v", result)
	}
	if !prompted {
		t.Fatalf("expected spotify auth callback to be called")
	}
	if len(runner.specs) != 2 {
		t.Fatalf("expected two runner calls, got %d", len(runner.specs))
	}
	retryArgs := runner.specs[1].Args
	retryJoined := strings.Join(retryArgs, " ")
	if !strings.Contains(retryJoined, "--user-auth") {
		t.Fatalf("expected --user-auth in retry args, got %v", retryArgs)
	}
	if strings.Contains(retryJoined, "--headless") {
		t.Fatalf("expected --headless to be removed when browser auth is chosen, got %v", retryArgs)
	}
}

func TestSyncerSpotifyRetryKeepsHeadlessWhenBrowserDeclined(t *testing.T) {
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
				ID:        "spotify-source",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-source.sync.spotdl",
				Adapter:   config.AdapterSpec{Kind: "spotdl", ExtraArgs: []string{"--headless", "--print-errors"}},
			},
		},
	}

	runner := &sequenceRunner{
		results: []ExecResult{
			{
				ExitCode:   1,
				StderrTail: "HTTP Error ... returned 401 due to Valid user authentication required",
			},
			{
				ExitCode: 0,
			},
		},
	}
	syncer := NewSyncer(map[string]Adapter{"spotdl": fakeSpotifyAdapter{}}, runner, output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true))

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{
		AllowPrompt: true,
		PromptOnSpotifyAuth: func(sourceID string) (bool, error) {
			return false, nil
		},
	})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("expected successful retry result, got %+v", result)
	}
	if len(runner.specs) != 2 {
		t.Fatalf("expected two runner calls, got %d", len(runner.specs))
	}
	retryArgs := runner.specs[1].Args
	retryJoined := strings.Join(retryArgs, " ")
	if !strings.Contains(retryJoined, "--user-auth") {
		t.Fatalf("expected --user-auth in retry args, got %v", retryArgs)
	}
	if !strings.Contains(retryJoined, "--headless") {
		t.Fatalf("expected --headless to remain for manual auth flow, got %v", retryArgs)
	}
}

func TestIsSpotifyRateLimited(t *testing.T) {
	source := config.Source{
		Type:    config.SourceTypeSpotify,
		Adapter: config.AdapterSpec{Kind: "spotdl"},
	}
	execResult := ExecResult{
		ExitCode:   1,
		StderrTail: "Your application has reached a rate/request limit. Retry will occur after: 71747 s",
	}

	if !isSpotifyRateLimited(source, execResult) {
		t.Fatalf("expected spotify rate-limit detection to return true")
	}
}

func TestResolveSpotDLOAuthCachePath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	cachePath := filepath.Join(tmpHome, ".spotdl", ".spotipy")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("token"), 0o600); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	got, ok := resolveSpotDLOAuthCachePath()
	if !ok {
		t.Fatalf("expected cache path to be detected")
	}
	if got != "~/.spotdl/.spotipy" {
		t.Fatalf("expected normalized cache path, got %q", got)
	}
}

func TestSyncerSpotifyDeemixScanGapsExecutesPlannedTracksDeterministically(t *testing.T) {
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
				ID:        "spotify-deemix",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-deemix.sync.spotify",
				Adapter:   config.AdapterSpec{Kind: "deemix"},
			},
		},
	}
	statePath := filepath.Join(stateDir, "spotify-deemix.sync.spotify")
	if err := os.WriteFile(statePath, []byte("2abc234def\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "artist-2 - track-2.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write local known file: %v", err)
	}

	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
	})

	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "arl", nil }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		return []spotifyRemoteTrack{
			{ID: "1abc234def", Title: "track-1", Artist: "artist-1", Album: "album-1"},
			{ID: "2abc234def", Title: "track-2", Artist: "artist-2", Album: "album-2"},
			{ID: "3abc234def", Title: "track-3", Artist: "artist-3", Album: "album-3"},
		}, nil
	}

	runner := &sequenceRunner{results: []ExecResult{{ExitCode: 0}, {ExitCode: 0}}}
	syncer := NewSyncer(
		map[string]Adapter{"deemix": fakeDeemixAdapter{}},
		runner,
		output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{ScanGaps: true})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("expected successful deemix source run, got %+v", result)
	}
	if len(runner.specs) != 2 {
		t.Fatalf("expected two track executions, got %d", len(runner.specs))
	}
	if got := runner.specs[0].Args[0]; got != "https://open.spotify.com/track/1abc234def" {
		t.Fatalf("expected first planned track URL, got %q", got)
	}
	if got := runner.specs[1].Args[0]; got != "https://open.spotify.com/track/3abc234def" {
		t.Fatalf("expected second planned track URL, got %q", got)
	}

	payload, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	lines := spotifyStateIDsFromPayload(string(payload))
	if len(lines) < 3 || lines[len(lines)-2] != "1abc234def" || lines[len(lines)-1] != "3abc234def" {
		t.Fatalf("expected appended spotify track ids in deterministic order, got %q", string(payload))
	}
}

func TestSyncerSpotifyDeemixFailureDoesNotAppendFailedTrack(t *testing.T) {
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
				ID:        "spotify-deemix",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-deemix.sync.spotify",
				Adapter:   config.AdapterSpec{Kind: "deemix"},
			},
		},
	}
	statePath := filepath.Join(stateDir, "spotify-deemix.sync.spotify")

	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
	})

	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "arl", nil }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		return []spotifyRemoteTrack{
			{ID: "1abc234def", Title: "track-1", Artist: "artist-1", Album: "album-1"},
			{ID: "2abc234def", Title: "track-2", Artist: "artist-2", Album: "album-2"},
		}, nil
	}

	runner := &sequenceRunner{results: []ExecResult{{ExitCode: 0}, {ExitCode: 1}}}
	syncer := NewSyncer(
		map[string]Adapter{"deemix": fakeDeemixAdapter{}},
		runner,
		output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{ScanGaps: true})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("expected failed deemix source when one track fails, got %+v", result)
	}
	if len(runner.specs) != 2 {
		t.Fatalf("expected two track executions before source failure, got %d", len(runner.specs))
	}

	state, err := parseSpotifySyncState(statePath)
	if err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if len(state.KnownIDs) != 1 {
		t.Fatalf("expected only successful track to be appended, got %+v", state.KnownIDs)
	}
	if _, ok := state.KnownIDs["1abc234def"]; !ok {
		t.Fatalf("expected successful track id to be present in state")
	}
	if _, ok := state.KnownIDs["2abc234def"]; ok {
		t.Fatalf("did not expect failed track id to be appended")
	}
}

func TestSyncerSpotifyDeemixUnavailableTrackIsSkippedAndNotAppended(t *testing.T) {
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
				ID:        "spotify-deemix",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-deemix.sync.spotify",
				Adapter:   config.AdapterSpec{Kind: "deemix"},
			},
		},
	}
	statePath := filepath.Join(stateDir, "spotify-deemix.sync.spotify")

	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
	})

	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "arl", nil }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		return []spotifyRemoteTrack{
			{ID: "1abc234def", Title: "Missing Song", Artist: "Regent", Album: "album-1"},
			{ID: "2abc234def", Title: "Available Song", Artist: "Regent", Album: "album-2"},
		}, nil
	}

	runner := &sequenceRunner{results: []ExecResult{
		{
			ExitCode:   0,
			StderrTail: "GWAPIError: Track unavailable on Deezer\nat GW.api_call (/snapshot/cli/dist/main.cjs)",
		},
		{ExitCode: 0},
	}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	syncer := NewSyncer(
		map[string]Adapter{"deemix": fakeDeemixAdapter{}},
		runner,
		output.NewHumanEmitter(stdout, stderr, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{ScanGaps: true})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("expected source to succeed with skipped unavailable track, got %+v", result)
	}
	if len(runner.specs) != 2 {
		t.Fatalf("expected both planned tracks to execute, got %d", len(runner.specs))
	}
	if !strings.Contains(stdout.String(), "[spotify-deemix] [skip] 1abc234def (Regent - Missing Song) (unavailable-on-deezer)") {
		t.Fatalf("expected normalized unavailable skip event, got stdout=%s stderr=%s", stdout.String(), stderr.String())
	}

	state, err := parseSpotifySyncState(statePath)
	if err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if _, ok := state.KnownIDs["1abc234def"]; ok {
		t.Fatalf("did not expect unavailable track id in state")
	}
	if _, ok := state.KnownIDs["2abc234def"]; !ok {
		t.Fatalf("expected available track id in state, got %+v", state.KnownIDs)
	}
}

func TestSyncerSpotifyDeemixSkipsSubprocessWhenNoDownloadsPlanned(t *testing.T) {
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
				ID:        "spotify-deemix",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-deemix.sync.spotify",
				Adapter:   config.AdapterSpec{Kind: "deemix"},
			},
		},
	}
	statePath := filepath.Join(stateDir, "spotify-deemix.sync.spotify")
	if err := os.WriteFile(statePath, []byte("1abc234def\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "artist-1 - track-1.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write local known file: %v", err)
	}

	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
	})

	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "arl", nil }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		return []spotifyRemoteTrack{
			{ID: "1abc234def", Title: "track-1", Artist: "artist-1", Album: "album-1"},
			{ID: "2abc234def", Title: "track-2", Artist: "artist-2", Album: "album-2"},
		}, nil
	}

	runner := &sequenceRunner{}
	syncer := NewSyncer(
		map[string]Adapter{"deemix": fakeDeemixAdapter{}},
		runner,
		output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("expected source to be marked up-to-date, got %+v", result)
	}
	if len(runner.specs) != 0 {
		t.Fatalf("expected no subprocess calls when planned_download_count=0, got %d", len(runner.specs))
	}
}

func TestSyncerSpotifyDeemixTreatsZeroExitTypeErrorAsFailure(t *testing.T) {
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
				ID:        "spotify-deemix",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-deemix.sync.spotify",
				Adapter:   config.AdapterSpec{Kind: "deemix"},
			},
		},
	}
	statePath := filepath.Join(stateDir, "spotify-deemix.sync.spotify")

	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
	})

	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "arl", nil }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		return []spotifyRemoteTrack{
			{ID: "1abc234def", Title: "track-1", Artist: "artist-1", Album: "album-1"},
		}, nil
	}

	runner := &sequenceRunner{results: []ExecResult{{
		ExitCode:   0,
		StderrTail: "TypeError: Cannot read properties of undefined (reading 'error')\n    at SpotifyPlugin.getTrack (/snapshot/cli/dist/main.cjs)",
	}}}
	syncer := NewSyncer(
		map[string]Adapter{"deemix": fakeDeemixAdapter{}},
		runner,
		output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{ScanGaps: true})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("expected deemix source failure on spotify plugin exception, got %+v", result)
	}

	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected state file to remain untouched on upstream exception, stat=%v", statErr)
	}
}

func TestSyncerSpotifyDeemixTrackURLNoPreflightPrimesCacheAndAppendsState(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	const trackID = "3n3Ppam7vgaVa1iaRUc9Lp"
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
				ID:        "spotify-deemix-track",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/track/" + trackID,
				StateFile: "spotify-deemix-track.sync.spotify",
				Adapter:   config.AdapterSpec{Kind: "deemix"},
			},
		},
	}
	statePath := filepath.Join(stateDir, "spotify-deemix-track.sync.spotify")

	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	origFetchTrackMetadata := fetchSpotifyTrackMetadataFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
		fetchSpotifyTrackMetadataFn = origFetchTrackMetadata
	})

	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "arl", nil }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		t.Fatalf("did not expect preflight enumeration for --no-preflight track run")
		return nil, nil
	}
	fetchSpotifyTrackMetadataFn = func(ctx context.Context, id string) (spotifyTrackMetadata, error) {
		if id != trackID {
			t.Fatalf("unexpected metadata lookup id: %q", id)
		}
		return spotifyTrackMetadata{
			Title:  "Mr. Brightside",
			Artist: "The Killers",
			Album:  "Hot Fuss",
		}, nil
	}

	runner := &cacheInspectRunner{t: t, expectSnippet: "Mr. Brightside"}
	syncer := NewSyncer(
		map[string]Adapter{"deemix": fakeDeemixAdapter{}},
		runner,
		output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{NoPreflight: true})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("expected successful source run, got %+v", result)
	}
	if len(runner.specs) != 1 {
		t.Fatalf("expected one deemix execution, got %d", len(runner.specs))
	}
	if got := runner.specs[0].Args[0]; got != "https://open.spotify.com/track/"+trackID {
		t.Fatalf("unexpected track execution arg: %q", got)
	}

	state, err := parseSpotifySyncState(statePath)
	if err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if _, ok := state.KnownIDs[trackID]; !ok {
		t.Fatalf("expected track id to be appended to state, got %+v", state.KnownIDs)
	}
}

func TestSyncerSpotifyDeemixNoPreflightPlaylistUsesPageEnumeration(t *testing.T) {
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
				ID:        "spotify-deemix-playlist",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/3yp4tiwWn1r0FE7jtvWhbb",
				StateFile: "spotify-deemix-playlist.sync.spotify",
				Adapter:   config.AdapterSpec{Kind: "deemix"},
			},
		},
	}
	statePath := filepath.Join(stateDir, "spotify-deemix-playlist.sync.spotify")

	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	origEnumerateViaPage := enumerateSpotifyViaPageFn
	origFetchTrackMetadata := fetchSpotifyTrackMetadataFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
		enumerateSpotifyViaPageFn = origEnumerateViaPage
		fetchSpotifyTrackMetadataFn = origFetchTrackMetadata
	})

	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "arl", nil }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		t.Fatalf("did not expect spotify api preflight enumeration for --no-preflight run")
		return nil, nil
	}
	enumerateSpotifyViaPageFn = func(ctx context.Context, playlistID string) ([]spotifyRemoteTrack, error) {
		if playlistID != "3yp4tiwWn1r0FE7jtvWhbb" {
			t.Fatalf("unexpected playlist id: %q", playlistID)
		}
		return []spotifyRemoteTrack{
			{ID: "41gXFhitx4whS6PsoXREzy", Title: "Permean", Artist: "Regent", Album: "Permean"},
			{ID: "5onvWxBJehSONyspmnrvhD", Title: "Encoder", Artist: "Regent", Album: "Encoder"},
		}, nil
	}
	fetchSpotifyTrackMetadataFn = func(ctx context.Context, id string) (spotifyTrackMetadata, error) {
		t.Fatalf("did not expect network metadata fetch when page metadata is already available")
		return spotifyTrackMetadata{}, nil
	}

	runner := &sequenceRunner{results: []ExecResult{{ExitCode: 0}, {ExitCode: 0}}}
	syncer := NewSyncer(
		map[string]Adapter{"deemix": fakeDeemixAdapter{}},
		runner,
		output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{NoPreflight: true})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("expected successful source run, got %+v", result)
	}
	if len(runner.specs) != 2 {
		t.Fatalf("expected two deemix executions, got %d", len(runner.specs))
	}
	if got := runner.specs[0].Args[0]; got != "https://open.spotify.com/track/41gXFhitx4whS6PsoXREzy" {
		t.Fatalf("unexpected first track arg: %q", got)
	}
	if got := runner.specs[1].Args[0]; got != "https://open.spotify.com/track/5onvWxBJehSONyspmnrvhD" {
		t.Fatalf("unexpected second track arg: %q", got)
	}

	state, err := parseSpotifySyncState(statePath)
	if err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if _, ok := state.KnownIDs["41gXFhitx4whS6PsoXREzy"]; !ok {
		t.Fatalf("expected first id in state, got %+v", state.KnownIDs)
	}
	if _, ok := state.KnownIDs["5onvWxBJehSONyspmnrvhD"]; !ok {
		t.Fatalf("expected second id in state, got %+v", state.KnownIDs)
	}
}

func TestSyncerSpotifyDeemixFailsClearlyWhenARLMissingAndPromptsDisabled(t *testing.T) {
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
				ID:        "spotify-deemix",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-deemix.sync.spotify",
				Adapter:   config.AdapterSpec{Kind: "deemix"},
			},
		},
	}

	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
	})

	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "", auth.ErrDeemixARLNotFound }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		t.Fatalf("did not expect preflight enumeration when ARL is missing")
		return nil, nil
	}

	runner := &sequenceRunner{}
	syncer := NewSyncer(
		map[string]Adapter{"deemix": fakeDeemixAdapter{}},
		runner,
		output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{AllowPrompt: false})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.DependencyFailures != 1 || result.Failed != 1 || result.Succeeded != 0 {
		t.Fatalf("expected dependency failure for missing ARL, got %+v", result)
	}
	if len(runner.specs) != 0 {
		t.Fatalf("expected no runner calls when ARL is missing, got %d", len(runner.specs))
	}
}

func TestSpotifyTrackDisplayName(t *testing.T) {
	metadata := map[string]spotifyTrackMetadata{
		"41gXFhitx4whS6PsoXREzy": {Title: "Permean", Artist: "Regent"},
		"5onvWxBJehSONyspmnrvhD": {Title: "Encoder"},
	}

	if got := spotifyTrackDisplayName("41gXFhitx4whS6PsoXREzy", metadata); got != "Regent - Permean" {
		t.Fatalf("unexpected display name with artist: %q", got)
	}
	if got := spotifyTrackDisplayName("5onvWxBJehSONyspmnrvhD", metadata); got != "Encoder" {
		t.Fatalf("unexpected display name without artist: %q", got)
	}
	if got := spotifyTrackDisplayName("missing", metadata); got != "" {
		t.Fatalf("expected empty display name for missing id, got %q", got)
	}
}

func TestSyncerSpotifyDeemixPlansKnownGapsWhenKnownTracksAreMissingLocally(t *testing.T) {
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
				ID:        "spotify-deemix-stale-state",
				Type:      config.SourceTypeSpotify,
				Enabled:   true,
				TargetDir: targetDir,
				URL:       "https://open.spotify.com/playlist/a",
				StateFile: "spotify-deemix-stale-state.sync.spotify",
				Adapter:   config.AdapterSpec{Kind: "deemix"},
			},
		},
	}
	statePath := filepath.Join(stateDir, "spotify-deemix-stale-state.sync.spotify")
	if err := os.WriteFile(statePath, []byte("1abc234def\n"), 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	origResolveCreds := resolveSpotifyCredentialsFn
	origResolveARL := resolveDeemixARLFn
	origSaveARL := saveDeemixARLFn
	origEnumerate := enumerateSpotifyTracksFn
	t.Cleanup(func() {
		resolveSpotifyCredentialsFn = origResolveCreds
		resolveDeemixARLFn = origResolveARL
		saveDeemixARLFn = origSaveARL
		enumerateSpotifyTracksFn = origEnumerate
	})

	resolveSpotifyCredentialsFn = func() (auth.SpotifyCredentials, error) {
		return auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"}, nil
	}
	resolveDeemixARLFn = func() (string, error) { return "arl", nil }
	saveDeemixARLFn = func(string) error { return nil }
	enumerateSpotifyTracksFn = func(ctx context.Context, source config.Source, creds auth.SpotifyCredentials) ([]spotifyRemoteTrack, error) {
		return []spotifyRemoteTrack{
			{ID: "1abc234def", Title: "track-1", Artist: "artist-1", Album: "album-1"},
			{ID: "2abc234def", Title: "track-2", Artist: "artist-2", Album: "album-2"},
		}, nil
	}

	runner := &sequenceRunner{results: []ExecResult{{ExitCode: 0}, {ExitCode: 0}}}
	syncer := NewSyncer(
		map[string]Adapter{"deemix": fakeDeemixAdapter{}},
		runner,
		output.NewHumanEmitter(&bytes.Buffer{}, &bytes.Buffer{}, false, true),
	)

	result, err := syncer.Sync(context.Background(), cfg, SyncOptions{})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if result.Succeeded != 1 || result.Failed != 0 {
		t.Fatalf("expected successful source run, got %+v", result)
	}
	if len(runner.specs) != 2 {
		t.Fatalf("expected stale state detection to re-plan all tracks, got %d exec(s)", len(runner.specs))
	}

	state, err := parseSpotifySyncState(statePath)
	if err != nil {
		t.Fatalf("parse state: %v", err)
	}
	if _, ok := state.KnownIDs["1abc234def"]; !ok {
		t.Fatalf("expected first id in state, got %+v", state.KnownIDs)
	}
	if _, ok := state.KnownIDs["2abc234def"]; !ok {
		t.Fatalf("expected second id in state, got %+v", state.KnownIDs)
	}
}

func spotifyStateIDsFromPayload(payload string) []string {
	lines := strings.Split(payload, "\n")
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		id, _ := parseSpotifyStateLine(trimmed)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

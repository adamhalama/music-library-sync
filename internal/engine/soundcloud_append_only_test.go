package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func TestSoundCloudAppendOnlyRefusesUnsafeModesBeforePlanning(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		args       []string
		opts       SyncOptions
	}{
		{name: "free download", kind: "scdl-freedl"},
		{name: "no preflight", kind: "scdl", opts: SyncOptions{NoPreflight: true}},
		{name: "overwrite", kind: "scdl", args: []string{"--overwrite"}},
		{name: "custom ytdlp", kind: "scdl", args: []string{"--yt-dlp-args", "--exec arbitrary"}},
		{name: "custom path", kind: "scdl", args: []string{"--path", "/tmp/elsewhere"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Defaults.StateDir = t.TempDir()
			cfg.Sources = []config.Source{{ID: "sample", Type: config.SourceTypeSoundCloud, Enabled: true, TargetDir: filepath.Join(t.TempDir(), "target"), URL: "https://soundcloud.com/example", StateFile: "source.scdl", Sync: config.SyncPolicy{AppendOnly: true}, Adapter: config.AdapterSpec{Kind: tc.kind, ExtraArgs: tc.args}}}
			old := enumerateSoundCloudTracksFn
			enumerateSoundCloudTracksFn = func(context.Context, config.Source) ([]soundCloudRemoteTrack, error) {
				t.Fatal("unsafe source reached enumeration")
				return nil, nil
			}
			defer func() { enumerateSoundCloudTracksFn = old }()
			runner := &execResultRunner{}
			emitter := &captureEventEmitter{}
			syncer := NewSyncer(map[string]Adapter{tc.kind: fakeAdapter{}}, runner, emitter)
			result, err := syncer.Sync(context.Background(), cfg, tc.opts)
			if err != nil || result.Failed != 1 || len(runner.specs) != 0 {
				t.Fatalf("unsafe mode did not fail closed: %+v %v", result, err)
			}
			found := false
			for _, event := range emitter.events {
				if strings.Contains(event.Message, "append_only") {
					found = true
				}
			}
			if !found {
				t.Fatal("no actionable append-only error emitted")
			}
		})
	}
}

func TestPublishAppendOnlySoundCloudTrackRefusesCollisionsAndEscapes(t *testing.T) {
	for _, mode := range []string{"existing file", "existing symlink", "staged symlink", "outside stage"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			stage, target := filepath.Join(root, "stage"), filepath.Join(root, "target")
			if err := os.Mkdir(stage, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatal(err)
			}
			oldFile := filepath.Join(target, "old.m4a")
			if err := os.WriteFile(oldFile, []byte("old audio"), 0o644); err != nil {
				t.Fatal(err)
			}
			statePath, archivePath := filepath.Join(root, "state"), filepath.Join(root, "archive")
			for _, path := range []string{statePath, archivePath} {
				if err := os.WriteFile(path, []byte("original\r\n\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(stage, "new.m4a")
			if mode == "staged symlink" {
				if err := os.Symlink(oldFile, path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, []byte("new audio"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "existing file":
				if err := os.WriteFile(filepath.Join(target, "new.m4a"), []byte("existing collision"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "existing symlink":
				if err := os.Symlink(oldFile, filepath.Join(target, "new.m4a")); err != nil {
					t.Fatal(err)
				}
			case "outside stage":
				path = oldFile
			}
			err := publishAppendOnlySoundCloudTrack(stage, target, statePath, archivePath, soundCloudSyncEntry{ID: "301", FilePath: path})
			if err == nil {
				t.Fatal("unsafe publish succeeded")
			}
			for _, path := range []string{statePath, archivePath} {
				payload, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(payload, []byte("original\r\n\n")) {
					t.Fatalf("original state changed: %q %v", payload, err)
				}
			}
			payload, err := os.ReadFile(oldFile)
			if err != nil || string(payload) != "old audio" {
				t.Fatalf("original file changed: %q %v", payload, err)
			}
			if mode == "existing file" {
				payload, err := os.ReadFile(filepath.Join(target, "new.m4a"))
				if err != nil || string(payload) != "existing collision" {
					t.Fatalf("collision overwritten: %q %v", payload, err)
				}
			}
		})
	}
}

func TestAppendSoundCloudPreservingLinesKeepsUnterminatedBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	before := []byte("# comment\r\n\n  soundcloud 101 old.m4a")
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := appendSoundCloudPreservingLines(path, "soundcloud 301 new.m4a\n"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, append(append([]byte{}, before...), []byte("\nsoundcloud 301 new.m4a\n")...)) {
		t.Fatalf("prefix changed: %q %v", after, err)
	}
}

func TestSoundCloudPreflightPreservesExplicitResourceURL(t *testing.T) {
	for _, path := range []string{"/example/likes", "/example/sets/sample", "/example/track"} {
		source := config.Source{URL: "https://soundcloud.com" + path, Adapter: config.AdapterSpec{ExtraArgs: []string{"-f"}}}
		if got := effectiveSoundCloudListURL(source); got != source.URL {
			t.Fatalf("explicit resource rewritten: %q", got)
		}
	}
}

func TestSCDLAppendOnlyPlanCannotSelectKnownGaps(t *testing.T) {
	root := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Defaults.StateDir = root
	source := config.Source{ID: "sample", Type: config.SourceTypeSoundCloud, StateFile: "state", TargetDir: root, Sync: config.SyncPolicy{AppendOnly: true}, Adapter: config.AdapterSpec{Kind: "scdl"}}
	if err := os.WriteFile(filepath.Join(root, "state"), []byte("soundcloud 101 missing.m4a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracks := []SoundCloudRemoteTrack{{ID: "301", Title: "New", URL: "https://soundcloud.com/example/301"}, {ID: "101", Title: "Changed", URL: "https://soundcloud.com/example/101"}}
	plan, err := NewSCDLPlanProvider().BuildWithTracks(context.Background(), cfg, source, SyncOptions{}, tracks)
	if err != nil {
		t.Fatal(err)
	}
	rows := plan.Rows()
	if rows[1].Toggleable || rows[1].Status != PlanRowAlreadyDownloaded {
		t.Fatalf("known gap can be selected: %+v", rows[1])
	}
	manifest, err := BuildExecutionManifest(source.ID, rows, []int{1}, DownloadOrderOldestFirst)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := plan.ApplySelection(manifest, PlanApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if execution.SourceForExec.StateFile != filepath.Join(root, "state") || execution.StateSwap.TempSyncPath != "" || execution.StateSwap.TempArchivePath != "" {
		t.Fatalf("append-only plan entered rewriting state swap: %+v", execution)
	}
}

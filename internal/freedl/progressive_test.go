package freedl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

func TestBuildCapturePlanProgressEmitsRowsBeforeDone(t *testing.T) {
	origStream := streamSoundCloudTracksFn
	origProbe := probeSoundCloudFreeDLFn
	t.Cleanup(func() {
		streamSoundCloudTracksFn = origStream
		probeSoundCloudFreeDLFn = origProbe
	})

	streamSoundCloudTracksFn = func(ctx context.Context, source config.Source, limit int, onTrack func(engine.SoundCloudRemoteTrack) error) ([]engine.SoundCloudRemoteTrack, error) {
		tracks := []engine.SoundCloudRemoteTrack{
			{ID: "111", Title: "First", URL: "https://soundcloud.com/u/first"},
			{ID: "222", Title: "Second", URL: "https://soundcloud.com/u/second"},
		}
		for _, track := range tracks {
			if err := onTrack(track); err != nil {
				return nil, err
			}
		}
		return tracks, nil
	}
	probeSoundCloudFreeDLFn = func(ctx context.Context, row engine.PlanRow) engine.SoundCloudFreeDLProbe {
		return engine.SoundCloudFreeDLProbe{
			Status:      engine.SoundCloudFreeDLAvailable,
			PurchaseURL: "https://hypeddit.com/u/" + row.RemoteID,
			Host:        "hypeddit.com",
		}
	}

	dir := t.TempDir()
	libraryDir := filepath.Join(dir, "library")
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		t.Fatalf("mkdir library: %v", err)
	}
	main := config.DefaultConfig()
	main.Defaults.StateDir = filepath.Join(dir, "state")
	main.Defaults.ArchiveFile = "archive.txt"
	job := Job{
		ID:              "soundcloud-free-dl",
		Enabled:         true,
		SourceURL:       "https://soundcloud.com/u/likes",
		LibraryDir:      libraryDir,
		BufferDir:       filepath.Join(dir, "buffer"),
		BackupDir:       filepath.Join(dir, "backups"),
		LogDir:          filepath.Join(dir, "logs"),
		StateFile:       "soundcloud-free-dl.sync.scdl",
		PlanLimit:       2,
		DownloadOrder:   DefaultDownloadOrder,
		TargetFormat:    TargetAuto,
		MinMatchScore:   DefaultMinMatchScore,
		AmbiguityGap:    DefaultAmbiguityGap,
		ApplyPromotions: false,
	}

	events := Service{}.BuildCapturePlanProgress(context.Background(), main, job)
	rowBeforeDone := false
	var final CapturePlan
	for event := range events {
		switch event.Kind {
		case CapturePlanEventRow:
			if final.RunID == "" {
				rowBeforeDone = true
			}
		case CapturePlanEventDone:
			final = event.Plan
		case CapturePlanEventFailed:
			t.Fatalf("progressive plan failed: %v", event.Err)
		}
	}

	if !rowBeforeDone {
		t.Fatalf("expected at least one row event before final plan")
	}
	if len(final.Rows) != 2 {
		t.Fatalf("expected 2 final rows, got %d", len(final.Rows))
	}
	for _, row := range final.Rows {
		if !row.Selectable || !row.Selected {
			t.Fatalf("expected row selectable and selected by default: %+v", row)
		}
	}
}

package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/sourceimport"
)

type fakeSourceImportBridge struct {
	inspect  bridge.SourceImportInspectResponse
	applied  []bridge.SourceImportApplyRequest
	tamperAt int
}

func (f *fakeSourceImportBridge) SourceImportInspect(_ context.Context, req bridge.SourceImportInspectRequest) (bridge.SourceImportInspectResponse, error) {
	return f.inspect, nil
}

func (f *fakeSourceImportBridge) SourceImportApply(_ context.Context, req bridge.SourceImportApplyRequest) (bridge.SourceImportApplyResponse, error) {
	f.applied = append(f.applied, req)
	resp := bridge.SourceImportApplyResponse{}
	for i, row := range req.Rows {
		id := row.ContentID
		if id == "" {
			id = "new-" + row.Key
		}
		created := row.CreatedAt
		if f.tamperAt == i+1 {
			created = "2000-01-01T00:00:00.900Z"
		}
		resp.Rows = append(resp.Rows, bridge.SourceImportAppliedRow{Key: row.Key, ContentID: id, Path: row.Path, CreatedAt: created, StockDate: row.StockDate})
	}
	return resp, nil
}

type stubTags struct{}

func (stubTags) ReadTags(_ context.Context, path string) (sourceimport.Tags, error) {
	return sourceimport.TagsFromFilename(path), nil
}

func sourceImportFixture(t *testing.T) (RekordboxSourceImportUseCase, *fakeSourceImportBridge, *[]string, RekordboxSourceImportRequest) {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, "music")
	fake := &fakeSourceImportBridge{inspect: bridge.SourceImportInspectResponse{Contents: []bridge.SourceImportContent{
		{ID: "11", FolderPath: filepath.Join(target, "Fluke - Zion.mp3"), CreatedAt: "2026-09-19T14:10:27.120Z", StockDate: "2026-09-19"},
		{ID: "12", FolderPath: filepath.Join(target, "Vitalic - Poison Lips.mp3"), CreatedAt: "2026-09-19T14:10:27.110Z", StockDate: "2026-09-19"},
	}}}
	events := []string{}
	useCase := RekordboxSourceImportUseCase{
		Bridge: fake,
		FetchOrder: func(_ context.Context, url string) ([]engine.SpotifyPlaylistTrack, error) {
			return []engine.SpotifyPlaylistTrack{
				{ID: "zion", Title: "Zion", Artist: "Fluke", Position: 1},
				{ID: "poison", Title: "Poison Lips", Artist: "Vitalic", Position: 2},
				{ID: "ness", Title: "A Darkly Sunset", Artist: "Ness", Position: 3},
				{ID: "abroad", Title: "Abroad", Artist: "Len Faki", Position: 4},
			}, nil
		},
		LoadState: func(path string) (map[string]engine.SpotifyStateEntry, error) {
			if !strings.HasSuffix(path, "slow.sync.spotify") {
				t.Fatalf("unexpected state path %s", path)
			}
			return map[string]engine.SpotifyStateEntry{
				"zion":   {LocalPath: "Fluke - Zion.mp3"},
				"poison": {LocalPath: "Vitalic - Poison Lips.mp3"},
				"abroad": {LocalPath: "Len Faki - Abroad.mp3"},
			}, nil
		},
		TagReader:  stubTags{},
		FileExists: func(string) bool { return true },
		Now:        func() time.Time { return time.Date(2026, 11, 21, 0, 30, 0, 0, time.UTC) },
		Location:   time.UTC,
		CheckClosed: func(context.Context, string) error {
			events = append(events, "check")
			return nil
		},
		CreateBackup: func(_ context.Context, dbDir, backupRoot string, _ time.Time) (string, error) {
			events = append(events, "backup")
			return filepath.Join(backupRoot, "b1"), nil
		},
	}
	req := RekordboxSourceImportRequest{
		Config: config.Config{
			Defaults: config.Defaults{StateDir: filepath.Join(root, "state")},
			Sources: []config.Source{
				{ID: "sc", Type: config.SourceTypeSoundCloud, TargetDir: target, URL: "https://soundcloud.com/x", StateFile: "sc.sync.scdl"},
				{ID: "spotify-slow-techno", Type: config.SourceTypeSpotify, TargetDir: target, URL: "https://open.spotify.com/playlist/abc", StateFile: "slow.sync.spotify"},
			},
		},
		SourceID:  "spotify-slow-techno",
		DBDir:     filepath.Join(root, "rb"),
		BackupDir: filepath.Join(root, "backups"),
		Step:      time.Second,
	}
	return useCase, fake, &events, req
}

func TestRekordboxSourceImportPlanAndApplyWritesOrderedRows(t *testing.T) {
	useCase, fake, events, req := sourceImportFixture(t)
	plan, err := useCase.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Existing != 2 || plan.Summary.Import != 1 || plan.Summary.DateChanges != 1 || plan.Summary.NotDownloaded != 1 {
		t.Fatalf("unexpected summary %+v", plan.Summary)
	}
	result, err := useCase.Apply(context.Background(), req, plan)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(*events, ",") != "check,backup,check" {
		t.Fatalf("expected guard before backup, got %v", *events)
	}
	if !result.Applied || result.BackupPath == "" || len(fake.applied) != 1 {
		t.Fatalf("unexpected result %+v", result)
	}
	rows := fake.applied[0].Rows
	if len(rows) != 2 {
		t.Fatalf("only changing rows should be sent, got %+v", rows)
	}
	if rows[0].Action != bridge.SourceImportActionUpdate || rows[0].ContentID != "12" || rows[0].ExpectedCreatedAt != "2026-09-19T14:10:27.110Z" || rows[0].CreatedAt != "2026-09-19T14:10:28.120Z" {
		t.Fatalf("unexpected redate row %+v", rows[0])
	}
	if rows[1].Action != bridge.SourceImportActionImport || rows[1].Tags == nil || rows[1].Tags.Artist != "Len Faki" || rows[1].CreatedAt != "2026-11-21T00:30:00.900Z" {
		t.Fatalf("unexpected import row %+v", rows[1])
	}
}

func TestRekordboxSourceImportDryRunWritesNothing(t *testing.T) {
	useCase, fake, events, req := sourceImportFixture(t)
	req.DryRun = true
	plan, err := useCase.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	result, err := useCase.Apply(context.Background(), req, plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Applied || len(*events) != 0 || len(fake.applied) != 0 {
		t.Fatalf("dry run must not check, back up or write: %+v %v", result, *events)
	}
}

func TestRekordboxSourceImportRefusesWhileLibraryAppIsOpen(t *testing.T) {
	useCase, fake, events, req := sourceImportFixture(t)
	useCase.CheckClosed = func(context.Context, string) error { return errors.New("rbxport is running") }
	plan, err := useCase.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := useCase.Apply(context.Background(), req, plan); err == nil || !strings.Contains(err.Error(), "rbxport") {
		t.Fatalf("expected guard error, got %v", err)
	}
	if len(*events) != 0 || len(fake.applied) != 0 {
		t.Fatalf("guard failure must stop before backup and write: %v", *events)
	}
}

func TestRekordboxSourceImportDetectsReadBackMismatch(t *testing.T) {
	useCase, fake, _, req := sourceImportFixture(t)
	fake.tamperAt = 2
	plan, err := useCase.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := useCase.Apply(context.Background(), req, plan); err == nil || !strings.Contains(err.Error(), "post-apply verification failed") {
		t.Fatalf("expected verification failure, got %v", err)
	}
}

func TestRekordboxSourceImportRejectsNonSpotifySource(t *testing.T) {
	useCase, _, _, req := sourceImportFixture(t)
	req.SourceID = "sc"
	if _, err := useCase.Plan(context.Background(), req); err == nil || !strings.Contains(err.Error(), "only Spotify sources") {
		t.Fatalf("expected source type error, got %v", err)
	}
}

func TestSourceImportAnalysesOnlyNewRowsWithExistingBackup(t *testing.T) {
	use, fake, _, req := sourceImportFixture(t)
	req.Analyse = true
	req.AnalysisMode = "rekordbox"
	called := false
	use.Analyse = func(_ context.Context, r RekordboxAnalysisRequest) (RekordboxAnalysisResult, error) {
		called = true
		if len(r.ContentIDs) != 1 || r.ContentIDs[0] != "new-abroad" {
			t.Fatalf("analysis selected existing rows: %v", r.ContentIDs)
		}
		if r.backupTaken == "" || r.Mode != "rekordbox" {
			t.Fatalf("missing backup or mode: %+v", r)
		}
		if len(fake.applied) != 1 {
			t.Fatal("analysis ran before import committed")
		}
		return RekordboxAnalysisResult{}, nil
	}
	plan, err := use.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	result, err := use.Apply(context.Background(), req, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !called || result.Analysis == nil {
		t.Fatal("new rows were not analysed")
	}
}

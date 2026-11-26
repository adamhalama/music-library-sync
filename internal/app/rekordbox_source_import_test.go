package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if strings.Join(*events, ",") != "check,backup" {
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

func TestRekordboxSourceImportRejectsUnsupportedSoundCloudAdapter(t *testing.T) {
	for _, kind := range []string{"", "scdl-freedl"} {
		useCase, _, _, req := sourceImportFixture(t)
		req.SourceID = "sc"
		req.Config.Sources[0].Adapter.Kind = kind
		if _, err := useCase.Plan(context.Background(), req); err == nil || !strings.Contains(err.Error(), "requires adapter.kind=scdl") {
			t.Fatalf("expected adapter error for %q, got %v", kind, err)
		}
	}
}

func TestSoundCloudSourceImportUsesRemoteAddedOrderAndStatePaths(t *testing.T) {
	for _, kind := range []string{"likes", "sets/sample"} {
		t.Run(kind, func(t *testing.T) {
			useCase, _, _, req := sourceImportFixture(t)
			req.SourceID = "sc"
			source := &req.Config.Sources[0]
			source.URL = "https://soundcloud.com/example/" + kind
			source.Adapter.Kind = "scdl"
			useCase.FetchSoundCloudOrder = func(context.Context, config.Source) ([]engine.SoundCloudRemoteTrack, error) {
				return []engine.SoundCloudRemoteTrack{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}, {ID: "c", Title: "C"}, {ID: "d", Title: "D"}}, nil
			}
			useCase.LoadSoundCloudState = func(string) (map[string]string, error) {
				return map[string]string{"a": "A.mp3", "b": filepath.Join(source.TargetDir, "B.mp3"), "c": "gone.mp3", "d": "A.mp3", "absent": "No Longer Remote.mp3"}, nil
			}
			useCase.FileExists = func(path string) bool { return !strings.HasSuffix(path, "gone.mp3") }
			plan, err := useCase.Plan(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			first := "d"
			if kind == "sets/sample" {
				first = "a"
			}
			if plan.Rows[0].ID != first || plan.Summary.Import != 2 || plan.Summary.FileMissing != 1 || plan.Summary.DuplicatePath != 1 || plan.Summary.LocalNotRemote != 1 {
				t.Fatalf("unexpected SoundCloud plan: %+v", plan)
			}
			for _, row := range plan.Rows {
				if row.Path != "" && !filepath.IsAbs(row.Path) {
					t.Fatalf("relative file path: %+v", row)
				}
			}
		})
	}
}

func TestSoundCloudAppendOnlyImportSendsNoExistingRowUpdates(t *testing.T) {
	useCase, fake, _, req := sourceImportFixture(t)
	req.SourceID = "sc"
	source := &req.Config.Sources[0]
	source.URL = "https://soundcloud.com/example/sets/sample"
	source.Adapter.Kind = "scdl"
	source.Sync.AppendOnly = true
	useCase.FetchSoundCloudOrder = func(context.Context, config.Source) ([]engine.SoundCloudRemoteTrack, error) {
		return []engine.SoundCloudRemoteTrack{{ID: "new-a"}, {ID: "old-a"}, {ID: "new-b"}, {ID: "old-b"}}, nil
	}
	useCase.LoadSoundCloudState = func(string) (map[string]string, error) {
		return map[string]string{"new-a": "New A.mp3", "old-a": "Fluke - Zion.mp3", "new-b": "New B.mp3", "old-b": "Vitalic - Poison Lips.mp3", "absent": "Gone From Remote.mp3"}, nil
	}
	fake.inspect.Contents = append(fake.inspect.Contents, bridge.SourceImportContent{ID: "13", FolderPath: filepath.Join(source.TargetDir, "Gone From Remote.mp3"), CreatedAt: "2027-01-01T00:00:00.500Z", StockDate: "2027-01-01"})
	before, _ := json.Marshal(fake.inspect.Contents)
	plan, err := useCase.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.DateChanges != 0 || plan.Summary.CollectionExisting != 3 || plan.Summary.LocalNotRemote != 1 {
		t.Fatalf("unexpected summary: %+v", plan.Summary)
	}
	if _, err := useCase.Apply(context.Background(), req, plan); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(fake.inspect.Contents)
	if !bytes.Equal(before, after) {
		t.Fatal("inspection rows mutated")
	}
	sent := fake.applied[0]
	if !sent.AppendOnly || len(sent.Rows) != 2 || sent.Rows[0].Key != "new-a" || sent.Rows[1].Key != "new-b" {
		t.Fatalf("wrong bridge request: %+v", sent)
	}
	for _, row := range sent.Rows {
		if row.Action != bridge.SourceImportActionImport || row.ContentID != "" || row.ExpectedCreatedAt != "" {
			t.Fatalf("existing row update leaked: %+v", sent)
		}
	}
	if sent.Rows[0].CreatedAt != "2027-01-01T00:00:01.500Z" || sent.Rows[1].CreatedAt != "2027-01-01T00:00:02.500Z" {
		t.Fatalf("wrong new timeline: %+v", sent)
	}
	// Rebuild against the collection after applying imports.
	for i, row := range sent.Rows {
		fake.inspect.Contents = append(fake.inspect.Contents, bridge.SourceImportContent{ID: fmt.Sprint(20 + i), FolderPath: row.Path, CreatedAt: row.CreatedAt, StockDate: row.StockDate})
	}
	second, err := useCase.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if second.HasChanges() {
		t.Fatalf("second run planned writes: %+v", second)
	}
}

func TestRekordboxSourceImportNewOnlyAppendsAfterExistingRowsAndNow(t *testing.T) {
	configure := func(t *testing.T) (RekordboxSourceImportUseCase, *fakeSourceImportBridge, RekordboxSourceImportRequest) {
		t.Helper()
		useCase, fake, _, req := sourceImportFixture(t)
		req.SourceID = "sc"
		source := &req.Config.Sources[0]
		source.URL = "https://soundcloud.com/example/sets/sample"
		source.Adapter.Kind = "scdl"
		source.Sync.AppendOnly = false
		useCase.FetchSoundCloudOrder = func(context.Context, config.Source) ([]engine.SoundCloudRemoteTrack, error) {
			return []engine.SoundCloudRemoteTrack{{ID: "new-a"}, {ID: "old-a"}, {ID: "new-b"}, {ID: "old-b"}}, nil
		}
		useCase.LoadSoundCloudState = func(string) (map[string]string, error) {
			return map[string]string{
				"new-a":  "New A.mp3",
				"old-a":  "Fluke - Zion.mp3",
				"new-b":  "New B.mp3",
				"old-b":  "Vitalic - Poison Lips.mp3",
				"absent": "Gone From Remote.mp3",
			}, nil
		}
		fake.inspect.Contents = append(fake.inspect.Contents, bridge.SourceImportContent{
			ID: "13", FolderPath: filepath.Join(source.TargetDir, "Gone From Remote.mp3"),
			CreatedAt: "2027-01-01T00:00:00.500Z", StockDate: "2027-01-01",
		})
		return useCase, fake, req
	}

	useCase, _, req := configure(t)
	plan, err := useCase.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if plan.AppendOnly || plan.Summary.DateChanges != 1 || !plan.Rows[3].Changes() || plan.Rows[3].ContentID != "12" {
		t.Fatalf("without new-only expected the out-of-order existing row to be redated: %+v", plan)
	}

	for _, tc := range []struct {
		name string
		now  time.Time
		want [2]string
	}{
		{"after_latest_existing_row", useCase.Now(), [2]string{"2027-01-01T00:00:01.500Z", "2027-01-01T00:00:02.500Z"}},
		{"after_run_time", time.Date(2028, 1, 1, 0, 0, 0, 500_000_000, time.UTC), [2]string{"2028-01-01T00:00:01.500Z", "2028-01-01T00:00:02.500Z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useCase, fake, req := configure(t)
			req.NewOnly = true
			useCase.Now = func() time.Time { return tc.now }
			plan, err := useCase.Plan(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if plan.Summary.DateChanges != 0 || plan.Rows[1].Changes() || plan.Rows[3].Changes() {
				t.Fatalf("new-only must preserve existing rows: %+v", plan)
			}
			if _, err := useCase.Apply(context.Background(), req, plan); err != nil {
				t.Fatal(err)
			}
			if len(fake.applied) != 1 {
				t.Fatalf("expected one bridge request, got %d", len(fake.applied))
			}
			sent := fake.applied[0]
			if !sent.AppendOnly || len(sent.Rows) != 2 || sent.Rows[0].Key != "new-a" || sent.Rows[1].Key != "new-b" {
				t.Fatalf("expected only new imports in source order: %+v", sent)
			}
			for i, row := range sent.Rows {
				if row.Action != bridge.SourceImportActionImport || row.ContentID != "" || row.ExpectedCreatedAt != "" {
					t.Fatalf("existing row update leaked into new-only request: %+v", sent.Rows)
				}
				if row.CreatedAt != tc.want[i] || row.StockDate != tc.want[i][:10] {
					t.Fatalf("wrong new timeline: %+v", sent.Rows)
				}
			}
		})
	}
}

func TestSoundCloudSourceImportUsesIDWhenPrivateTrackTitleIsUnavailable(t *testing.T) {
	useCase, _, _, req := sourceImportFixture(t)
	req.SourceID = "sc"
	req.Config.Sources[0].Adapter.Kind = "scdl"
	useCase.FetchSoundCloudOrder = func(context.Context, config.Source) ([]engine.SoundCloudRemoteTrack, error) {
		return []engine.SoundCloudRemoteTrack{{ID: "123", Title: "NA"}}, nil
	}
	useCase.LoadSoundCloudState = func(string) (map[string]string, error) { return map[string]string{"123": "Artist - Track.m4a"}, nil }
	plan, err := useCase.Plan(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].ID != "123" || plan.Rows[0].Title != "" || plan.Rows[0].Tags.Title != "Track" || plan.Rows[0].Tags.Artist != "Artist" {
		t.Fatalf("private metadata did not use ID and tags: %+v", plan)
	}
}

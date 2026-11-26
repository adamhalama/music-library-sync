package sourceimport

import (
	"context"
	"testing"
	"time"
)

type fakeTagReader struct{ calls []string }

func (f *fakeTagReader) ReadTags(_ context.Context, path string) (Tags, error) {
	f.calls = append(f.calls, path)
	tags := TagsFromFilename(path)
	tags.Genre = "Electro"
	tags.Source = TagSourceFFProbe
	return tags, nil
}

func mustTime(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := ParseTimestamp(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func baseInput(t *testing.T, tracks []SourceTrack, contents []Content) Input {
	t.Helper()
	return Input{
		SourceID:   "spotify-test",
		TargetDir:  "/music/src",
		DBDir:      "/rb",
		Tracks:     tracks,
		Contents:   contents,
		ImportTime: mustTime(t, "2026-11-21T00:30:00.000Z"),
		Step:       time.Second,
		Location:   time.FixedZone("CET", 3600),
		FileExists: func(path string) bool { return path != "/music/src/Gone - Missing.mp3" },
		TagReader:  &fakeTagReader{},
	}
}

func rowsByPosition(plan Plan) map[int]Row {
	out := map[int]Row{}
	for _, row := range plan.Rows {
		out[row.Position] = row
	}
	return out
}

func TestBuildPlanOrdersOldestFirstAndKeepsOrderedTimestamps(t *testing.T) {
	tracks := []SourceTrack{
		{Position: 3, ID: "c", Title: "C", Path: "/music/src/A - C.mp3"},
		{Position: 1, ID: "a", Title: "A", Path: "/music/src/A - A.mp3"},
		{Position: 2, ID: "b", Title: "B", Path: "/music/src/A - B.mp3"},
		{Position: 4, ID: "d", Title: "D", Path: "/music/src/A - D.mp3"},
	}
	contents := []Content{
		// Imported in reverse order within the same tenth of a second.
		{ID: "1", FolderPath: "/music/src/A - A.mp3", CreatedAt: mustTime(t, "2026-09-19T14:10:27.120Z"), StockDate: "2026-09-19"},
		{ID: "2", FolderPath: "/music/src/A - B.mp3", CreatedAt: mustTime(t, "2026-09-19T14:10:27.110Z"), StockDate: "2026-09-19"},
		// Already a full day later: keeps its own time.
		{ID: "3", FolderPath: "/music/src/A - C.mp3", CreatedAt: mustTime(t, "2026-09-25T19:03:36.447Z"), StockDate: "2026-09-25"},
	}
	plan, err := BuildPlan(context.Background(), baseInput(t, tracks, contents))
	if err != nil {
		t.Fatal(err)
	}
	got := rowsByPosition(plan)
	if plan.Rows[0].Position != 1 || plan.Rows[3].Position != 4 {
		t.Fatalf("rows not in playlist order: %+v", plan.Rows)
	}
	if got[1].Changes() || got[1].NewCreatedAt != "2026-09-19T14:10:27.120Z" {
		t.Fatalf("first track should keep its time, got %+v", got[1])
	}
	if got[2].NewCreatedAt != "2026-09-19T14:10:28.120Z" || got[2].NewStockDate != "2026-09-19" || !got[2].Changes() {
		t.Fatalf("second track should move one second after the first, got %+v", got[2])
	}
	if got[3].Changes() {
		t.Fatalf("already ordered track should not change, got %+v", got[3])
	}
	if got[4].Status != StatusImport || got[4].NewCreatedAt != "2026-11-21T00:30:00.900Z" || got[4].NewStockDate != "2026-11-21" {
		t.Fatalf("trailing new track should start at import time with a non-zero fraction, got %+v", got[4])
	}
	if plan.Summary.Existing != 3 || plan.Summary.Import != 1 || plan.Summary.DateChanges != 1 {
		t.Fatalf("unexpected summary %+v", plan.Summary)
	}
	if err := VerifyTimeline(plan.TimelineRows()); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPlanNewTrackInsideExistingRunFollowsPreviousTrack(t *testing.T) {
	tracks := []SourceTrack{
		{Position: 1, ID: "a", Title: "A", Path: "/music/src/A - A.mp3"},
		{Position: 2, ID: "new", Title: "New", Path: "/music/src/A - New.mp3"},
		{Position: 3, ID: "nd", Title: "Not downloaded"},
		{Position: 4, ID: "b", Title: "B", Path: "/music/src/A - B.mp3"},
	}
	contents := []Content{
		{ID: "1", FolderPath: "/music/src/A - A.mp3", CreatedAt: mustTime(t, "2026-09-25T19:03:36.447Z"), StockDate: "2026-09-25"},
		{ID: "2", FolderPath: "/music/src/A - B.mp3", CreatedAt: mustTime(t, "2026-09-25T19:03:36.460Z"), StockDate: "2026-09-25"},
	}
	plan, err := BuildPlan(context.Background(), baseInput(t, tracks, contents))
	if err != nil {
		t.Fatal(err)
	}
	got := rowsByPosition(plan)
	if got[2].Status != StatusImport || got[2].NewCreatedAt != "2026-09-25T19:03:37.447Z" || got[2].NewStockDate != "2026-09-25" {
		t.Fatalf("new track between existing ones should follow its predecessor, got %+v", got[2])
	}
	if got[3].Status != StatusNotDownloaded || got[3].NewCreatedAt != "" {
		t.Fatalf("not downloaded track should be skipped, got %+v", got[3])
	}
	if got[4].NewCreatedAt != "2026-09-25T19:03:38.447Z" {
		t.Fatalf("existing track after the new one should move after it, got %+v", got[4])
	}
}

func TestBuildPlanLeadingNewTracksSortBeforeFirstExisting(t *testing.T) {
	tracks := []SourceTrack{
		{Position: 1, ID: "n1", Title: "N1", Path: "/music/src/A - N1.mp3"},
		{Position: 2, ID: "n2", Title: "N2", Path: "/music/src/A - N2.mp3"},
		{Position: 3, ID: "a", Title: "A", Path: "/music/src/A - A.mp3"},
	}
	contents := []Content{
		{ID: "1", FolderPath: "/music/src/A - A.mp3", CreatedAt: mustTime(t, "2026-09-25T19:03:36.447Z"), StockDate: "2026-09-25"},
	}
	plan, err := BuildPlan(context.Background(), baseInput(t, tracks, contents))
	if err != nil {
		t.Fatal(err)
	}
	got := rowsByPosition(plan)
	if got[1].NewCreatedAt != "2026-09-25T19:03:34.447Z" || got[2].NewCreatedAt != "2026-09-25T19:03:35.447Z" || got[3].Changes() {
		t.Fatalf("leading new tracks should count back from the first existing one: %+v", plan.Rows)
	}
}

func TestBuildPlanIsStableOnRerun(t *testing.T) {
	tracks := []SourceTrack{
		{Position: 1, ID: "a", Title: "A", Path: "/music/src/A - A.mp3"},
		{Position: 2, ID: "b", Title: "B", Path: "/music/src/A - B.mp3"},
		{Position: 3, ID: "c", Title: "C", Path: "/music/src/A - C.mp3"},
	}
	contents := []Content{
		{ID: "1", FolderPath: "/music/src/A - A.mp3", CreatedAt: mustTime(t, "2026-09-19T14:10:27.120Z"), StockDate: "2026-09-19"},
		{ID: "2", FolderPath: "/music/src/A - B.mp3", CreatedAt: mustTime(t, "2026-09-19T14:10:27.110Z"), StockDate: "2026-09-19"},
	}
	first, err := BuildPlan(context.Background(), baseInput(t, tracks, contents))
	if err != nil {
		t.Fatal(err)
	}
	// Apply the first plan to the fake collection.
	applied := []Content{}
	for i, row := range first.TimelineRows() {
		applied = append(applied, Content{ID: string(rune('1' + i)), FolderPath: row.Path, CreatedAt: mustTime(t, row.NewCreatedAt), StockDate: row.NewStockDate})
	}
	in := baseInput(t, tracks, applied)
	in.ImportTime = in.ImportTime.Add(48 * time.Hour)
	second, err := BuildPlan(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if second.HasChanges() {
		t.Fatalf("re-run should change nothing, got %+v", second.Rows)
	}
}

func TestBuildPlanMatchesPathsAcrossUnicodeNormalization(t *testing.T) {
	nfc := "/music/src/Howling - Signs (Rødhåd Remix).mp3"
	nfd := "/music/src/Howling - Signs (Rødhåd Remix).mp3"
	tracks := []SourceTrack{{Position: 1, ID: "a", Title: "Signs", Path: nfc}}
	contents := []Content{{ID: "1", FolderPath: nfd, CreatedAt: mustTime(t, "2026-09-25T19:03:36.447Z"), StockDate: "2026-09-25"}}
	plan, err := BuildPlan(context.Background(), baseInput(t, tracks, contents))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Rows[0].Status != StatusExisting || plan.Rows[0].Path != nfd {
		t.Fatalf("expected NFD collection row to match NFC state path, got %+v", plan.Rows[0])
	}
}

func TestBuildPlanSkipsMissingAndDuplicateFilesAndOverridesGenre(t *testing.T) {
	tracks := []SourceTrack{
		{Position: 1, ID: "a", Title: "A", Path: "/music/src/A - A.mp3"},
		{Position: 2, ID: "a2", Title: "A again", Path: "/music/src/A - A.mp3"},
		{Position: 3, ID: "gone", Title: "Missing", Path: "/music/src/Gone - Missing.mp3"},
	}
	in := baseInput(t, tracks, nil)
	in.GenreOverride = "electro techno"
	plan, err := BuildPlan(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	got := rowsByPosition(plan)
	if got[1].Status != StatusImport || got[1].Tags == nil || got[1].Tags.Genre != "electro techno" || got[1].Tags.Artist != "A" {
		t.Fatalf("expected import with overridden genre, got %+v", got[1])
	}
	if got[2].Status != StatusDuplicatePath || got[3].Status != StatusFileMissing {
		t.Fatalf("expected duplicate and missing rows to be skipped, got %+v %+v", got[2], got[3])
	}
	if plan.Summary.Import != 1 || plan.Summary.DuplicatePath != 1 || plan.Summary.FileMissing != 1 {
		t.Fatalf("unexpected summary %+v", plan.Summary)
	}
}

func TestBuildPlanRejectsSubSecondStep(t *testing.T) {
	in := baseInput(t, nil, nil)
	in.Step = 500 * time.Millisecond
	if _, err := BuildPlan(context.Background(), in); err == nil {
		t.Fatal("expected sub-second step to be rejected")
	}
}

func TestNormalizeTimestampKeepsNonZeroMilliseconds(t *testing.T) {
	got := normalizeTimestamp(mustTime(t, "2026-11-21T00:30:00Z"))
	if FormatTimestamp(got) != "2026-11-21T00:30:00.900Z" {
		t.Fatalf("got %s", FormatTimestamp(got))
	}
	got = normalizeTimestamp(mustTime(t, "2026-11-21T00:30:00.123456Z"))
	if FormatTimestamp(got) != "2026-11-21T00:30:00.123Z" {
		t.Fatalf("got %s", FormatTimestamp(got))
	}
}

func TestBuildAppendOnlyPlanPreservesExistingRowsAndAppendsAfterAllSourceRows(t *testing.T) {
	tracks := []SourceTrack{
		{Position: 1, ID: "new-a", Path: "/music/src/New A.mp3"},
		{Position: 2, ID: "old-a", Path: "/music/src/Old A.mp3"},
		{Position: 3, ID: "new-b", Path: "/music/src/New B.mp3"},
		{Position: 4, ID: "old-b", Path: "/music/src/Old B.mp3"},
	}
	contents := []Content{
		{ID: "1", FolderPath: tracks[1].Path, CreatedAt: mustTime(t, "2026-11-01T00:00:00.000Z"), StockDate: "2026-10-30"},
		{ID: "2", FolderPath: tracks[3].Path, CreatedAt: mustTime(t, "2026-10-01T00:00:00.100Z"), StockDate: "2026-10-01"},
		{ID: "3", FolderPath: "/music/src/No Longer Remote.mp3", CreatedAt: mustTime(t, "2027-01-01T00:00:00.500Z"), StockDate: "2027-01-01"},
	}
	in := baseInput(t, tracks, contents)
	in.AppendOnly = true
	in.LocalNotRemote = 1
	plan, err := BuildPlan(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Rows[1].Changes() || plan.Rows[3].Changes() || plan.Rows[1].NewStockDate != "2026-10-30" || plan.Rows[1].NewCreatedAt != "2026-11-01T00:00:00.000Z" {
		t.Fatalf("existing rows changed: %+v", plan.Rows)
	}
	if plan.Rows[0].NewCreatedAt != "2027-01-01T00:00:01.500Z" || plan.Rows[2].NewCreatedAt != "2027-01-01T00:00:02.500Z" {
		t.Fatalf("wrong append timeline: %+v", plan.Rows)
	}
	if plan.Summary.CollectionExisting != 3 || plan.Summary.LocalNotRemote != 1 || plan.Summary.DateChanges != 0 || plan.Rows[0].Tags.Genre != "Electro" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if err := VerifyTimeline(plan.TimelineRows()); err != nil {
		t.Fatal(err)
	}
	for _, row := range plan.Rows {
		if row.Status == StatusImport {
			contents = append(contents, Content{ID: row.ID, FolderPath: row.Path, CreatedAt: mustTime(t, row.NewCreatedAt), StockDate: row.NewStockDate})
		}
	}
	in.Contents = contents
	in.ImportTime = in.ImportTime.Add(24 * time.Hour)
	second, err := BuildPlan(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if second.HasChanges() {
		t.Fatalf("second run changes existing rows: %+v", second)
	}
}

func TestBuildAppendOnlyPlanNewImportsFollowNow(t *testing.T) {
	in := baseInput(t, []SourceTrack{{Position: 1, ID: "new", Path: "/music/src/New.mp3"}}, nil)
	in.AppendOnly = true
	plan, err := BuildPlan(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	created := mustTime(t, plan.Rows[0].NewCreatedAt)
	if !created.After(in.ImportTime) {
		t.Fatalf("new import does not follow now: %v", created)
	}
}

package navidrome

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeFavoriteAPI is an in-memory Navidrome whose star operations can be made
// to fail at a chosen point, which is what the compensation path needs.
type fakeFavoriteAPI struct {
	catalog     []Song
	starred     map[string]bool
	failStarAt  int
	starCalls   int
	failUnstar  map[string]bool
	unstarCalls []string
}

func newFakeFavoriteAPI(catalog []Song) *fakeFavoriteAPI {
	api := &fakeFavoriteAPI{catalog: catalog, starred: map[string]bool{}, failStarAt: -1, failUnstar: map[string]bool{}}
	for _, song := range catalog {
		if song.Starred {
			api.starred[song.ID] = true
		}
	}
	return api
}

func (f *fakeFavoriteAPI) Songs(context.Context) ([]Song, error) {
	out := make([]Song, len(f.catalog))
	copy(out, f.catalog)
	for i := range out {
		out[i].Starred = f.starred[out[i].ID]
	}
	return out, nil
}

func (f *fakeFavoriteAPI) Starred(context.Context) ([]Song, error) {
	out := []Song{}
	for _, song := range f.catalog {
		if f.starred[song.ID] {
			copySong := song
			copySong.Starred = true
			out = append(out, copySong)
		}
	}
	return out, nil
}

func (f *fakeFavoriteAPI) Star(_ context.Context, id string) error {
	f.starCalls++
	if f.failStarAt >= 0 && f.starCalls > f.failStarAt {
		return errors.New("server refused the star")
	}
	f.starred[id] = true
	return nil
}

func (f *fakeFavoriteAPI) Unstar(_ context.Context, id string) error {
	f.unstarCalls = append(f.unstarCalls, id)
	if f.failUnstar[id] {
		return errors.New("server refused the unstar")
	}
	delete(f.starred, id)
	return nil
}

type favoriteFixture struct {
	Config   Config
	Resolved Resolved
	Music    string
	Catalog  []Song
	Apple    []AppleFavorite
	Backup   BackupFunc
	BackupAt string
}

func newFavoriteFixture(t *testing.T) *favoriteFixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := DefaultConfig()
	cfg.Server.Username = "jaa"
	normalize(&cfg)
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	music := resolved.MusicDir
	backupPath := filepath.Join(resolved.BackupDir, "navidrome_backup.db")

	fixture := &favoriteFixture{
		Config:   cfg,
		Resolved: resolved,
		Music:    music,
		BackupAt: backupPath,
		Catalog: []Song{
			{ID: "n1", Title: "One", Artist: "A", Path: filepath.Join(music, "one.mp3")},
			{ID: "n2", Title: "Two", Artist: "B", Path: filepath.Join(music, "two.mp3")},
			{ID: "n3", Title: "Three", Artist: "C", Path: filepath.Join(music, "three.mp3"), Starred: true},
		},
		Apple: []AppleFavorite{
			{PersistentID: "a1", Title: "One", Artist: "A", Path: filepath.Join(music, "one.mp3")},
			{PersistentID: "a2", Title: "Two", Artist: "B", Path: filepath.Join(music, "two.mp3")},
			{PersistentID: "a3", Title: "Three", Artist: "C", Path: filepath.Join(music, "three.mp3")},
		},
	}
	fixture.Backup = func(context.Context) (Backup, error) {
		if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
			return Backup{}, err
		}
		if err := os.WriteFile(backupPath, []byte("database"), 0o600); err != nil {
			return Backup{}, err
		}
		return Backup{Path: backupPath, SizeBytes: 8, CreatedAt: time.Now().UTC()}, nil
	}
	return fixture
}

func (f *favoriteFixture) plan(t *testing.T) FavoritePlan {
	t.Helper()
	plan, err := BuildFavoritePlan(context.Background(), f.Config, f.Resolved, f.Apple, f.Catalog,
		time.Unix(1700000000, 0).UTC())
	if err != nil {
		t.Fatalf("BuildFavoritePlan: %v", err)
	}
	return plan
}

func TestFavoritePlanClassifiesEveryRow(t *testing.T) {
	f := newFavoriteFixture(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.mp3")
	f.Apple = append(f.Apple,
		AppleFavorite{PersistentID: "a4", Title: "Missing", Artist: "D", Path: filepath.Join(f.Music, "gone.mp3")},
		AppleFavorite{PersistentID: "a5", Title: "Outside", Artist: "E", Path: outside},
		AppleFavorite{PersistentID: "a6", Title: "Cloud only", Artist: "F"},
	)
	// A metadata-only candidate: the Navidrome file lives at a different path.
	f.Catalog = append(f.Catalog, Song{ID: "n9", Title: "Missing", Artist: "D", Path: filepath.Join(f.Music, "renamed.mp3")})

	plan := f.plan(t)
	if plan.Counts.Matched != 2 {
		t.Fatalf("matched = %d, want 2", plan.Counts.Matched)
	}
	if plan.Counts.AlreadyStarred != 1 {
		t.Fatalf("already starred = %d", plan.Counts.AlreadyStarred)
	}
	if plan.Counts.OutsideLibrary != 2 {
		t.Fatalf("outside = %d, want the elsewhere file and the cloud-only track", plan.Counts.OutsideLibrary)
	}
	if plan.Counts.MetadataOnly != 1 {
		t.Fatalf("metadata only = %d", plan.Counts.MetadataOnly)
	}
	if plan.Counts.ExpectedStarred != 3 {
		t.Fatalf("expected starred = %d", plan.Counts.ExpectedStarred)
	}
	// The metadata-only row must never reach the apply set.
	for _, id := range plan.StarIDs() {
		if id == "n9" {
			t.Fatalf("a metadata-only match must never be applied")
		}
	}
}

func TestFavoritePlanReportsAmbiguity(t *testing.T) {
	f := newFavoriteFixture(t)
	f.Catalog = append(f.Catalog, Song{ID: "dup", Title: "One", Artist: "A", Path: filepath.Join(f.Music, "one.mp3")})
	plan := f.plan(t)
	if plan.Counts.Ambiguous != 1 {
		t.Fatalf("ambiguous = %d", plan.Counts.Ambiguous)
	}
	for _, id := range plan.StarIDs() {
		if id == "n1" || id == "dup" {
			t.Fatalf("an ambiguous row must not be applied")
		}
	}
	if !containsFragment(plan.Warnings, "more than one") {
		t.Fatalf("warnings = %v", plan.Warnings)
	}
}

func TestFavoritePlanBlocksOnEmptyLibrary(t *testing.T) {
	f := newFavoriteFixture(t)
	f.Catalog = nil
	plan := f.plan(t)
	if plan.Applicable() {
		t.Fatalf("an unscanned library must block apply")
	}
	if !containsFragment(plan.Blockers, "scan it") {
		t.Fatalf("blockers = %v", plan.Blockers)
	}
}

func TestFavoritePlanChecksumRejectsTampering(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	if err := VerifyFavoritePlanChecksum(plan); err != nil {
		t.Fatalf("VerifyFavoritePlanChecksum: %v", err)
	}
	plan.Rows[0].NavidromeID = "n2"
	if err := VerifyFavoritePlanChecksum(plan); err == nil {
		t.Fatalf("a modified plan must fail verification")
	}
}

func TestFavoritePlanFileRoundTrip(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	path := filepath.Join(t.TempDir(), "favorites.json")
	if err := WriteFavoritePlan(path, plan); err != nil {
		t.Fatalf("WriteFavoritePlan: %v", err)
	}
	reread, err := ReadFavoritePlan(path)
	if err != nil {
		t.Fatalf("ReadFavoritePlan: %v", err)
	}
	if reread.ChecksumSHA256 != plan.ChecksumSHA256 {
		t.Fatalf("checksum changed across the round trip")
	}
}

func TestApplyStarsExactMatchesAndVerifiesParity(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)

	result, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, f.Backup)
	if err != nil {
		t.Fatalf("ApplyFavoritePlan: %v", err)
	}
	if len(result.NewlyStarred) != 2 {
		t.Fatalf("newly starred = %v", result.NewlyStarred)
	}
	if !result.ParityVerified {
		t.Fatalf("parity was not verified")
	}
	if result.FinalStarred != 3 {
		t.Fatalf("final starred = %d, want 3", result.FinalStarred)
	}
	if result.BackupPath != f.BackupAt {
		t.Fatalf("backup path = %q", result.BackupPath)
	}
}

func TestApplyIsIdempotent(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)
	if _, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, f.Backup); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	// Re-planning against the now-starred server yields nothing left to do.
	catalog, err := api.Songs(context.Background())
	if err != nil {
		t.Fatalf("Songs: %v", err)
	}
	second, err := BuildFavoritePlan(context.Background(), f.Config, f.Resolved, f.Apple, catalog, time.Now())
	if err != nil {
		t.Fatalf("re-plan: %v", err)
	}
	if second.Counts.Matched != 0 || second.Counts.AlreadyStarred != 3 {
		t.Fatalf("second plan = %+v", second.Counts)
	}
	result, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, second, f.Apple, api, f.Backup)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if len(result.NewlyStarred) != 0 {
		t.Fatalf("a re-apply must change nothing: %v", result.NewlyStarred)
	}
}

func TestApplyRejectsChangedAppleFavorites(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)
	changed := append([]AppleFavorite(nil), f.Apple...)
	changed = append(changed, AppleFavorite{PersistentID: "a9", Title: "New", Path: filepath.Join(f.Music, "new.mp3")})

	_, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, changed, api, f.Backup)
	if err == nil || !strings.Contains(err.Error(), "Apple Music favorites changed") {
		t.Fatalf("error = %v", err)
	}
	if api.starCalls != 0 {
		t.Fatalf("a stale plan must not star anything")
	}
}

func TestApplyRejectsChangedNavidromeLibrary(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(append(f.Catalog, Song{ID: "n4", Title: "Four", Path: filepath.Join(f.Music, "four.mp3")}))

	_, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, f.Backup)
	if err == nil || !strings.Contains(err.Error(), "Navidrome library changed") {
		t.Fatalf("error = %v", err)
	}
	if api.starCalls != 0 {
		t.Fatalf("a stale plan must not star anything")
	}
}

func TestApplyRejectsMismatchedAccount(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)
	f.Config.Server.Username = "someone-else"

	_, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, f.Backup)
	if err == nil || !strings.Contains(err.Error(), "account") {
		t.Fatalf("error = %v", err)
	}
}

func TestApplyRefusesWithoutABackup(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)

	_, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, nil)
	if err == nil || !strings.Contains(err.Error(), "backup is required") {
		t.Fatalf("error = %v", err)
	}
	if api.starCalls != 0 {
		t.Fatalf("nothing may be starred without a backup")
	}
}

func TestApplyRefusesWhenTheBackupFails(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)
	failing := func(context.Context) (Backup, error) { return Backup{}, errors.New("disk full") }

	_, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, failing)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v", err)
	}
	if api.starCalls != 0 {
		t.Fatalf("nothing may be starred when the backup fails")
	}
}

func TestApplyRefusesWhenTheBackupFileIsEmpty(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)
	// A backup that reports success but leaves a zero-byte file is worse than
	// no backup, because it looks like protection.
	empty := func(context.Context) (Backup, error) {
		if err := os.MkdirAll(filepath.Dir(f.BackupAt), 0o755); err != nil {
			return Backup{}, err
		}
		if err := os.WriteFile(f.BackupAt, nil, 0o600); err != nil {
			return Backup{}, err
		}
		return Backup{Path: f.BackupAt}, nil
	}
	_, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, empty)
	if err == nil || !strings.Contains(err.Error(), "missing or empty") {
		t.Fatalf("error = %v", err)
	}
	if api.starCalls != 0 {
		t.Fatalf("nothing may be starred behind an empty backup")
	}
}

func TestApplyCompensatesOnlyItsOwnStars(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)
	api.failStarAt = 1 // the first star succeeds, the second fails

	result, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, f.Backup)
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("error = %v", err)
	}
	if len(result.Compensated) != 1 {
		t.Fatalf("compensated = %v", result.Compensated)
	}
	// The pre-existing star must survive untouched.
	if !api.starred["n3"] {
		t.Fatalf("compensation removed a star it did not create")
	}
	for _, id := range api.unstarCalls {
		if id == "n3" {
			t.Fatalf("compensation must never touch a pre-existing star")
		}
	}
	if api.starred["n1"] {
		t.Fatalf("the star added by this attempt was not rolled back")
	}
}

func TestApplyReportsTheBackupWhenCompensationFails(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)
	api.failStarAt = 1
	api.failUnstar["n1"] = true

	result, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, f.Backup)
	if err == nil || !strings.Contains(err.Error(), "restore the backup") {
		t.Fatalf("error = %v", err)
	}
	if result.RecoveryCommand == "" {
		t.Fatalf("an unrecoverable state must carry a recovery instruction")
	}
	if !strings.Contains(result.RecoveryCommand, f.BackupAt) {
		t.Fatalf("recovery = %q, want the backup path", result.RecoveryCommand)
	}
}

func TestApplyRollsBackOnCancellation(t *testing.T) {
	f := newFavoriteFixture(t)
	plan := f.plan(t)
	api := newFakeFavoriteAPI(f.Catalog)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after the first star lands.
	original := api.failStarAt
	_ = original
	starred := 0
	wrapped := &cancelAfterAPI{fakeFavoriteAPI: api, after: 1, cancel: cancel, count: &starred}

	result, err := ApplyFavoritePlan(ctx, f.Config, f.Resolved, plan, f.Apple, wrapped, f.Backup)
	if err == nil {
		t.Fatalf("expected a cancellation failure")
	}
	if len(result.Compensated) != 1 {
		t.Fatalf("compensation must run even when the context is canceled: %+v", result)
	}
	if api.starred["n1"] {
		t.Fatalf("the star added before cancellation was not rolled back")
	}
}

type cancelAfterAPI struct {
	*fakeFavoriteAPI
	after  int
	cancel context.CancelFunc
	count  *int
}

func (c *cancelAfterAPI) Star(ctx context.Context, id string) error {
	if err := c.fakeFavoriteAPI.Star(ctx, id); err != nil {
		return err
	}
	*c.count++
	if *c.count >= c.after {
		c.cancel()
	}
	return nil
}

func TestApplyNeverTouchesAppleMusic(t *testing.T) {
	// The migration takes Apple favorites as a plain value and returns nothing
	// for Apple Music, so there is no code path that could write back. This
	// test pins that shape: if a future change adds an Apple writer, the
	// signature below stops compiling.
	var _ func(context.Context, Config, Resolved, FavoritePlan, []AppleFavorite, FavoriteAPI, BackupFunc) (FavoriteApplyResult, error) = ApplyFavoritePlan

	f := newFavoriteFixture(t)
	plan := f.plan(t)
	before := append([]AppleFavorite(nil), f.Apple...)
	api := newFakeFavoriteAPI(f.Catalog)
	if _, err := ApplyFavoritePlan(context.Background(), f.Config, f.Resolved, plan, f.Apple, api, f.Backup); err != nil {
		t.Fatalf("ApplyFavoritePlan: %v", err)
	}
	if len(f.Apple) != len(before) {
		t.Fatalf("the Apple favorite set was mutated")
	}
	for i := range before {
		if f.Apple[i] != before[i] {
			t.Fatalf("the Apple favorite set was mutated at %d", i)
		}
	}
}

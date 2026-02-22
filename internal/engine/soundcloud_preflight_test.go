package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func TestParseSoundCloudTrackList(t *testing.T) {
	payload := []byte("111\tTrack One\thttps://soundcloud.com/u/one\n222\tTrack Two\thttps://soundcloud.com/u/two\n")
	tracks := parseSoundCloudTrackList(payload)
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}
	if tracks[0].ID != "111" || tracks[1].Title != "Track Two" {
		t.Fatalf("unexpected tracks parsed: %+v", tracks)
	}
}

func TestBuildSoundCloudPreflightBreakMode(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	existingFile := filepath.Join(targetDir, "track-two.m4a")
	if err := os.WriteFile(existingFile, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write existing file: %v", err)
	}

	remote := []soundCloudRemoteTrack{
		{ID: "111", Title: "One"},
		{ID: "222", Title: "Two"},
		{ID: "333", Title: "Three"},
	}
	state := soundCloudSyncState{
		Entries: []soundCloudSyncEntry{
			{RawLine: "soundcloud 111 missing-one.m4a", ID: "111", FilePath: "missing-one.m4a"},
			{RawLine: "soundcloud 222 " + existingFile, ID: "222", FilePath: existingFile},
		},
		ByID: map[string]soundCloudSyncEntry{
			"111": {RawLine: "soundcloud 111 missing-one.m4a", ID: "111", FilePath: "missing-one.m4a"},
			"222": {RawLine: "soundcloud 222 " + existingFile, ID: "222", FilePath: existingFile},
		},
	}

	preflight, gaps, knownGaps, _ := buildSoundCloudPreflight(remote, state, idSet{}, targetDir, SoundCloudModeBreak)
	if preflight.RemoteTotal != 3 || preflight.KnownCount != 2 {
		t.Fatalf("unexpected totals: %+v", preflight)
	}
	if preflight.FirstExistingIndex != 2 {
		t.Fatalf("expected first existing index 2, got %d", preflight.FirstExistingIndex)
	}
	if preflight.PlannedDownloadCount != 1 {
		t.Fatalf("expected one planned download before first local existing, got %+v", preflight)
	}
	if _, ok := gaps["333"]; !ok {
		t.Fatalf("expected archive gap for 333")
	}
	if _, ok := knownGaps["111"]; !ok {
		t.Fatalf("expected known gap marker for 111")
	}
	if preflight.KnownGapCount != 1 {
		t.Fatalf("expected known gap count of 1, got %+v", preflight)
	}
}

func TestBuildSoundCloudPreflightScanMode(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	remote := []soundCloudRemoteTrack{
		{ID: "111"},
		{ID: "222"},
		{ID: "333"},
	}
	state := soundCloudSyncState{
		Entries: []soundCloudSyncEntry{
			{RawLine: "soundcloud 111 missing-one.m4a", ID: "111", FilePath: "missing-one.m4a"},
		},
		ByID: map[string]soundCloudSyncEntry{
			"111": {RawLine: "soundcloud 111 missing-one.m4a", ID: "111", FilePath: "missing-one.m4a"},
		},
	}

	preflight, _, _, _ := buildSoundCloudPreflight(remote, state, idSet{}, targetDir, SoundCloudModeScanGaps)
	if preflight.PlannedDownloadCount != 3 {
		t.Fatalf("expected scan mode to include archive + known gaps, got %+v", preflight)
	}
	if preflight.KnownGapCount != 1 {
		t.Fatalf("expected one known gap in scan mode, got %+v", preflight)
	}
}

func TestBuildSoundCloudPreflightUsesArchiveWhenSyncMissing(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	remote := []soundCloudRemoteTrack{
		{ID: "111"},
		{ID: "222"},
		{ID: "333"},
	}
	state := soundCloudSyncState{
		Entries: []soundCloudSyncEntry{},
		ByID:    map[string]soundCloudSyncEntry{},
	}
	archiveKnown := idSet{"111": {}, "222": {}}

	preflight, gaps, knownGaps, _ := buildSoundCloudPreflight(remote, state, archiveKnown, targetDir, SoundCloudModeBreak)
	if preflight.KnownCount != 2 {
		t.Fatalf("expected known count from archive to be 2, got %+v", preflight)
	}
	if preflight.FirstExistingIndex != 0 {
		t.Fatalf("expected no local existing match, got %+v", preflight)
	}
	if preflight.ArchiveGapCount != 1 {
		t.Fatalf("expected one gap, got %+v", preflight)
	}
	if len(knownGaps) != 2 {
		t.Fatalf("expected two known gaps from archive-only entries, got %v", knownGaps)
	}
	if _, ok := gaps["333"]; !ok {
		t.Fatalf("expected gap for id 333")
	}
	if preflight.PlannedDownloadCount != 3 {
		t.Fatalf("expected break mode to plan all tracks when no local existing is present, got %+v", preflight)
	}
}

func TestBuildSoundCloudPreflightArchiveDeletionPlannedBeforeFirstLocalExisting(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "Track Two.m4a"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("write track two: %v", err)
	}

	remote := []soundCloudRemoteTrack{
		{ID: "111", Title: "Track One"},
		{ID: "222", Title: "Track Two"},
	}
	state := soundCloudSyncState{
		Entries: []soundCloudSyncEntry{},
		ByID:    map[string]soundCloudSyncEntry{},
	}
	archiveKnown := idSet{"111": {}, "222": {}}

	preflight, _, knownGaps, _ := buildSoundCloudPreflight(remote, state, archiveKnown, targetDir, SoundCloudModeBreak)
	if preflight.FirstExistingIndex != 2 {
		t.Fatalf("expected first existing index 2 from local file match, got %+v", preflight)
	}
	if preflight.PlannedDownloadCount != 1 {
		t.Fatalf("expected one planned redownload before first local existing, got %+v", preflight)
	}
	if _, ok := knownGaps["111"]; !ok {
		t.Fatalf("expected known gap marker for deleted track one")
	}
}

func TestParseSoundCloudArchive(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "archive.txt")
	payload := "soundcloud 111\nsoundcloud 222\nyoutube 333\nbadline\n"
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("write archive file: %v", err)
	}

	known, err := parseSoundCloudArchive(path)
	if err != nil {
		t.Fatalf("parse archive file: %v", err)
	}
	if len(known) != 2 {
		t.Fatalf("expected 2 soundcloud ids in archive, got %d", len(known))
	}
	if _, ok := known["111"]; !ok {
		t.Fatalf("expected id 111 in archive set")
	}
}

func TestResolveSoundCloudArchivePathFromCustomYTDLPArgs(t *testing.T) {
	source := config.Source{
		ID:      "sc-a",
		Adapter: config.AdapterSpec{ExtraArgs: []string{"--yt-dlp-args", "--download-archive custom-archive.txt"}},
	}
	defaults := config.Defaults{
		StateDir: "/tmp/state",
	}

	path, err := resolveSoundCloudArchivePath(source, defaults)
	if err != nil {
		t.Fatalf("resolve archive path: %v", err)
	}
	expected := filepath.Clean("/tmp/state/sc-a.custom-archive.txt")
	if path != expected {
		t.Fatalf("unexpected archive path. got=%q want=%q", path, expected)
	}
}

func TestWriteFilteredSyncStateFileDropsMissingIDs(t *testing.T) {
	tmp := t.TempDir()
	original := filepath.Join(tmp, "source.sync.scdl")
	state := soundCloudSyncState{
		Entries: []soundCloudSyncEntry{
			{RawLine: "soundcloud 111 /tmp/one.m4a", ID: "111", FilePath: "/tmp/one.m4a"},
			{RawLine: "soundcloud 222 /tmp/two.m4a", ID: "222", FilePath: "/tmp/two.m4a"},
		},
		ByID: map[string]soundCloudSyncEntry{
			"111": {RawLine: "soundcloud 111 /tmp/one.m4a", ID: "111", FilePath: "/tmp/one.m4a"},
			"222": {RawLine: "soundcloud 222 /tmp/two.m4a", ID: "222", FilePath: "/tmp/two.m4a"},
		},
	}
	remove := map[string]struct{}{"111": {}}
	temp, err := writeFilteredSyncStateFile(original, state, remove)
	if err != nil {
		t.Fatalf("write filtered sync state: %v", err)
	}
	defer os.Remove(temp)

	payload, err := os.ReadFile(temp)
	if err != nil {
		t.Fatalf("read temp sync file: %v", err)
	}
	text := string(payload)
	if text == "" {
		t.Fatalf("expected non-empty sync file")
	}
	if filepath.Base(temp) == filepath.Base(original) {
		t.Fatalf("expected temp file path, got %s", temp)
	}
	if strings.Contains(text, "soundcloud 111") {
		t.Fatalf("expected dropped id 111, got %q", text)
	}
	if !strings.Contains(text, "soundcloud 222") {
		t.Fatalf("expected kept id 222, got %q", text)
	}
}

func TestWriteFilteredArchiveFileDropsMissingIDs(t *testing.T) {
	tmp := t.TempDir()
	original := filepath.Join(tmp, "source.archive.txt")
	payload := "soundcloud 111\nsoundcloud 222\nyoutube abc\n"
	if err := os.WriteFile(original, []byte(payload), 0o644); err != nil {
		t.Fatalf("write archive source: %v", err)
	}

	remove := map[string]struct{}{"111": {}}
	temp, err := writeFilteredArchiveFile(original, remove)
	if err != nil {
		t.Fatalf("write filtered archive: %v", err)
	}
	defer os.Remove(temp)

	filtered, err := os.ReadFile(temp)
	if err != nil {
		t.Fatalf("read filtered archive: %v", err)
	}
	text := string(filtered)
	if strings.Contains(text, "soundcloud 111") {
		t.Fatalf("expected id 111 to be removed, got %q", text)
	}
	if !strings.Contains(text, "soundcloud 222") {
		t.Fatalf("expected id 222 to be kept, got %q", text)
	}
	if !strings.Contains(text, "youtube abc") {
		t.Fatalf("expected non-soundcloud archive lines to be preserved, got %q", text)
	}
}

func TestNeedsSoundCloudLocalIndex(t *testing.T) {
	remote := []soundCloudRemoteTrack{
		{ID: "111"},
		{ID: "222"},
	}
	state := soundCloudSyncState{
		ByID: map[string]soundCloudSyncEntry{
			"111": {ID: "111", FilePath: "/tmp/one.m4a"},
		},
	}
	archiveKnown := idSet{
		"111": {},
	}
	if needsSoundCloudLocalIndex(remote, state, archiveKnown) {
		t.Fatalf("did not expect local index scan when no archive-only known entries exist")
	}

	archiveKnown["222"] = struct{}{}
	if !needsSoundCloudLocalIndex(remote, state, archiveKnown) {
		t.Fatalf("expected local index scan when archive-only known entries exist")
	}
}

func TestLoadSoundCloudLocalIndexStageUsesCacheWhenValid(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "Track One.m4a"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write track: %v", err)
	}

	first, err := loadSoundCloudLocalIndexStage(soundCloudLocalIndexStageInput{
		SourceID:  "sc-a",
		TargetDir: targetDir,
		StateDir:  stateDir,
		NeedScan:  true,
		UseCache:  true,
	})
	if err != nil {
		t.Fatalf("first stage run: %v", err)
	}
	if !first.Scanned || first.CacheHit {
		t.Fatalf("expected first run to scan and miss cache, got %+v", first)
	}
	if first.Index["track one"] != 1 {
		t.Fatalf("expected cached index key for track one, got %+v", first.Index)
	}

	second, err := loadSoundCloudLocalIndexStage(soundCloudLocalIndexStageInput{
		SourceID:  "sc-a",
		TargetDir: targetDir,
		StateDir:  stateDir,
		NeedScan:  true,
		UseCache:  true,
	})
	if err != nil {
		t.Fatalf("second stage run: %v", err)
	}
	if second.Scanned || !second.CacheHit {
		t.Fatalf("expected second run to hit cache without scan, got %+v", second)
	}
	if second.Index["track one"] != 1 {
		t.Fatalf("expected cached index key for track one, got %+v", second.Index)
	}
}

func TestLoadSoundCloudLocalIndexStageRebuildsOnSignatureChange(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "Track One.m4a"), []byte("a"), 0o644); err != nil {
		t.Fatalf("write track: %v", err)
	}

	if _, err := loadSoundCloudLocalIndexStage(soundCloudLocalIndexStageInput{
		SourceID:  "sc-a",
		TargetDir: targetDir,
		StateDir:  stateDir,
		NeedScan:  true,
		UseCache:  true,
	}); err != nil {
		t.Fatalf("initial stage run: %v", err)
	}

	if err := os.WriteFile(filepath.Join(targetDir, "Track Two.m4a"), []byte("b"), 0o644); err != nil {
		t.Fatalf("write second track: %v", err)
	}

	afterChange, err := loadSoundCloudLocalIndexStage(soundCloudLocalIndexStageInput{
		SourceID:  "sc-a",
		TargetDir: targetDir,
		StateDir:  stateDir,
		NeedScan:  true,
		UseCache:  true,
	})
	if err != nil {
		t.Fatalf("stage after signature change: %v", err)
	}
	if !afterChange.Scanned || afterChange.CacheHit {
		t.Fatalf("expected cache miss and rebuild after signature change, got %+v", afterChange)
	}
	if afterChange.Index["track two"] != 1 {
		t.Fatalf("expected rebuilt index to include new track, got %+v", afterChange.Index)
	}
}

func TestLoadSoundCloudLocalIndexStageSkipsScanWhenNotNeeded(t *testing.T) {
	tmp := t.TempDir()
	targetDir := filepath.Join(tmp, "target")
	stateDir := filepath.Join(tmp, "state")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "Track One.m4a"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}

	result, err := loadSoundCloudLocalIndexStage(soundCloudLocalIndexStageInput{
		SourceID:  "sc-a",
		TargetDir: targetDir,
		StateDir:  stateDir,
		NeedScan:  false,
		UseCache:  true,
	})
	if err != nil {
		t.Fatalf("load local index stage: %v", err)
	}
	if result.Scanned || result.CacheHit {
		t.Fatalf("expected no scan/cache hit when scan is not needed, got %+v", result)
	}
	if len(result.Index) != 0 {
		t.Fatalf("expected empty index when scan skipped, got %+v", result.Index)
	}
}

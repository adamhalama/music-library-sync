package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSpotifyPlaylistID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "spotify url",
			in:   "https://open.spotify.com/playlist/3yp4tiwWn1r0FE7jtvWhbb?si=abc",
			want: "3yp4tiwWn1r0FE7jtvWhbb",
		},
		{
			name: "spotify uri",
			in:   "spotify:playlist:3yp4tiwWn1r0FE7jtvWhbb",
			want: "3yp4tiwWn1r0FE7jtvWhbb",
		},
	}

	for _, tc := range tests {
		got, err := resolveSpotifyPlaylistID(tc.in)
		if err != nil {
			t.Fatalf("%s: resolve playlist id: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: unexpected playlist id. got=%q want=%q", tc.name, got, tc.want)
		}
	}
}

func TestBuildSpotifyPreflightBreakMode(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "artist-2 - track-2.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write local media: %v", err)
	}

	remote := []spotifyRemoteTrack{
		{ID: "1abc234def", Title: "track-1", Artist: "artist-1"},
		{ID: "2abc234def", Title: "track-2", Artist: "artist-2"},
		{ID: "3abc234def", Title: "track-3", Artist: "artist-3"},
	}
	state := spotifySyncState{
		KnownIDs: map[string]struct{}{
			"2abc234def": {},
		},
	}

	preflight, archiveGaps, knownGaps, planned, existing, _ := buildSpotifyPreflight(remote, state, tmp, SoundCloudModeBreak)
	if preflight.RemoteTotal != 3 || preflight.KnownCount != 1 {
		t.Fatalf("unexpected preflight counts: %+v", preflight)
	}
	if preflight.FirstExistingIndex != 2 {
		t.Fatalf("expected first existing index 2, got %d", preflight.FirstExistingIndex)
	}
	if preflight.ArchiveGapCount != 2 || preflight.KnownGapCount != 0 {
		t.Fatalf("unexpected gap counts: %+v", preflight)
	}
	if preflight.PlannedDownloadCount != 1 {
		t.Fatalf("expected one planned download before first known track, got %+v", preflight)
	}
	if _, ok := archiveGaps["1abc234def"]; !ok {
		t.Fatalf("expected archive gap id 1abc234def")
	}
	if _, ok := archiveGaps["3abc234def"]; !ok {
		t.Fatalf("expected archive gap id 3abc234def")
	}
	if len(knownGaps) != 0 {
		t.Fatalf("did not expect known gaps when local file exists, got %+v", knownGaps)
	}
	if len(planned) != 1 || planned[0] != "1abc234def" {
		t.Fatalf("unexpected planned ids: %v", planned)
	}
	if len(existing) != 1 || existing[0] != "2abc234def" {
		t.Fatalf("expected one existing known id, got %v", existing)
	}
}

func TestBuildSpotifyPreflightScanMode(t *testing.T) {
	tmp := t.TempDir()

	remote := []spotifyRemoteTrack{
		{ID: "1abc234def", Title: "track-1", Artist: "artist-1"},
		{ID: "2abc234def", Title: "track-2", Artist: "artist-2"},
		{ID: "3abc234def", Title: "track-3", Artist: "artist-3"},
	}
	state := spotifySyncState{
		KnownIDs: map[string]struct{}{
			"2abc234def": {},
		},
	}

	preflight, archiveGaps, knownGaps, planned, existing, _ := buildSpotifyPreflight(remote, state, tmp, SoundCloudModeScanGaps)
	if preflight.PlannedDownloadCount != 3 {
		t.Fatalf("expected three planned downloads (archive + known gaps), got %+v", preflight)
	}
	if preflight.KnownGapCount != 1 || preflight.ArchiveGapCount != 2 {
		t.Fatalf("unexpected gap counts in scan mode, got %+v", preflight)
	}
	if _, ok := knownGaps["2abc234def"]; !ok {
		t.Fatalf("expected known gap for missing local known track")
	}
	if _, ok := archiveGaps["1abc234def"]; !ok {
		t.Fatalf("expected archive gap for unknown id 1abc234def")
	}
	if len(planned) != 3 || planned[0] != "1abc234def" || planned[1] != "2abc234def" || planned[2] != "3abc234def" {
		t.Fatalf("unexpected planned ids: %v", planned)
	}
	if len(existing) != 0 {
		t.Fatalf("did not expect existing known ids in scan gaps test, got %v", existing)
	}
}

func TestBuildSpotifyPreflightUsesStateLocalPathWhenPresent(t *testing.T) {
	tmp := t.TempDir()
	nested := filepath.Join(tmp, "library")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "Permean.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write local media: %v", err)
	}

	remote := []spotifyRemoteTrack{
		{ID: "41gXFhitx4whS6PsoXREzy", Title: "41gXFhitx4whS6PsoXREzy"},
	}
	state := spotifySyncState{
		KnownIDs: map[string]struct{}{
			"41gXFhitx4whS6PsoXREzy": {},
		},
		Entries: map[string]spotifyStateEntry{
			"41gXFhitx4whS6PsoXREzy": {LocalPath: "library/Permean.mp3"},
		},
	}

	preflight, _, knownGaps, planned, existing, _ := buildSpotifyPreflight(remote, state, tmp, SoundCloudModeBreak)
	if preflight.FirstExistingIndex != 1 {
		t.Fatalf("expected first existing index from local path metadata, got %+v", preflight)
	}
	if preflight.KnownGapCount != 0 {
		t.Fatalf("expected no known gaps with local path metadata, got %+v", preflight)
	}
	if len(knownGaps) != 0 {
		t.Fatalf("expected no known gap ids, got %+v", knownGaps)
	}
	if len(planned) != 0 {
		t.Fatalf("expected no planned downloads when local path is present, got %v", planned)
	}
	if len(existing) != 1 || existing[0] != "41gXFhitx4whS6PsoXREzy" {
		t.Fatalf("expected existing list to include known local path id, got %v", existing)
	}
}

func TestBuildSpotifyPreflightBackfillsUnknownLocalTrack(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "Maddix - 90s Bitch.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write local media: %v", err)
	}

	remote := []spotifyRemoteTrack{
		{ID: "2GcncPaqXotQIPokzRM5pP", Title: "90s Bitch", Artist: "Maddix"},
	}
	state := spotifySyncState{KnownIDs: map[string]struct{}{}, Entries: map[string]spotifyStateEntry{}}

	preflight, archiveGaps, knownGaps, planned, existing, backfills := buildSpotifyPreflight(remote, state, tmp, SoundCloudModeBreak)
	if preflight.ArchiveGapCount != 0 || preflight.KnownGapCount != 0 || preflight.PlannedDownloadCount != 0 {
		t.Fatalf("expected local unknown track to be treated as present, got %+v", preflight)
	}
	if len(archiveGaps) != 0 || len(knownGaps) != 0 || len(planned) != 0 {
		t.Fatalf("unexpected gaps/planned: archive=%v known=%v planned=%v", archiveGaps, knownGaps, planned)
	}
	if len(existing) != 1 || existing[0] != "2GcncPaqXotQIPokzRM5pP" {
		t.Fatalf("expected existing local id, got %v", existing)
	}
	if len(backfills) != 1 || backfills[0].ID != "2GcncPaqXotQIPokzRM5pP" || backfills[0].LocalPath != "Maddix - 90s Bitch.mp3" {
		t.Fatalf("unexpected backfill entries: %+v", backfills)
	}
}

func TestBuildSpotifyPreflightDoesNotReuseStatePathForSameTitle(t *testing.T) {
	tmp := t.TempDir()
	localName := "Artist - Shared Title.mp3"
	if err := os.WriteFile(filepath.Join(tmp, localName), []byte("x"), 0o644); err != nil {
		t.Fatalf("write local media: %v", err)
	}

	remote := []spotifyRemoteTrack{
		{ID: "known1234567890123456", Title: "Shared Title", Artist: "Artist"},
		{ID: "newer1234567890123456", Title: "Shared Title", Artist: "Artist"},
	}
	state := spotifySyncState{
		KnownIDs: map[string]struct{}{"known1234567890123456": {}},
		Entries: map[string]spotifyStateEntry{
			"known1234567890123456": {DisplayName: "Artist - Shared Title", LocalPath: localName},
		},
	}

	preflight, archiveGaps, knownGaps, planned, existing, backfills := buildSpotifyPreflight(
		remote,
		state,
		tmp,
		SoundCloudModeScanGaps,
	)
	if len(existing) != 1 || existing[0] != "known1234567890123456" {
		t.Fatalf("expected only state-backed track to be present, got %v", existing)
	}
	if _, ok := archiveGaps["newer1234567890123456"]; !ok {
		t.Fatalf("expected same-title unknown track to remain an archive gap")
	}
	if len(knownGaps) != 0 || len(backfills) != 0 {
		t.Fatalf("unexpected known gaps/backfills: known=%v backfills=%+v", knownGaps, backfills)
	}
	if len(planned) != 1 || planned[0] != "newer1234567890123456" {
		t.Fatalf("expected only unknown duplicate-title track to be planned, got %v", planned)
	}
	if preflight.PlannedDownloadCount != 1 {
		t.Fatalf("expected one planned download, got %+v", preflight)
	}
}

func TestBuildSpotifyPreflightMatchesUnicodeFoldedLocalTrack(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "BYØRN - Bass Fusion.mp3"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write local media: %v", err)
	}

	remote := []spotifyRemoteTrack{
		{ID: "1gngfhBp6ffaF83T0dLRfO", Title: "Bass Fusion", Artist: "BYORN"},
	}
	state := spotifySyncState{KnownIDs: map[string]struct{}{}, Entries: map[string]spotifyStateEntry{}}

	preflight, _, _, planned, existing, backfills := buildSpotifyPreflight(remote, state, tmp, SoundCloudModeBreak)
	if preflight.PlannedDownloadCount != 0 || len(planned) != 0 {
		t.Fatalf("expected unicode-folded local match to avoid download, got %+v planned=%v", preflight, planned)
	}
	if len(existing) != 1 || len(backfills) != 1 {
		t.Fatalf("expected existing/backfill match, existing=%v backfills=%+v", existing, backfills)
	}
}

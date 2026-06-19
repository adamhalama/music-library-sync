package playlists

import "testing"

func TestMatchTrackPrefersExactPath(t *testing.T) {
	snapshot := Snapshot{Tracks: []Track{
		{Artist: "One", Title: "Same", Path: "/Music/one.m4a"},
		{Artist: "Two", Title: "Same", Path: "/Music/two.m4a"},
	}}
	got := MatchTrack(snapshot, "/Music/two.m4a", "anything")
	if got.Status != MatchPath || got.Index != 1 {
		t.Fatalf("unexpected match: %#v", got)
	}
}

func TestMatchTrackUsesConservativeMetadata(t *testing.T) {
	snapshot := Snapshot{Tracks: []Track{{Artist: "Artist", Title: "Track"}}}
	got := MatchTrack(snapshot, "", "Artist - Track")
	if got.Status != MatchMetadata || got.Index != 0 {
		t.Fatalf("unexpected match: %#v", got)
	}
}

func TestMatchTrackRejectsAmbiguousMetadata(t *testing.T) {
	snapshot := Snapshot{Tracks: []Track{{Title: "Track"}, {Title: "Track"}}}
	got := MatchTrack(snapshot, "", "Track")
	if got.Status != MatchAmbiguous {
		t.Fatalf("unexpected match: %#v", got)
	}
}

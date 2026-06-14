package music

import "testing"

func TestParsePlaylistTracks(t *testing.T) {
	raw := "PLAYLIST\t70C641CA78BB0F3C\tFavourites\ttrue\t2\n" +
		"TRACK\t1\tAA26\t1690\tAdrian Mills\tINTERGALÁCTICO\tINTERGALÁCTICO EP\t279.33\t/Users/jaa/Music/a.mp3\n" +
		"TRACK\t2\tBB27\t1700\tArtist\tTitle\tAlbum\t180\t/Users/jaa/Music/b.m4a"

	playlist, tracks, err := ParsePlaylistTracks(raw)
	if err != nil {
		t.Fatalf("ParsePlaylistTracks: %v", err)
	}
	if playlist.Name != "Favourites" || playlist.PersistentID != "70C641CA78BB0F3C" || !playlist.Smart || playlist.TrackCount != 2 {
		t.Fatalf("unexpected playlist: %+v", playlist)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %d", len(tracks))
	}
	if tracks[0].Title != "INTERGALÁCTICO" || tracks[0].Path != "/Users/jaa/Music/a.mp3" {
		t.Fatalf("unexpected first track: %+v", tracks[0])
	}
}

func TestParsePlaylistListRejectsMalformedRows(t *testing.T) {
	_, err := ParsePlaylistList("only\tthree\tcolumns")
	if err == nil {
		t.Fatalf("expected malformed playlist row error")
	}
}

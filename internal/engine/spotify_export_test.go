package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSpotifyEmbedPlaylistOrderKeepsPlaylistOrder(t *testing.T) {
	document := `<html><body><script id="__NEXT_DATA__" type="application/json">` +
		`{"props":{"pageProps":{"state":{"data":{"entity":{"trackList":[` +
		`{"uri":"spotify:track:7isVtUtibpSO2eVSkSJcc6","title":"Zion","subtitle":"Fluke"},` +
		`{"uri":"spotify:track:0qPpFSdqIpzgNU6lHy3iF6","title":"Poison Lips","subtitle":"Vitalic"},` +
		`{"uri":"spotify:track:1KeHS6mGuEbwaTcuILtEq1","title":"Abroad","subtitle":"Len Faki, Regal"}` +
		`]}}}}}}</script></body></html>`

	tracks, err := ParseSpotifyEmbedPlaylistOrder(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 3 || tracks[0].Title != "Zion" || tracks[2].ID != "1KeHS6mGuEbwaTcuILtEq1" || tracks[2].Position != 3 {
		t.Fatalf("unexpected order: %+v", tracks)
	}
}

func TestLoadSpotifyStateEntriesReadsV2PathsAndV1IDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slow.sync.spotify")
	payload := "# udl spotify state v2\n" +
		"7isVtUtibpSO2eVSkSJcc6\ttitle=Fluke+-+Zion\tpath=Fluke+-+Zion.mp3\n" +
		"0qPpFSdqIpzgNU6lHy3iF6\n"
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadSpotifyStateEntries(path)
	if err != nil {
		t.Fatal(err)
	}
	if entries["7isVtUtibpSO2eVSkSJcc6"].LocalPath != "Fluke - Zion.mp3" {
		t.Fatalf("expected decoded relative path, got %+v", entries)
	}
	if _, ok := entries["0qPpFSdqIpzgNU6lHy3iF6"]; !ok {
		t.Fatalf("expected v1 ID-only entry, got %+v", entries)
	}
}

package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractSpotifyTrackIDsFromPlaylistHTML(t *testing.T) {
	document := `
		<html>
			<body>
				<a href="spotify:track:41gXFhitx4whS6PsoXREzy">one</a>
				<a href="spotify:track:41gXFhitx4whS6PsoXREzy">dupe</a>
				<a href="spotify:track:5onvWxBJehSONyspmnrvhD">two</a>
			</body>
		</html>
	`
	ids := extractSpotifyTrackIDsFromPlaylistHTML(document)
	if len(ids) != 2 {
		t.Fatalf("expected two unique ids, got %v", ids)
	}
	if ids[0] != "41gXFhitx4whS6PsoXREzy" || ids[1] != "5onvWxBJehSONyspmnrvhD" {
		t.Fatalf("unexpected id order: %v", ids)
	}
}

func TestParseSpotifyTrackMetadataFromHTML(t *testing.T) {
	document := `
		<html>
			<head>
				<meta property="og:title" content="Mr. Brightside" />
				<meta property="og:description" content="The Killers · Hot Fuss · Song · 2004" />
			</head>
		</html>
	`
	metadata, err := parseSpotifyTrackMetadataFromHTML(document)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	if metadata.Title != "Mr. Brightside" {
		t.Fatalf("unexpected title: %q", metadata.Title)
	}
	if metadata.Artist != "The Killers" {
		t.Fatalf("unexpected artist: %q", metadata.Artist)
	}
	if metadata.Album != "Hot Fuss" {
		t.Fatalf("unexpected album: %q", metadata.Album)
	}
}

func TestWriteSpotifyTrackMetadataCache(t *testing.T) {
	runtimeDir := t.TempDir()

	first := spotifyTrackMetadata{
		Title:  "Permean",
		Artist: "Regent",
		Album:  "Permean",
	}
	if err := writeSpotifyTrackMetadataCache(runtimeDir, "41gXFhitx4whS6PsoXREzy", first); err != nil {
		t.Fatalf("write first metadata: %v", err)
	}

	second := spotifyTrackMetadata{
		Title:  "Encoder",
		Artist: "Regent",
		Album:  "Encoder",
	}
	if err := writeSpotifyTrackMetadataCache(runtimeDir, "5onvWxBJehSONyspmnrvhD", second); err != nil {
		t.Fatalf("write second metadata: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(runtimeDir, "config", "spotify", "cache.json"))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}

	var decoded struct {
		Tracks map[string]struct {
			Data struct {
				Title  string `json:"title"`
				Artist string `json:"artist"`
				Album  string `json:"album"`
			} `json:"data"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode cache: %v", err)
	}

	if len(decoded.Tracks) != 2 {
		t.Fatalf("expected two cached tracks, got %+v", decoded.Tracks)
	}
	if decoded.Tracks["41gXFhitx4whS6PsoXREzy"].Data.Title != "Permean" {
		t.Fatalf("unexpected first track cache: %+v", decoded.Tracks["41gXFhitx4whS6PsoXREzy"])
	}
	if decoded.Tracks["5onvWxBJehSONyspmnrvhD"].Data.Title != "Encoder" {
		t.Fatalf("unexpected second track cache: %+v", decoded.Tracks["5onvWxBJehSONyspmnrvhD"])
	}
}

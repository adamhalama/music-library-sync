package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jaa/update-downloads/internal/auth"
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

// The deemix Spotify plugin matches Spotify tracks to Deezer by ISRC. A primed
// cache entry that carries only title/artist/album makes deemix fail every
// download with "Track unavailable on Deezer", so the ISRC must survive into
// the cache file.
func TestWriteSpotifyTrackMetadataCachePersistsISRC(t *testing.T) {
	runtimeDir := t.TempDir()

	metadata := spotifyTrackMetadata{
		Title:  "Breathe",
		Artist: "The Prodigy",
		Album:  "The Fat of the Land",
		ISRC:   "GBBKS9700074",
	}
	if err := writeSpotifyTrackMetadataCache(runtimeDir, "0Ja4hLKiUSw01E01pJ1yGr", metadata); err != nil {
		t.Fatalf("write metadata: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(runtimeDir, "config", "spotify", "cache.json"))
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	var decoded struct {
		Tracks map[string]struct {
			ISRC string `json:"isrc"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode cache: %v", err)
	}
	if got := decoded.Tracks["0Ja4hLKiUSw01E01pJ1yGr"].ISRC; got != "GBBKS9700074" {
		t.Fatalf("expected isrc to be cached, got %q", got)
	}
}

func TestSpotifyMetadataResolverPrefersAPIForISRC(t *testing.T) {
	origToken := fetchSpotifyAccessTokenFn
	origAPI := fetchSpotifyTrackMetadataFromAPIFn
	origPage := fetchSpotifyTrackMetadataFn
	t.Cleanup(func() {
		fetchSpotifyAccessTokenFn = origToken
		fetchSpotifyTrackMetadataFromAPIFn = origAPI
		fetchSpotifyTrackMetadataFn = origPage
	})

	fetchSpotifyAccessTokenFn = func(ctx context.Context, creds auth.SpotifyCredentials) (string, error) {
		return "token", nil
	}
	fetchSpotifyTrackMetadataFromAPIFn = func(ctx context.Context, id, token string) (spotifyTrackMetadata, error) {
		return spotifyTrackMetadata{Title: "Zion", Artist: "Fluke", Album: "Puppy", ISRC: "USMV20300030"}, nil
	}
	fetchSpotifyTrackMetadataFn = func(ctx context.Context, id string) (spotifyTrackMetadata, error) {
		t.Fatalf("page scraping must not run once the api returned an isrc")
		return spotifyTrackMetadata{}, nil
	}

	resolver := newSpotifyMetadataResolver(auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"})
	metadata, err := resolver.Resolve(context.Background(), "7isVtUtibpSO2eVSkSJcc6")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if metadata.ISRC != "USMV20300030" {
		t.Fatalf("unexpected isrc: %q", metadata.ISRC)
	}
}

// When the track endpoint is unavailable the run must still proceed on scraped
// metadata rather than failing outright.
func TestSpotifyMetadataResolverFallsBackToPage(t *testing.T) {
	origToken := fetchSpotifyAccessTokenFn
	origAPI := fetchSpotifyTrackMetadataFromAPIFn
	origPage := fetchSpotifyTrackMetadataFn
	t.Cleanup(func() {
		fetchSpotifyAccessTokenFn = origToken
		fetchSpotifyTrackMetadataFromAPIFn = origAPI
		fetchSpotifyTrackMetadataFn = origPage
	})

	fetchSpotifyAccessTokenFn = func(ctx context.Context, creds auth.SpotifyCredentials) (string, error) {
		return "", errors.New("token unavailable")
	}
	fetchSpotifyTrackMetadataFromAPIFn = func(ctx context.Context, id, token string) (spotifyTrackMetadata, error) {
		t.Fatalf("api lookup must not run without a token")
		return spotifyTrackMetadata{}, nil
	}
	fetchSpotifyTrackMetadataFn = func(ctx context.Context, id string) (spotifyTrackMetadata, error) {
		return spotifyTrackMetadata{Title: "Zion", Artist: "Fluke", Album: "Puppy"}, nil
	}

	resolver := newSpotifyMetadataResolver(auth.SpotifyCredentials{})
	metadata, err := resolver.Resolve(context.Background(), "7isVtUtibpSO2eVSkSJcc6")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if metadata.Title != "Zion" || metadata.Artist != "Fluke" {
		t.Fatalf("unexpected fallback metadata: %+v", metadata)
	}
	if metadata.ISRC != "" {
		t.Fatalf("expected no isrc from page fallback, got %q", metadata.ISRC)
	}
}

// A cached preflight entry without an ISRC is not good enough to prime deemix;
// the execution path must go resolve one.
func TestResolveSpotifyTrackMetadataForExecutionResolvesMissingISRC(t *testing.T) {
	origToken := fetchSpotifyAccessTokenFn
	origAPI := fetchSpotifyTrackMetadataFromAPIFn
	t.Cleanup(func() {
		fetchSpotifyAccessTokenFn = origToken
		fetchSpotifyTrackMetadataFromAPIFn = origAPI
	})

	fetchSpotifyAccessTokenFn = func(ctx context.Context, creds auth.SpotifyCredentials) (string, error) {
		return "token", nil
	}
	apiCalls := 0
	fetchSpotifyTrackMetadataFromAPIFn = func(ctx context.Context, id, token string) (spotifyTrackMetadata, error) {
		apiCalls++
		return spotifyTrackMetadata{Title: "Breathe", Artist: "The Prodigy", Album: "The Fat of the Land", ISRC: "GBBKS9700074"}, nil
	}

	preflight := map[string]spotifyTrackMetadata{
		"0Ja4hLKiUSw01E01pJ1yGr": {Title: "Breathe", Artist: "The Prodigy", Album: "The Fat of the Land"},
	}
	resolver := newSpotifyMetadataResolver(auth.SpotifyCredentials{ClientID: "id", ClientSecret: "secret"})
	metadata, err := resolveSpotifyTrackMetadataForExecution(context.Background(), "0Ja4hLKiUSw01E01pJ1yGr", preflight, resolver)
	if err != nil {
		t.Fatalf("resolve for execution: %v", err)
	}
	if metadata.ISRC != "GBBKS9700074" {
		t.Fatalf("expected resolved isrc, got %q", metadata.ISRC)
	}
	if apiCalls != 1 {
		t.Fatalf("expected exactly one api lookup, got %d", apiCalls)
	}

	// A cached entry that already has an ISRC must not trigger another lookup.
	preflight["0Ja4hLKiUSw01E01pJ1yGr"] = spotifyTrackMetadata{
		Title: "Breathe", Artist: "The Prodigy", Album: "The Fat of the Land", ISRC: "GBBKS9700074",
	}
	if _, err := resolveSpotifyTrackMetadataForExecution(context.Background(), "0Ja4hLKiUSw01E01pJ1yGr", preflight, resolver); err != nil {
		t.Fatalf("resolve cached: %v", err)
	}
	if apiCalls != 1 {
		t.Fatalf("expected cached isrc to skip the api, got %d calls", apiCalls)
	}
}

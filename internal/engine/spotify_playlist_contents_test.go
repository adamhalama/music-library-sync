package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testSpotifyTrackID(i int) string {
	return fmt.Sprintf("trk%07dabcdef", i)
}

// testSpotifyEmbedHTML builds an embed page listing tracks 1..listed.
func testSpotifyEmbedHTML(t *testing.T, listed int, token string) string {
	t.Helper()
	trackList := make([]map[string]string, 0, listed)
	for i := 1; i <= listed; i++ {
		trackList = append(trackList, map[string]string{
			"uri":      "spotify:track:" + testSpotifyTrackID(i),
			"title":    fmt.Sprintf("Title %d", i),
			"subtitle": fmt.Sprintf("Artist %d", i),
		})
	}
	payload := map[string]any{"props": map[string]any{"pageProps": map[string]any{"state": map[string]any{
		"data":     map[string]any{"entity": map[string]any{"trackList": trackList}},
		"settings": map[string]any{"session": map[string]any{"accessToken": token}},
	}}}}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal embed payload: %v", err)
	}
	return `<html><script id="__NEXT_DATA__" type="application/json">` + string(raw) + `</script></html>`
}

// testSpotifyContentsServer serves a playlist of total tracks the way the
// contents endpoint pages it, capping each response at pageCap items.
func testSpotifyContentsServer(t *testing.T, total int, pageCap int, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		length, _ := strconv.Atoi(r.URL.Query().Get("length"))
		if length > pageCap {
			length = pageCap
		}
		items := []map[string]any{}
		for i := from + 1; i <= total && len(items) < length; i++ {
			items = append(items, map[string]any{
				"uri":        "spotify:track:" + testSpotifyTrackID(i),
				"attributes": map[string]any{"timestamp": strconv.FormatInt(int64(1_700_000_000_000+i*1000), 10)},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"length":   total,
			"contents": map[string]any{"pos": from, "items": items},
		})
	}))
}

func withSpotifyPlaylistServers(t *testing.T, embedHTML string, contents *httptest.Server) {
	t.Helper()
	embed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(embedHTML))
	}))
	origEmbed, origContents := spotifyEmbedPlaylistBaseURL, spotifyPlaylistContentsBaseURL
	spotifyEmbedPlaylistBaseURL = embed.URL + "/embed/playlist/"
	if contents != nil {
		spotifyPlaylistContentsBaseURL = contents.URL + "/playlist/v2/playlist/"
	} else {
		spotifyPlaylistContentsBaseURL = embed.URL + "/missing/"
	}
	t.Cleanup(func() {
		spotifyEmbedPlaylistBaseURL, spotifyPlaylistContentsBaseURL = origEmbed, origContents
		embed.Close()
		if contents != nil {
			contents.Close()
		}
	})
}

func TestEnumerateSpotifyPlaylistViaEmbedReadsTracksPastEmbedLimit(t *testing.T) {
	contents := testSpotifyContentsServer(t, 122, 100, "anon-token")
	withSpotifyPlaylistServers(t, testSpotifyEmbedHTML(t, 100, "anon-token"), contents)

	tracks, err := enumerateSpotifyPlaylistTracksViaEmbed(context.Background(), "playlist123")
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(tracks) != 122 {
		t.Fatalf("expected all 122 tracks, got %d", len(tracks))
	}
	first, last := tracks[0], tracks[121]
	if first.ID != testSpotifyTrackID(1) || first.Title != "Title 1" || first.Artist != "Artist 1" || first.Position != 1 {
		t.Fatalf("unexpected first track: %+v", first)
	}
	if last.ID != testSpotifyTrackID(122) || last.Position != 122 || last.Title != last.ID {
		t.Fatalf("unexpected last track: %+v", last)
	}
	if want := time.UnixMilli(1_700_000_000_000 + 122*1000).UTC(); !last.AddedAt.Equal(want) {
		t.Fatalf("expected added time %v, got %v", want, last.AddedAt)
	}
}

func TestEnumerateSpotifyPlaylistViaEmbedFailsWhenListMayBeCutOff(t *testing.T) {
	withSpotifyPlaylistServers(t, testSpotifyEmbedHTML(t, 100, "anon-token"), nil)

	_, err := enumerateSpotifyPlaylistTracksViaPage(context.Background(), "playlist123")
	if !errors.Is(err, errSpotifyPlaylistIncomplete) {
		t.Fatalf("expected incomplete playlist error, got %v", err)
	}
	if !strings.Contains(err.Error(), "first 100 tracks") {
		t.Fatalf("expected error to name the embed limit, got %v", err)
	}
}

func TestEnumerateSpotifyPlaylistViaEmbedKeepsShortListWhenContentsFail(t *testing.T) {
	withSpotifyPlaylistServers(t, testSpotifyEmbedHTML(t, 57, "anon-token"), nil)

	tracks, err := enumerateSpotifyPlaylistTracksViaEmbed(context.Background(), "playlist123")
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(tracks) != 57 || tracks[56].Title != "Title 57" {
		t.Fatalf("expected the 57 embed tracks, got %d", len(tracks))
	}
}

func TestFetchSpotifyPlaylistContentsPagesUntilLength(t *testing.T) {
	contents := testSpotifyContentsServer(t, 250, 100, "anon-token")
	defer contents.Close()
	orig := spotifyPlaylistContentsBaseURL
	spotifyPlaylistContentsBaseURL = contents.URL + "/playlist/v2/playlist/"
	defer func() { spotifyPlaylistContentsBaseURL = orig }()

	items, err := fetchSpotifyPlaylistContents(context.Background(), "playlist123", "anon-token")
	if err != nil {
		t.Fatalf("fetch contents: %v", err)
	}
	if len(items) != 250 || items[249].URI != "spotify:track:"+testSpotifyTrackID(250) {
		t.Fatalf("expected 250 items in order, got %d", len(items))
	}
}

func TestFetchSpotifyPlaylistContentsRequiresToken(t *testing.T) {
	if _, err := fetchSpotifyPlaylistContents(context.Background(), "playlist123", " "); err == nil {
		t.Fatalf("expected an error without a token")
	}
}

func TestMergeSpotifyPlaylistContentsSkipsNonTracksAndDuplicates(t *testing.T) {
	items := []spotifyPlaylistItem{
		{URI: "spotify:track:" + testSpotifyTrackID(1)},
		{URI: "spotify:local:artist:album:title:180"},
		{URI: "spotify:track:" + testSpotifyTrackID(1)},
		{URI: "spotify:track:" + testSpotifyTrackID(2)},
	}
	embed := []spotifyRemoteTrack{{ID: testSpotifyTrackID(1), Title: "Named", Artist: "Someone"}}

	tracks, err := mergeSpotifyPlaylistContents(items, embed)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 tracks, got %+v", tracks)
	}
	if tracks[0].Title != "Named" || tracks[0].Artist != "Someone" || tracks[0].Position != 1 {
		t.Fatalf("unexpected first track: %+v", tracks[0])
	}
	if tracks[1].ID != testSpotifyTrackID(2) || tracks[1].Position != 4 {
		t.Fatalf("expected second track at playlist position 4, got %+v", tracks[1])
	}
}

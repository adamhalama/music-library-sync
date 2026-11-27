package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// spotifyEmbedTrackLimit is the most tracks the public embed page lists.
const spotifyEmbedTrackLimit = 100

// spotifyPlaylistContentsPageSize is how many items one contents request asks for.
const spotifyPlaylistContentsPageSize = 100

var errSpotifyPlaylistIncomplete = errors.New("spotify playlist could not be read in full")

var (
	spotifyEmbedPlaylistBaseURL    = "https://open.spotify.com/embed/playlist/"
	spotifyPlaylistContentsBaseURL = "https://spclient.wg.spotify.com/playlist/v2/playlist/"
	fetchSpotifyPlaylistContentsFn = fetchSpotifyPlaylistContents
)

// spotifyPlaylistItem is one entry of a playlist's stored order.
type spotifyPlaylistItem struct {
	URI     string
	AddedAt time.Time
}

type spotifyPlaylistContentsPage struct {
	Length   int `json:"length"`
	Contents struct {
		Pos   int `json:"pos"`
		Items []struct {
			URI        string `json:"uri"`
			Attributes struct {
				Timestamp string `json:"timestamp"`
			} `json:"attributes"`
		} `json:"items"`
	} `json:"contents"`
}

// fetchSpotifyPlaylistContents reads every item of a playlist in stored order
// with the anonymous token from the embed page. The response carries URIs and
// added times but no titles.
func fetchSpotifyPlaylistContents(ctx context.Context, playlistID string, token string) ([]spotifyPlaylistItem, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("spotify embed page has no access token")
	}
	client := &http.Client{Timeout: 20 * time.Second}
	items := []spotifyPlaylistItem{}
	total := -1
	for total < 0 || len(items) < total {
		pageURL := fmt.Sprintf("%s%s?from=%d&length=%d", spotifyPlaylistContentsBaseURL, playlistID, len(items), spotifyPlaylistContentsPageSize)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create spotify playlist contents request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "udl/spotify-deemix")

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("spotify playlist contents request failed: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read spotify playlist contents response: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("spotify playlist contents request failed: status=%d", resp.StatusCode)
		}

		var page spotifyPlaylistContentsPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode spotify playlist contents response: %w", err)
		}
		if total < 0 {
			total = page.Length
		} else if page.Length != total {
			return nil, fmt.Errorf("spotify playlist changed while reading: length %d, then %d", total, page.Length)
		}
		if page.Contents.Pos != len(items) {
			return nil, fmt.Errorf("spotify playlist contents started at %d, expected %d", page.Contents.Pos, len(items))
		}
		if len(page.Contents.Items) == 0 && len(items) < total {
			return nil, fmt.Errorf("spotify playlist contents stopped at %d of %d items", len(items), total)
		}
		for _, raw := range page.Contents.Items {
			item := spotifyPlaylistItem{URI: strings.TrimSpace(raw.URI)}
			if ms, err := strconv.ParseInt(strings.TrimSpace(raw.Attributes.Timestamp), 10, 64); err == nil && ms > 0 {
				item.AddedAt = time.UnixMilli(ms).UTC()
			}
			items = append(items, item)
		}
	}
	if len(items) != total {
		return nil, fmt.Errorf("spotify playlist contents returned %d of %d items", len(items), total)
	}
	return items, nil
}

// mergeSpotifyPlaylistContents builds the remote track list from the full
// playlist contents, taking titles and artists from the embed page where it
// lists the track. Tracks past the embed limit keep their ID as title, like the
// playlist page fallback; metadata enrichment resolves them later.
func mergeSpotifyPlaylistContents(items []spotifyPlaylistItem, embedTracks []spotifyRemoteTrack) ([]spotifyRemoteTrack, error) {
	named := make(map[string]spotifyRemoteTrack, len(embedTracks))
	for _, track := range embedTracks {
		named[track.ID] = track
	}
	tracks := make([]spotifyRemoteTrack, 0, len(items))
	seen := map[string]struct{}{}
	for i, item := range items {
		if !strings.HasPrefix(item.URI, "spotify:track:") {
			continue
		}
		id := extractSpotifyTrackID(strings.TrimPrefix(item.URI, "spotify:track:"))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		track := spotifyRemoteTrack{
			ID:       id,
			URL:      spotifyTrackURL(id),
			Title:    id,
			AddedAt:  item.AddedAt,
			Position: i + 1,
		}
		if listed, ok := named[id]; ok {
			track.Title = listed.Title
			track.Artist = listed.Artist
		}
		tracks = append(tracks, track)
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("spotify playlist contents did not expose track IDs")
	}
	return tracks, nil
}

// spotifyTracksNewestFirst orders tracks by Spotify added time, newest first.
// Tracks without an added time follow, and ties fall back to playlist
// position, later first: Spotify appends new tracks to the end.
func spotifyTracksNewestFirst(tracks []spotifyRemoteTrack) []spotifyRemoteTrack {
	type indexed struct {
		track spotifyRemoteTrack
		index int
	}
	ordered := make([]indexed, len(tracks))
	for i, track := range tracks {
		ordered[i] = indexed{track: track, index: i}
	}
	slices.SortStableFunc(ordered, func(a, b indexed) int {
		switch {
		case !a.track.AddedAt.IsZero() && !b.track.AddedAt.IsZero() && !a.track.AddedAt.Equal(b.track.AddedAt):
			return b.track.AddedAt.Compare(a.track.AddedAt)
		case !a.track.AddedAt.IsZero() && b.track.AddedAt.IsZero():
			return -1
		case a.track.AddedAt.IsZero() && !b.track.AddedAt.IsZero():
			return 1
		case a.track.Position != b.track.Position:
			return b.track.Position - a.track.Position
		default:
			return b.index - a.index
		}
	})
	out := make([]spotifyRemoteTrack, len(ordered))
	for i, entry := range ordered {
		out[i] = entry.track
	}
	return out
}

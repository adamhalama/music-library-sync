package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/auth"
)

// The deemix Spotify plugin matches a Spotify track to a Deezer track by ISRC.
// Its own Spotify lookup is unreliable (the Web API rejects several endpoints
// for client-credentials tokens), and a primed cache entry that carries only
// title/artist/album makes every download fail with "Track unavailable on
// Deezer". The ISRC therefore has to come from udl, and the single-track
// endpoint is the one that still answers.
type spotifyTrackAPIResponse struct {
	Name    string `json:"name"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album *struct {
		Name string `json:"name"`
	} `json:"album"`
	ExternalIDs struct {
		ISRC string `json:"isrc"`
	} `json:"external_ids"`
}

func fetchSpotifyTrackMetadataFromAPI(ctx context.Context, trackID, token string) (spotifyTrackMetadata, error) {
	id := extractSpotifyTrackID(trackID)
	if id == "" {
		return spotifyTrackMetadata{}, fmt.Errorf("invalid spotify track id %q", trackID)
	}
	if strings.TrimSpace(token) == "" {
		return spotifyTrackMetadata{}, fmt.Errorf("spotify access token is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.spotify.com/v1/tracks/"+id, nil)
	if err != nil {
		return spotifyTrackMetadata{}, fmt.Errorf("create spotify track request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return spotifyTrackMetadata{}, fmt.Errorf("spotify track request failed: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return spotifyTrackMetadata{}, fmt.Errorf("read spotify track response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return spotifyTrackMetadata{}, fmt.Errorf("spotify track request failed: status=%d", resp.StatusCode)
	}

	var decoded spotifyTrackAPIResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return spotifyTrackMetadata{}, fmt.Errorf("decode spotify track response: %w", err)
	}

	metadata := spotifyTrackMetadata{
		Title: strings.TrimSpace(decoded.Name),
		ISRC:  strings.TrimSpace(decoded.ExternalIDs.ISRC),
	}
	if len(decoded.Artists) > 0 {
		metadata.Artist = strings.TrimSpace(decoded.Artists[0].Name)
	}
	if decoded.Album != nil {
		metadata.Album = strings.TrimSpace(decoded.Album.Name)
	}
	if !hasUsableSpotifyMetadata(metadata) {
		return spotifyTrackMetadata{}, fmt.Errorf("spotify track response is missing title/artist")
	}
	return normalizeSpotifyTrackMetadata(metadata), nil
}

// spotifyMetadataResolver resolves per-track metadata, preferring the Spotify
// Web API (the only source that yields an ISRC) and falling back to scraping
// the public track page. The access token is fetched at most once.
type spotifyMetadataResolver struct {
	creds     auth.SpotifyCredentials
	token     string
	tokenDone bool
}

func newSpotifyMetadataResolver(creds auth.SpotifyCredentials) *spotifyMetadataResolver {
	return &spotifyMetadataResolver{creds: creds}
}

func (r *spotifyMetadataResolver) accessToken(ctx context.Context) string {
	if r == nil {
		return ""
	}
	if !r.tokenDone {
		r.tokenDone = true
		if token, err := fetchSpotifyAccessTokenFn(ctx, r.creds); err == nil {
			r.token = strings.TrimSpace(token)
		}
	}
	return r.token
}

// Resolve returns the best metadata available for trackID. An error is only
// returned when no source produced a usable title/artist pair; a result
// without an ISRC is still worth having, because priming the deemix cache with
// correct title/artist prevents deemix from silently downloading an unrelated
// track.
func (r *spotifyMetadataResolver) Resolve(ctx context.Context, trackID string) (spotifyTrackMetadata, error) {
	id := extractSpotifyTrackID(trackID)
	if id == "" {
		return spotifyTrackMetadata{}, fmt.Errorf("invalid spotify track id %q", trackID)
	}

	var apiMetadata spotifyTrackMetadata
	if token := r.accessToken(ctx); token != "" {
		metadata, err := fetchSpotifyTrackMetadataFromAPIFn(ctx, id, token)
		if err == nil {
			if strings.TrimSpace(metadata.ISRC) != "" {
				return metadata, nil
			}
			apiMetadata = metadata
		}
	}

	pageMetadata, pageErr := fetchSpotifyTrackMetadataFn(ctx, id)
	if pageErr == nil {
		pageMetadata.ISRC = strings.TrimSpace(apiMetadata.ISRC)
		return normalizeSpotifyTrackMetadata(pageMetadata), nil
	}
	if hasUsableSpotifyMetadata(apiMetadata) {
		return apiMetadata, nil
	}
	return spotifyTrackMetadata{}, pageErr
}

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/auth"
)

// spotifyDeezerLookup maps Spotify track IDs to Deezer track IDs by ISRC.
//
// deemix's Spotify plugin can silently resolve a Spotify link to an unrelated
// Deezer track (or report it unavailable) when its own metadata lookup fails.
// Handing deemix a Deezer URL resolved by ISRC avoids both failure modes.
type spotifyDeezerLookup struct {
	creds      auth.SpotifyCredentials
	token      string
	tokenErr   error
	tokenReady bool
	fetchToken func(context.Context, auth.SpotifyCredentials) (string, error)
	client     *http.Client
}

func newSpotifyDeezerLookup(creds auth.SpotifyCredentials) *spotifyDeezerLookup {
	return &spotifyDeezerLookup{
		creds:      creds,
		fetchToken: fetchSpotifyAccessToken,
		client:     &http.Client{Timeout: 20 * time.Second},
	}
}

// lookupDeezerTrackForSpotifyFn returns the Deezer track ID for a Spotify track.
// found=false with a nil error means Deezer has no recording with that ISRC.
var lookupDeezerTrackForSpotifyFn = func(ctx context.Context, lookup *spotifyDeezerLookup, spotifyTrackID string) (string, bool, error) {
	return lookup.resolve(ctx, spotifyTrackID)
}

func deezerTrackURL(deezerID string) string {
	return "https://www.deezer.com/track/" + deezerID
}

func (l *spotifyDeezerLookup) resolve(ctx context.Context, spotifyTrackID string) (string, bool, error) {
	id := extractSpotifyTrackID(spotifyTrackID)
	if id == "" {
		return "", false, fmt.Errorf("invalid spotify track id %q", spotifyTrackID)
	}
	isrc, err := l.spotifyISRC(ctx, id)
	if err != nil {
		return "", false, err
	}
	return l.deezerTrackByISRC(ctx, isrc)
}

func (l *spotifyDeezerLookup) accessToken(ctx context.Context) (string, error) {
	if !l.tokenReady {
		l.token, l.tokenErr = l.fetchToken(ctx, l.creds)
		l.tokenReady = true
	}
	return l.token, l.tokenErr
}

func (l *spotifyDeezerLookup) spotifyISRC(ctx context.Context, trackID string) (string, error) {
	var decoded struct {
		ExternalIDs struct {
			ISRC string `json:"isrc"`
		} `json:"external_ids"`
	}
	trackURL := "https://api.spotify.com/v1/tracks/" + url.PathEscape(trackID)
	// Client-credentials tokens expire after about an hour; refresh once on 401.
	for attempt := 0; ; attempt++ {
		token, err := l.accessToken(ctx)
		if err != nil {
			return "", err
		}
		err = l.getJSON(ctx, trackURL, map[string]string{"Authorization": "Bearer " + token}, &decoded)
		var statusErr httpStatusError
		if err != nil && attempt == 0 && errors.As(err, &statusErr) && int(statusErr) == http.StatusUnauthorized {
			l.tokenReady = false
			continue
		}
		if err != nil {
			return "", fmt.Errorf("spotify track lookup: %w", err)
		}
		break
	}
	isrc := strings.ToUpper(strings.TrimSpace(decoded.ExternalIDs.ISRC))
	if isrc == "" {
		return "", fmt.Errorf("spotify track %s has no ISRC", trackID)
	}
	return isrc, nil
}

func (l *spotifyDeezerLookup) deezerTrackByISRC(ctx context.Context, isrc string) (string, bool, error) {
	var decoded struct {
		ID    json.Number `json:"id"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := l.getJSON(ctx, "https://api.deezer.com/2.0/track/isrc:"+url.PathEscape(isrc), nil, &decoded); err != nil {
		return "", false, fmt.Errorf("deezer isrc lookup: %w", err)
	}
	if decoded.Error != nil {
		// 800 = "no data": Deezer has no track with this ISRC.
		if decoded.Error.Code == 800 {
			return "", false, nil
		}
		return "", false, fmt.Errorf("deezer isrc lookup: code=%d %s", decoded.Error.Code, decoded.Error.Message)
	}
	deezerID := strings.TrimSpace(decoded.ID.String())
	if deezerID == "" || deezerID == "0" {
		return "", false, fmt.Errorf("deezer isrc lookup: response has no track id")
	}
	return deezerID, true, nil
}

type httpStatusError int

func (e httpStatusError) Error() string { return fmt.Sprintf("status=%d", int(e)) }

func (l *spotifyDeezerLookup) getJSON(ctx context.Context, rawURL string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return err
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpStatusError(resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

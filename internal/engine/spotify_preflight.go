package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
)

var spotifyIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{10,32}$`)

type spotifyRemoteTrack struct {
	ID       string
	Title    string
	Artist   string
	Album    string
	URL      string
	AddedAt  time.Time
	Position int
}

type spotifyTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type spotifyPlaylistTrackPage struct {
	Items []struct {
		AddedAt string `json:"added_at"`
		Track   *struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Album *struct {
				Name string `json:"name"`
			} `json:"album"`
			ExternalURLs map[string]string `json:"external_urls"`
		} `json:"track"`
	} `json:"items"`
	Next string `json:"next"`
}

func enumerateSpotifyPlaylistTracks(
	ctx context.Context,
	source config.Source,
	creds auth.SpotifyCredentials,
) ([]spotifyRemoteTrack, error) {
	playlistID, err := resolveSpotifyPlaylistID(source.URL)
	if err != nil {
		return nil, err
	}

	token, err := fetchSpotifyAccessToken(ctx, creds)
	if err != nil {
		fallbackTracks, fallbackErr := enumerateSpotifyPlaylistTracksViaPage(ctx, playlistID)
		if fallbackErr == nil {
			return fallbackTracks, nil
		}
		return nil, fmt.Errorf("%v (fallback playlist scraping failed: %v)", err, fallbackErr)
	}

	tracks, err := enumerateSpotifyPlaylistTracksWithToken(ctx, playlistID, token)
	if err == nil {
		return tracks, nil
	}
	fallbackTracks, fallbackErr := enumerateSpotifyPlaylistTracksViaPage(ctx, playlistID)
	if fallbackErr == nil {
		return fallbackTracks, nil
	}
	return nil, fmt.Errorf("%v (fallback playlist scraping failed: %v)", err, fallbackErr)
}

func enumerateSpotifyPlaylistTracksWithToken(
	ctx context.Context,
	playlistID string,
	token string,
) ([]spotifyRemoteTrack, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	nextURL := fmt.Sprintf("https://api.spotify.com/v1/playlists/%s/tracks?limit=100", playlistID)
	tracks := make([]spotifyRemoteTrack, 0, 256)
	seen := map[string]struct{}{}

	position := 0
	for strings.TrimSpace(nextURL) != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return nil, fmt.Errorf("create spotify playlist request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("spotify playlist request failed: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read spotify playlist response: %w", readErr)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("spotify playlist request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var page spotifyPlaylistTrackPage
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decode spotify playlist response: %w", err)
		}

		for _, item := range page.Items {
			position++
			if item.Track == nil {
				continue
			}
			id := extractSpotifyTrackID(item.Track.ID)
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}

			artist := ""
			if len(item.Track.Artists) > 0 {
				artist = strings.TrimSpace(item.Track.Artists[0].Name)
			}
			title := strings.TrimSpace(item.Track.Name)
			album := ""
			if item.Track.Album != nil {
				album = strings.TrimSpace(item.Track.Album.Name)
			}

			trackURL := spotifyTrackURL(id)
			if item.Track.ExternalURLs != nil && strings.TrimSpace(item.Track.ExternalURLs["spotify"]) != "" {
				trackURL = strings.TrimSpace(item.Track.ExternalURLs["spotify"])
			}
			var addedAt time.Time
			if trimmed := strings.TrimSpace(item.AddedAt); trimmed != "" {
				addedAt, _ = time.Parse(time.RFC3339, trimmed)
			}

			tracks = append(tracks, spotifyRemoteTrack{
				ID:       id,
				Title:    title,
				Artist:   artist,
				Album:    album,
				URL:      trackURL,
				AddedAt:  addedAt,
				Position: position,
			})
		}

		nextURL = strings.TrimSpace(page.Next)
	}

	return tracks, nil
}

func enumerateSpotifyPlaylistTracksViaPage(
	ctx context.Context,
	playlistID string,
) ([]spotifyRemoteTrack, error) {
	playlistURL := "https://open.spotify.com/playlist/" + playlistID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playlistURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create spotify playlist page request: %w", err)
	}
	req.Header.Set("User-Agent", "udl/spotify-deemix")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("spotify playlist page request failed: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read spotify playlist page response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("spotify playlist page request failed: status=%d", resp.StatusCode)
	}

	ids := extractSpotifyTrackIDsFromPlaylistHTML(string(body))
	if len(ids) == 0 {
		return nil, fmt.Errorf("spotify playlist page did not expose track IDs")
	}

	tracks := make([]spotifyRemoteTrack, 0, len(ids))
	for i, id := range ids {
		tracks = append(tracks, spotifyRemoteTrack{
			ID:       id,
			URL:      spotifyTrackURL(id),
			Title:    id,
			Position: i + 1,
		})
	}
	return tracks, nil
}

func fetchSpotifyAccessToken(ctx context.Context, creds auth.SpotifyCredentials) (string, error) {
	clientID := strings.TrimSpace(creds.ClientID)
	clientSecret := strings.TrimSpace(creds.ClientSecret)
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("spotify client credentials are missing")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://accounts.spotify.com/api/token",
		strings.NewReader("grant_type=client_credentials"),
	)
	if err != nil {
		return "", fmt.Errorf("create spotify token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(clientID+":"+clientSecret)))

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("spotify token request failed: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return "", fmt.Errorf("read spotify token response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("spotify token request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var decoded spotifyTokenResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", fmt.Errorf("decode spotify token response: %w", err)
	}
	token := strings.TrimSpace(decoded.AccessToken)
	if token == "" {
		return "", fmt.Errorf("spotify token response missing access_token")
	}
	return token, nil
}

func resolveSpotifyPlaylistID(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("spotify url must not be empty")
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "spotify:playlist:") {
		id := strings.TrimSpace(strings.TrimPrefix(trimmed, "spotify:playlist:"))
		id = extractSpotifyTrackID(id)
		if id == "" {
			return "", fmt.Errorf("invalid spotify playlist id in url %q", rawURL)
		}
		return id, nil
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse spotify url %q: %w", rawURL, err)
	}

	parts := strings.Split(strings.Trim(path.Clean(parsed.Path), "/"), "/")
	for i := 0; i < len(parts); i++ {
		if parts[i] != "playlist" {
			continue
		}
		if i+1 >= len(parts) {
			break
		}
		id := extractSpotifyTrackID(parts[i+1])
		if id == "" {
			break
		}
		return id, nil
	}
	return "", fmt.Errorf("spotify playlist id not found in url %q", rawURL)
}

func extractSpotifyTrackID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "http://") {
		parsed, err := url.Parse(trimmed)
		if err == nil {
			parts := strings.Split(strings.Trim(path.Clean(parsed.Path), "/"), "/")
			for i := 0; i < len(parts); i++ {
				if parts[i] != "track" {
					continue
				}
				if i+1 >= len(parts) {
					break
				}
				trimmed = strings.TrimSpace(parts[i+1])
				break
			}
		}
	}

	if strings.HasPrefix(strings.ToLower(trimmed), "spotify:track:") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "spotify:track:"))
	}

	if !spotifyIDPattern.MatchString(trimmed) {
		return ""
	}
	return trimmed
}

func spotifyTrackURL(trackID string) string {
	return "https://open.spotify.com/track/" + trackID
}

func buildSpotifyPreflight(
	remoteTracks []spotifyRemoteTrack,
	state spotifySyncState,
	targetDir string,
	mode SoundCloudMode,
) (SoundCloudPreflight, map[string]struct{}, map[string]struct{}, []string, []string, []spotifyStateBackfillEntry) {
	archiveGapIDs := map[string]struct{}{}
	knownGapIDs := map[string]struct{}{}
	existingKnownIDs := make([]string, 0, len(remoteTracks))
	availableLocalPaths := scanLocalMediaTitlePathIndex(targetDir)
	consumedStatePaths := map[string]struct{}{}
	backfillEntries := []spotifyStateBackfillEntry{}

	knownCount := 0
	firstExisting := 0

	for i, track := range remoteTracks {
		_, known := state.KnownIDs[track.ID]
		entry := state.Entries[track.ID]
		if known {
			knownCount++
		}
		hasLocal := spotifyStateTrackPresent(targetDir, entry, consumedStatePaths)
		localPath := ""
		if !hasLocal {
			for _, candidate := range spotifyTrackLocalTitleCandidates(track, entry) {
				if matchedPath, ok := consumeLocalTitlePathMatch(availableLocalPaths, candidate); ok {
					hasLocal = true
					localPath = matchedPath
					break
				}
			}
		}
		if hasLocal {
			existingKnownIDs = append(existingKnownIDs, track.ID)
			if firstExisting == 0 {
				firstExisting = i + 1
			}
			if !known {
				backfillEntries = append(backfillEntries, spotifyStateBackfillEntry{
					ID:          track.ID,
					DisplayName: spotifyTrackLocalTitle(track),
					LocalPath:   localPath,
				})
			}
			continue
		}
		if !known {
			archiveGapIDs[track.ID] = struct{}{}
			continue
		}
		knownGapIDs[track.ID] = struct{}{}
	}

	planned := make([]string, 0, len(remoteTracks))
	switch mode {
	case SoundCloudModeScanGaps:
		for _, track := range remoteTracks {
			if _, ok := archiveGapIDs[track.ID]; ok {
				planned = append(planned, track.ID)
				continue
			}
			if _, ok := knownGapIDs[track.ID]; ok {
				planned = append(planned, track.ID)
			}
		}
	default:
		limit := len(remoteTracks)
		if firstExisting > 0 {
			limit = firstExisting - 1
		}
		for i := 0; i < limit; i++ {
			id := remoteTracks[i].ID
			if _, ok := archiveGapIDs[id]; ok {
				planned = append(planned, id)
				continue
			}
			if _, ok := knownGapIDs[id]; ok {
				planned = append(planned, id)
			}
		}
	}

	preflight := SoundCloudPreflight{
		RemoteTotal:          len(remoteTracks),
		KnownCount:           knownCount,
		ArchiveGapCount:      len(archiveGapIDs),
		KnownGapCount:        len(knownGapIDs),
		FirstExistingIndex:   firstExisting,
		PlannedDownloadCount: len(planned),
		Mode:                 mode,
	}
	return preflight, archiveGapIDs, knownGapIDs, planned, existingKnownIDs, backfillEntries
}

func spotifyTrackLocalTitle(track spotifyRemoteTrack) string {
	title := strings.TrimSpace(track.Title)
	artist := strings.TrimSpace(track.Artist)
	switch {
	case artist != "" && title != "":
		return artist + " - " + title
	case title != "":
		return title
	default:
		return track.ID
	}
}

func spotifyTrackLocalTitleCandidates(track spotifyRemoteTrack, entry spotifyStateEntry) []string {
	candidates := []string{}
	seen := map[string]struct{}{}
	add := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		candidates = append(candidates, trimmed)
	}

	add(spotifyTrackLocalTitle(track))
	add(entry.DisplayName)
	for _, artist := range splitSpotifyArtistCandidates(track.Artist) {
		if track.Title != "" {
			add(artist + " - " + track.Title)
		}
	}
	add(track.Title)
	add(track.ID)
	return candidates
}

func splitSpotifyArtistCandidates(artist string) []string {
	parts := strings.FieldsFunc(artist, func(r rune) bool {
		return r == ',' || r == '&' || r == ';'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func scanLocalMediaTitlePathIndex(root string) map[string][]string {
	index := map[string][]string{}
	root = strings.TrimSpace(root)
	if root == "" {
		return index
	}
	if _, err := os.Stat(root); err != nil {
		return index
	}
	_ = filepath.WalkDir(root, func(mediaPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !isMediaExt(ext) {
			return nil
		}
		stem := strings.TrimSpace(strings.TrimSuffix(name, ext))
		key := normalizeTrackKey(stem)
		if key == "" {
			return nil
		}
		rel, err := filepath.Rel(root, mediaPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = name
		}
		index[key] = append(index[key], filepath.ToSlash(rel))
		return nil
	})
	return index
}

func consumeLocalTitlePathMatch(available map[string][]string, title string) (string, bool) {
	key := normalizeTrackKey(title)
	if key == "" {
		return "", false
	}
	paths := available[key]
	if len(paths) == 0 {
		return "", false
	}
	path := paths[0]
	available[key] = paths[1:]
	return path, true
}

func spotifyStateTrackPresent(targetDir string, entry spotifyStateEntry, consumed map[string]struct{}) bool {
	localPath := normalizeSpotifyStatePath(entry.LocalPath)
	if localPath == "" {
		return false
	}
	if _, used := consumed[localPath]; used {
		return false
	}
	root := strings.TrimSpace(targetDir)
	if root == "" {
		return false
	}
	abs := filepath.Join(root, filepath.FromSlash(localPath))
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return false
	}
	if !isMediaExt(strings.ToLower(filepath.Ext(abs))) {
		return false
	}
	consumed[localPath] = struct{}{}
	return true
}

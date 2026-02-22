package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	spotifyPlaylistTrackRefPattern = regexp.MustCompile(`spotify:track:([A-Za-z0-9]{22})`)
	spotifyOGTitlePattern          = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:title["'][^>]*content=["']([^"']+)["']`)
	spotifyOGDescriptionPattern    = regexp.MustCompile(`(?i)<meta[^>]*property=["']og:description["'][^>]*content=["']([^"']+)["']`)
	spotifyTitleTagPattern         = regexp.MustCompile(`(?i)<title>([^<]+)</title>`)
)

type spotifyTrackMetadata struct {
	Title  string
	Artist string
	Album  string
	ISRC   string
}

func buildSpotifyTrackMetadataIndex(tracks []spotifyRemoteTrack) map[string]spotifyTrackMetadata {
	if len(tracks) == 0 {
		return nil
	}
	lookup := make(map[string]spotifyTrackMetadata, len(tracks))
	for _, track := range tracks {
		id := extractSpotifyTrackID(track.ID)
		if id == "" {
			continue
		}
		lookup[id] = spotifyTrackMetadata{
			Title:  strings.TrimSpace(track.Title),
			Artist: strings.TrimSpace(track.Artist),
			Album:  strings.TrimSpace(track.Album),
		}
	}
	return lookup
}

func resolveSpotifyTrackMetadataForExecution(
	ctx context.Context,
	trackID string,
	preflight map[string]spotifyTrackMetadata,
) (spotifyTrackMetadata, error) {
	id := extractSpotifyTrackID(trackID)
	if id == "" {
		return spotifyTrackMetadata{}, fmt.Errorf("invalid spotify track id %q", trackID)
	}
	if preflight != nil {
		if cached, ok := preflight[id]; ok {
			if hasUsableSpotifyMetadata(cached) {
				return normalizeSpotifyTrackMetadata(cached), nil
			}
		}
	}
	return fetchSpotifyTrackMetadataFn(ctx, id)
}

func hasUsableSpotifyMetadata(metadata spotifyTrackMetadata) bool {
	return strings.TrimSpace(metadata.Title) != "" && strings.TrimSpace(metadata.Artist) != ""
}

func normalizeSpotifyTrackMetadata(metadata spotifyTrackMetadata) spotifyTrackMetadata {
	title := strings.TrimSpace(metadata.Title)
	artist := strings.TrimSpace(metadata.Artist)
	album := strings.TrimSpace(metadata.Album)
	isrc := strings.TrimSpace(metadata.ISRC)
	if album == "" {
		// deemix fallbackSearch expects a non-empty album string.
		album = title
	}
	return spotifyTrackMetadata{
		Title:  title,
		Artist: artist,
		Album:  album,
		ISRC:   isrc,
	}
}

func fetchSpotifyTrackMetadataFromPage(ctx context.Context, trackID string) (spotifyTrackMetadata, error) {
	id := extractSpotifyTrackID(trackID)
	if id == "" {
		return spotifyTrackMetadata{}, fmt.Errorf("invalid spotify track id %q", trackID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spotifyTrackURL(id), nil)
	if err != nil {
		return spotifyTrackMetadata{}, fmt.Errorf("create spotify track page request: %w", err)
	}
	req.Header.Set("User-Agent", "udl/spotify-deemix")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return spotifyTrackMetadata{}, fmt.Errorf("spotify track page request failed: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return spotifyTrackMetadata{}, fmt.Errorf("read spotify track page response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return spotifyTrackMetadata{}, fmt.Errorf("spotify track page request failed: status=%d", resp.StatusCode)
	}

	metadata, parseErr := parseSpotifyTrackMetadataFromHTML(string(body))
	if parseErr != nil {
		return spotifyTrackMetadata{}, fmt.Errorf("parse spotify track metadata: %w", parseErr)
	}
	return metadata, nil
}

func parseSpotifyTrackMetadataFromHTML(document string) (spotifyTrackMetadata, error) {
	title := extractFirstHTMLMeta(spotifyOGTitlePattern, document)
	if title == "" {
		if titleTag := extractFirstHTMLMeta(spotifyTitleTagPattern, document); titleTag != "" {
			titleTag = strings.TrimSpace(strings.TrimSuffix(titleTag, "| Spotify"))
			if idx := strings.Index(titleTag, " - song and lyrics by "); idx > 0 {
				titleTag = strings.TrimSpace(titleTag[:idx])
			}
			title = titleTag
		}
	}

	description := extractFirstHTMLMeta(spotifyOGDescriptionPattern, document)
	artist, album := parseSpotifyDescription(description)

	metadata := spotifyTrackMetadata{
		Title:  title,
		Artist: artist,
		Album:  album,
	}
	if !hasUsableSpotifyMetadata(metadata) {
		return spotifyTrackMetadata{}, fmt.Errorf("insufficient metadata extracted")
	}
	return normalizeSpotifyTrackMetadata(metadata), nil
}

func extractFirstHTMLMeta(pattern *regexp.Regexp, document string) string {
	match := pattern.FindStringSubmatch(document)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(match[1]))
}

func parseSpotifyDescription(description string) (string, string) {
	parts := strings.Split(description, " · ")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		clean = append(clean, trimmed)
	}
	if len(clean) == 0 {
		return "", ""
	}
	if len(clean) == 1 {
		return clean[0], ""
	}
	return clean[0], clean[1]
}

func extractSpotifyTrackIDsFromPlaylistHTML(document string) []string {
	matches := spotifyPlaylistTrackRefPattern.FindAllStringSubmatch(document, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		id := extractSpotifyTrackID(match[1])
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

type spotifyPluginTrackCache struct {
	ID   any                        `json:"id,omitempty"`
	ISRC string                     `json:"isrc,omitempty"`
	Data *spotifyPluginTrackDetails `json:"data,omitempty"`
}

type spotifyPluginTrackDetails struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
}

type spotifyPluginCache struct {
	Tracks map[string]spotifyPluginTrackCache `json:"tracks"`
	Albums map[string]any                     `json:"albums"`
}

func writeSpotifyTrackMetadataCache(runtimeDir, trackID string, metadata spotifyTrackMetadata) error {
	dir := strings.TrimSpace(runtimeDir)
	if dir == "" {
		return fmt.Errorf("runtime directory is required")
	}
	id := extractSpotifyTrackID(trackID)
	if id == "" {
		return fmt.Errorf("invalid spotify track id %q", trackID)
	}
	if !hasUsableSpotifyMetadata(metadata) {
		return fmt.Errorf("spotify metadata is incomplete for track %s", id)
	}

	cachePath := filepath.Join(dir, "config", "spotify", "cache.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		return fmt.Errorf("create spotify cache directory: %w", err)
	}

	cache := spotifyPluginCache{
		Tracks: map[string]spotifyPluginTrackCache{},
		Albums: map[string]any{},
	}
	if payload, err := os.ReadFile(cachePath); err == nil {
		if len(payload) > 0 {
			if decodeErr := json.Unmarshal(payload, &cache); decodeErr != nil {
				return fmt.Errorf("decode spotify cache: %w", decodeErr)
			}
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read spotify cache: %w", err)
	}

	if cache.Tracks == nil {
		cache.Tracks = map[string]spotifyPluginTrackCache{}
	}
	if cache.Albums == nil {
		cache.Albums = map[string]any{}
	}

	normalized := normalizeSpotifyTrackMetadata(metadata)
	entry := cache.Tracks[id]
	if strings.TrimSpace(normalized.ISRC) != "" {
		entry.ISRC = normalized.ISRC
	}
	entry.Data = &spotifyPluginTrackDetails{
		Title:  normalized.Title,
		Artist: normalized.Artist,
		Album:  normalized.Album,
	}
	cache.Tracks[id] = entry

	payload, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("encode spotify cache: %w", err)
	}
	if err := os.WriteFile(cachePath, payload, 0o600); err != nil {
		return fmt.Errorf("write spotify cache: %w", err)
	}
	return nil
}

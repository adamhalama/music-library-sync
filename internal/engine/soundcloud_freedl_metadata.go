package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/fileops"
)

var errSoundCloudNoFreeDownloadLink = errors.New("soundcloud track has no free-download link")

var soundCloudBuyAnchorPattern = regexp.MustCompile(`(?i)<a href="([^"]+)">Buy[^<]*</a>`)

type soundCloudHydrationItem struct {
	Hydratable string          `json:"hydratable"`
	Data       json.RawMessage `json:"data"`
}

type soundCloudHydratedSound struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Genre        string `json:"genre"`
	ArtworkURL   string `json:"artwork_url"`
	PurchaseURL  string `json:"purchase_url"`
	PermalinkURL string `json:"permalink_url"`
	User         struct {
		Username  string `json:"username"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
}

type soundCloudFreeDownloadMetadata struct {
	ID            string
	Title         string
	Artist        string
	Genre         string
	SoundCloudURL string
	ArtworkURL    string
	PurchaseURL   string
}

func fetchSoundCloudFreeDownloadMetadata(ctx context.Context, track soundCloudRemoteTrack) (soundCloudFreeDownloadMetadata, error) {
	metadata := soundCloudFreeDownloadMetadata{
		ID:            strings.TrimSpace(track.ID),
		Title:         strings.TrimSpace(track.Title),
		SoundCloudURL: strings.TrimSpace(track.URL),
	}
	trackURL := strings.TrimSpace(track.URL)
	if trackURL == "" {
		return metadata, fmt.Errorf("soundcloud track %q has empty url", track.ID)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trackURL, nil)
	if err != nil {
		return metadata, fmt.Errorf("create soundcloud track page request: %w", err)
	}
	req.Header.Set("User-Agent", "udl/soundcloud-freedl")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return metadata, fmt.Errorf("soundcloud track page request failed: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return metadata, fmt.Errorf("read soundcloud track page response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return metadata, fmt.Errorf("soundcloud track page request failed: status=%d", resp.StatusCode)
	}

	document := string(body)
	if hydrated, hydrationErr := parseSoundCloudHydratedSound(document); hydrationErr == nil {
		if hydrated.ID != 0 && metadata.ID == "" {
			metadata.ID = strconv.FormatInt(hydrated.ID, 10)
		}
		if title := strings.TrimSpace(hydrated.Title); title != "" {
			metadata.Title = title
		}
		if artist := strings.TrimSpace(hydrated.User.Username); artist != "" {
			metadata.Artist = artist
		}
		if genre := strings.TrimSpace(hydrated.Genre); genre != "" {
			metadata.Genre = genre
		}
		if url := strings.TrimSpace(hydrated.PermalinkURL); url != "" {
			metadata.SoundCloudURL = url
		}
		if artworkURL := strings.TrimSpace(hydrated.ArtworkURL); artworkURL != "" {
			metadata.ArtworkURL = resolveRelativeURL(trackURL, resolveSoundCloudArtworkURL(artworkURL))
		}
		if strings.TrimSpace(metadata.ArtworkURL) == "" {
			if avatarURL := strings.TrimSpace(hydrated.User.AvatarURL); avatarURL != "" {
				metadata.ArtworkURL = resolveRelativeURL(trackURL, resolveSoundCloudArtworkURL(avatarURL))
			}
		}
		if purchaseURL := strings.TrimSpace(hydrated.PurchaseURL); purchaseURL != "" {
			metadata.PurchaseURL = resolveRelativeURL(trackURL, purchaseURL)
		}
	}
	if strings.TrimSpace(metadata.PurchaseURL) == "" {
		if fallback := extractSoundCloudBuyURLFallback(document); fallback != "" {
			metadata.PurchaseURL = resolveRelativeURL(trackURL, fallback)
		}
	}
	if strings.TrimSpace(metadata.PurchaseURL) == "" {
		return metadata, errSoundCloudNoFreeDownloadLink
	}
	return metadata, nil
}

func parseSoundCloudHydratedSound(document string) (soundCloudHydratedSound, error) {
	payload, err := extractSoundCloudHydrationPayload(document)
	if err != nil {
		return soundCloudHydratedSound{}, err
	}

	items := []soundCloudHydrationItem{}
	if err := json.Unmarshal([]byte(payload), &items); err != nil {
		return soundCloudHydratedSound{}, fmt.Errorf("decode soundcloud hydration payload: %w", err)
	}
	for _, item := range items {
		if item.Hydratable != "sound" {
			continue
		}
		var sound soundCloudHydratedSound
		if err := json.Unmarshal(item.Data, &sound); err != nil {
			continue
		}
		if sound.ID != 0 || strings.TrimSpace(sound.Title) != "" {
			return sound, nil
		}
	}
	return soundCloudHydratedSound{}, fmt.Errorf("soundcloud hydration payload missing sound entry")
}

func extractSoundCloudHydrationPayload(document string) (string, error) {
	const prefix = "window.__sc_hydration = "
	start := strings.Index(document, prefix)
	if start < 0 {
		return "", fmt.Errorf("soundcloud hydration payload not found")
	}
	start += len(prefix)

	suffixOffset := strings.Index(document[start:], ";</script>")
	if suffixOffset < 0 {
		suffixOffset = strings.Index(document[start:], "</script>")
	}
	if suffixOffset < 0 {
		return "", fmt.Errorf("soundcloud hydration payload terminator not found")
	}
	payload := strings.TrimSpace(document[start : start+suffixOffset])
	payload = strings.TrimSuffix(payload, ";")
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "", fmt.Errorf("soundcloud hydration payload is empty")
	}
	return payload, nil
}

func extractSoundCloudBuyURLFallback(document string) string {
	match := soundCloudBuyAnchorPattern.FindStringSubmatch(document)
	if len(match) != 2 {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(match[1]))
}

func resolveRelativeURL(base string, candidate string) string {
	raw := strings.TrimSpace(candidate)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if parsed.IsAbs() {
		return parsed.String()
	}
	baseParsed, baseErr := url.Parse(strings.TrimSpace(base))
	if baseErr != nil {
		return raw
	}
	return baseParsed.ResolveReference(parsed).String()
}

func resolveSoundCloudArtworkURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return strings.Replace(trimmed, "-large.", "-t500x500.", 1)
}

func applySoundCloudTrackMetadata(ctx context.Context, filePath string, metadata soundCloudFreeDownloadMetadata) error {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return fmt.Errorf("empty file path")
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory: %s", trimmed)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(trimmed), ".udl-meta-*"+filepath.Ext(trimmed))
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	_ = os.Remove(tempPath)

	artworkPath := ""
	artworkEmbedErr := error(nil)
	if artworkURL := strings.TrimSpace(metadata.ArtworkURL); artworkURL != "" {
		downloadedArtworkPath, artworkErr := downloadSoundCloudArtwork(ctx, artworkURL, filepath.Dir(trimmed))
		if artworkErr == nil {
			artworkPath = downloadedArtworkPath
			defer func() {
				_ = os.Remove(downloadedArtworkPath)
			}()
		} else {
			artworkEmbedErr = fmt.Errorf("artwork download failed: %w", artworkErr)
		}
	}

	if artworkPath != "" {
		if err := runSoundCloudMetadataFFmpeg(ctx, trimmed, tempPath, metadata, artworkPath); err == nil {
			return fileops.ReplaceFileSafely(tempPath, trimmed)
		} else {
			artworkEmbedErr = err
			_ = os.Remove(tempPath)
		}
	}

	if err := runSoundCloudMetadataFFmpeg(ctx, trimmed, tempPath, metadata, ""); err != nil {
		if artworkEmbedErr == nil {
			return err
		}
		return fmt.Errorf("artwork embedding failed (%v) and metadata fallback failed (%w)", artworkEmbedErr, err)
	}
	if err := fileops.ReplaceFileSafely(tempPath, trimmed); err != nil {
		return err
	}
	if artworkEmbedErr != nil {
		return fmt.Errorf("metadata written without artwork: %v", artworkEmbedErr)
	}
	return nil
}

func runSoundCloudMetadataFFmpeg(
	ctx context.Context,
	inputPath string,
	outputPath string,
	metadata soundCloudFreeDownloadMetadata,
	artworkPath string,
) error {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-map", "0",
		"-codec", "copy",
	}
	if strings.TrimSpace(artworkPath) != "" {
		args = append(args, "-i", artworkPath)
		args = append(args, "-map", "1:0")
		args = append(args, "-c:v", "mjpeg", "-disposition:v:0", "attached_pic")
	}
	if title := strings.TrimSpace(metadata.Title); title != "" {
		args = append(args, "-metadata", "title="+title)
	}
	if artist := strings.TrimSpace(metadata.Artist); artist != "" {
		args = append(args, "-metadata", "artist="+artist)
		args = append(args, "-metadata", "album_artist="+artist)
	}
	if genre := strings.TrimSpace(metadata.Genre); genre != "" {
		args = append(args, "-metadata", "genre="+genre)
	}
	if sourceURL := strings.TrimSpace(metadata.SoundCloudURL); sourceURL != "" {
		args = append(args, "-metadata", "comment="+sourceURL)
	}
	args = append(args, outputPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		_ = os.Remove(outputPath)
		trimmedOutput := strings.TrimSpace(string(output))
		if trimmedOutput == "" {
			return runErr
		}
		return fmt.Errorf("%v: %s", runErr, trimmedOutput)
	}
	return nil
}

func downloadSoundCloudArtwork(ctx context.Context, rawURL string, tempDir string) (string, error) {
	resolvedURL := strings.TrimSpace(resolveSoundCloudArtworkURL(rawURL))
	if resolvedURL == "" {
		return "", fmt.Errorf("empty artwork url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolvedURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "udl/soundcloud-freedl")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("artwork request failed: status=%d", resp.StatusCode)
	}

	pathHint := ""
	if resp.Request != nil && resp.Request.URL != nil {
		pathHint = resp.Request.URL.Path
	}
	ext := inferArtworkFileExtension(resp.Header.Get("Content-Type"), pathHint)
	tmp, err := os.CreateTemp(tempDir, ".udl-artwork-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func inferArtworkFileExtension(contentType string, pathHint string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(pathHint)))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
		return ext
	}
	normalized := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch normalized {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/jpg", "image/jpeg":
		return ".jpg"
	default:
		return ".jpg"
	}
}

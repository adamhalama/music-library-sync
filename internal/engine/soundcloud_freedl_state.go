package engine

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
)

type soundCloudFreeDLStuckRecord struct {
	Timestamp     string `json:"timestamp"`
	SourceID      string `json:"source_id"`
	TrackID       string `json:"track_id"`
	Title         string `json:"title,omitempty"`
	Artist        string `json:"artist,omitempty"`
	SoundCloudURL string `json:"soundcloud_url,omitempty"`
	PurchaseURL   string `json:"purchase_url,omitempty"`
	DownloadDir   string `json:"download_dir,omitempty"`
	Stage         string `json:"stage"`
	Error         string `json:"error"`
	Strategy      string `json:"strategy"`
}

func sanitizeSoundCloudFreeDownloadURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return raw
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func normalizeSoundCloudStatePath(targetDir string, downloadedPath string) string {
	candidate := strings.TrimSpace(downloadedPath)
	if candidate == "" {
		return ""
	}
	abs := candidate
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(targetDir, abs)
	}
	abs = filepath.Clean(abs)
	rel, err := filepath.Rel(targetDir, abs)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.ToSlash(rel)
	}
	return abs
}

func appendSoundCloudSyncStateEntry(statePath string, trackID string, localPath string) error {
	id := strings.TrimSpace(trackID)
	path := strings.TrimSpace(localPath)
	if id == "" {
		return fmt.Errorf("soundcloud state append requires track id")
	}
	if path == "" {
		return fmt.Errorf("soundcloud state append requires local path")
	}
	return appendLine(statePath, fmt.Sprintf("soundcloud %s %s\n", id, path))
}

func appendSoundCloudArchiveID(archivePath string, trackID string) error {
	id := strings.TrimSpace(trackID)
	if id == "" {
		return fmt.Errorf("soundcloud archive append requires track id")
	}
	return appendLine(archivePath, fmt.Sprintf("soundcloud %s\n", id))
}

func appendLine(path string, line string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return fmt.Errorf("append target path must not be empty")
	}
	if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(trimmed, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	if _, err := file.WriteString(line); err != nil {
		return err
	}
	return file.Sync()
}

func resolveSoundCloudFreeDLStuckLogPath(defaultStateDir string, sourceID string) (string, error) {
	stateDir, err := config.ExpandPath(defaultStateDir)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(stateDir) {
		return "", fmt.Errorf("state_dir must resolve to an absolute path")
	}
	trimmedID := strings.TrimSpace(sourceID)
	if trimmedID == "" {
		return "", fmt.Errorf("source id is required")
	}
	return filepath.Join(stateDir, trimmedID+".freedl-stuck.jsonl"), nil
}

func appendSoundCloudFreeDLStuckRecord(path string, record soundCloudFreeDLStuckRecord) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(trimmed), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(trimmed, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		return err
	}
	if _, err := file.WriteString("\n"); err != nil {
		return err
	}
	return file.Sync()
}

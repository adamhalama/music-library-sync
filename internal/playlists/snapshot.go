package playlists

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

const SnapshotVersion = 1

type Track struct {
	Index        int    `json:"index"`
	ProviderID   string `json:"provider_id,omitempty"`
	DatabaseID   string `json:"database_id,omitempty"`
	Artist       string `json:"artist,omitempty"`
	Title        string `json:"title"`
	Album        string `json:"album,omitempty"`
	Duration     string `json:"duration,omitempty"`
	Path         string `json:"path,omitempty"`
	MissingLocal bool   `json:"missing_local,omitempty"`
}

type Snapshot struct {
	Version            int       `json:"version"`
	PlaylistID         string    `json:"playlist_id"`
	Name               string    `json:"name"`
	Provider           string    `json:"provider"`
	ProviderPlaylist   string    `json:"provider_playlist"`
	ProviderPlaylistID string    `json:"provider_playlist_id,omitempty"`
	RefreshedAt        time.Time `json:"refreshed_at"`
	Tracks             []Track   `json:"tracks"`
	ChecksumSHA256     string    `json:"checksum_sha256"`
}

type Changes struct {
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Kept    int `json:"kept"`
}

func SnapshotPath(stateDir, playlistID string) (string, error) {
	if strings.TrimSpace(playlistID) == "" {
		return "", fmt.Errorf("playlist id must not be empty")
	}
	expanded, err := config.ExpandPath(firstNonEmpty(stateDir, config.DefaultStateDir()))
	if err != nil {
		return "", err
	}
	return filepath.Join(expanded, "playlists", playlistID+".json"), nil
}

func LoadSnapshot(stateDir, playlistID string) (Snapshot, error) {
	path, err := SnapshotPath(stateDir, playlistID)
	if err != nil {
		return Snapshot{}, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("parse playlist snapshot %s: %w", path, err)
	}
	if err := ValidateSnapshot(snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("invalid playlist snapshot %s: %w", path, err)
	}
	return snapshot, nil
}

func WriteSnapshot(stateDir string, snapshot Snapshot) (string, error) {
	snapshot.Version = SnapshotVersion
	snapshot.ChecksumSHA256 = ""
	checksum, err := snapshotChecksum(snapshot)
	if err != nil {
		return "", err
	}
	snapshot.ChecksumSHA256 = checksum
	if err := ValidateSnapshot(snapshot); err != nil {
		return "", err
	}
	payload, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode playlist snapshot: %w", err)
	}
	payload = append(payload, '\n')
	path, err := SnapshotPath(stateDir, snapshot.PlaylistID)
	if err != nil {
		return "", err
	}
	if err := atomicWrite(path, payload, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func ValidateSnapshot(snapshot Snapshot) error {
	if snapshot.Version != SnapshotVersion {
		return fmt.Errorf("snapshot version must be %d", SnapshotVersion)
	}
	if strings.TrimSpace(snapshot.PlaylistID) == "" {
		return errors.New("snapshot playlist_id must not be empty")
	}
	if strings.TrimSpace(snapshot.Name) == "" {
		return errors.New("snapshot name must not be empty")
	}
	if !SupportedProvider(snapshot.Provider) {
		return fmt.Errorf("snapshot provider %q is unsupported", snapshot.Provider)
	}
	if snapshot.RefreshedAt.IsZero() {
		return errors.New("snapshot refreshed_at must be set")
	}
	expected, err := snapshotChecksum(snapshot)
	if err != nil {
		return err
	}
	if snapshot.ChecksumSHA256 == "" || snapshot.ChecksumSHA256 != expected {
		return errors.New("snapshot checksum does not match content")
	}
	return nil
}

func CompareSnapshots(previous, next Snapshot) Changes {
	oldCounts := map[string]int{}
	for _, track := range previous.Tracks {
		oldCounts[trackIdentity(track)]++
	}
	changes := Changes{}
	for _, track := range next.Tracks {
		key := trackIdentity(track)
		if oldCounts[key] > 0 {
			changes.Kept++
			oldCounts[key]--
		} else {
			changes.Added++
		}
	}
	for _, count := range oldCounts {
		changes.Removed += count
	}
	return changes
}

func snapshotChecksum(snapshot Snapshot) (string, error) {
	copySnapshot := snapshot
	copySnapshot.ChecksumSHA256 = ""
	payload, err := json.Marshal(copySnapshot)
	if err != nil {
		return "", fmt.Errorf("encode playlist snapshot checksum payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func trackIdentity(track Track) string {
	for _, value := range []string{track.ProviderID, track.DatabaseID, cleanPath(track.Path)} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return normalizeText(track.Artist + " " + track.Title)
}

func atomicWrite(path string, payload []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".playlist-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	cleanup = false
	return nil
}

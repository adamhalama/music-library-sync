package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
)

const soundCloudLocalIndexCacheSchema = 1

type soundCloudLocalIndexCacheRecord struct {
	Schema          int            `json:"schema"`
	SourceID        string         `json:"source_id"`
	TargetDir       string         `json:"target_dir"`
	TargetSignature string         `json:"target_signature"`
	Index           map[string]int `json:"index"`
	IntegrityHash   string         `json:"integrity_hash"`
}

type soundCloudLocalIndexCachePayload struct {
	Schema          int            `json:"schema"`
	SourceID        string         `json:"source_id"`
	TargetDir       string         `json:"target_dir"`
	TargetSignature string         `json:"target_signature"`
	Index           map[string]int `json:"index"`
}

func loadLocalIndexCache(stateDir string, sourceID string, targetDir string, signature string) (map[string]int, bool) {
	cachePath, err := localIndexCachePath(stateDir, sourceID)
	if err != nil {
		return nil, false
	}

	raw, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, false
	}

	record := soundCloudLocalIndexCacheRecord{}
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, false
	}

	if record.Schema != soundCloudLocalIndexCacheSchema {
		return nil, false
	}

	normalizedTargetDir := filepath.Clean(strings.TrimSpace(targetDir))
	if filepath.Clean(strings.TrimSpace(record.TargetDir)) != normalizedTargetDir {
		return nil, false
	}
	if strings.TrimSpace(record.SourceID) != strings.TrimSpace(sourceID) {
		return nil, false
	}
	if strings.TrimSpace(record.TargetSignature) == "" || strings.TrimSpace(record.TargetSignature) != strings.TrimSpace(signature) {
		return nil, false
	}
	if len(record.Index) == 0 {
		return nil, false
	}

	expectedHash, err := localIndexCacheHash(soundCloudLocalIndexCachePayload{
		Schema:          record.Schema,
		SourceID:        record.SourceID,
		TargetDir:       record.TargetDir,
		TargetSignature: record.TargetSignature,
		Index:           record.Index,
	})
	if err != nil {
		return nil, false
	}
	if expectedHash != strings.TrimSpace(record.IntegrityHash) {
		return nil, false
	}

	out := make(map[string]int, len(record.Index))
	for key, count := range record.Index {
		out[key] = count
	}
	return out, true
}

func storeLocalIndexCache(stateDir string, sourceID string, targetDir string, signature string, index map[string]int) {
	if len(index) == 0 {
		return
	}
	cachePath, err := localIndexCachePath(stateDir, sourceID)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return
	}

	normalizedSourceID := strings.TrimSpace(sourceID)
	normalizedTargetDir := filepath.Clean(strings.TrimSpace(targetDir))
	payload := soundCloudLocalIndexCachePayload{
		Schema:          soundCloudLocalIndexCacheSchema,
		SourceID:        normalizedSourceID,
		TargetDir:       normalizedTargetDir,
		TargetSignature: strings.TrimSpace(signature),
		Index:           map[string]int{},
	}
	for key, count := range index {
		payload.Index[key] = count
	}

	hash, err := localIndexCacheHash(payload)
	if err != nil {
		return
	}
	record := soundCloudLocalIndexCacheRecord{
		Schema:          payload.Schema,
		SourceID:        payload.SourceID,
		TargetDir:       payload.TargetDir,
		TargetSignature: payload.TargetSignature,
		Index:           payload.Index,
		IntegrityHash:   hash,
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return
	}

	tempFile, err := os.CreateTemp(filepath.Dir(cachePath), ".udl-local-index-*.tmp")
	if err != nil {
		return
	}
	tempPath := tempFile.Name()
	if _, err := tempFile.Write(encoded); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return
	}
	if err := os.Rename(tempPath, cachePath); err != nil {
		_ = os.Remove(tempPath)
		return
	}
}

func localIndexCacheHash(payload soundCloudLocalIndexCachePayload) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func localIndexCachePath(stateDir string, sourceID string) (string, error) {
	cacheName := "soundcloud.local-index.json"
	if trimmedID := strings.TrimSpace(sourceID); trimmedID != "" {
		cacheName = fmt.Sprintf("%s.local-index.json", trimmedID)
	}
	return config.ResolveStateFile(stateDir, cacheName)
}

func targetDirSignature(targetDir string) (string, error) {
	info, err := os.Stat(targetDir)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size()), nil
}

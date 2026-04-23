package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

type CredentialKind string

const (
	CredentialKindSoundCloudClientID CredentialKind = "soundcloud_client_id"
	CredentialKindDeemixARL          CredentialKind = "deemix_arl"
	CredentialKindSpotifyApp         CredentialKind = "spotify_app"
)

type CredentialStorageSource string

const (
	CredentialStorageSourceNone        CredentialStorageSource = ""
	CredentialStorageSourceEnv         CredentialStorageSource = "env"
	CredentialStorageSourceKeychain    CredentialStorageSource = "keychain"
	CredentialStorageSourceSpotDL      CredentialStorageSource = "spotdl_config"
	CredentialStorageSourceUnknown     CredentialStorageSource = "unknown"
)

type CredentialHealth string

const (
	CredentialHealthMissing          CredentialHealth = "missing"
	CredentialHealthAvailable        CredentialHealth = "available"
	CredentialHealthExternalOverride CredentialHealth = "external_override"
	CredentialHealthNeedsRefresh     CredentialHealth = "needs_refresh"
)

type CredentialMetadata struct {
	LastCheckedAt     time.Time               `json:"last_checked_at,omitempty"`
	LastFailureKind   string                  `json:"last_failure_kind,omitempty"`
	LastFailureMessage string                 `json:"last_failure_message,omitempty"`
	StorageSource     CredentialStorageSource `json:"storage_source,omitempty"`
}

type CredentialMetadataStore struct {
	Credentials map[CredentialKind]CredentialMetadata `json:"credentials"`
}

func LoadCredentialMetadata(stateDir string) (CredentialMetadataStore, error) {
	store := CredentialMetadataStore{Credentials: map[CredentialKind]CredentialMetadata{}}
	path, err := credentialMetadataPath(stateDir)
	if err != nil {
		return store, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return store, fmt.Errorf("read credential metadata: %w", err)
	}
	if len(strings.TrimSpace(string(payload))) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(payload, &store); err != nil {
		return store, fmt.Errorf("parse credential metadata: %w", err)
	}
	if store.Credentials == nil {
		store.Credentials = map[CredentialKind]CredentialMetadata{}
	}
	return store, nil
}

func SaveCredentialMetadata(stateDir string, store CredentialMetadataStore) error {
	path, err := credentialMetadataPath(stateDir)
	if err != nil {
		return err
	}
	if store.Credentials == nil {
		store.Credentials = map[CredentialKind]CredentialMetadata{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create credential metadata directory: %w", err)
	}
	payload, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credential metadata: %w", err)
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("write credential metadata: %w", err)
	}
	return nil
}

func RecordCredentialCheck(stateDir string, kind CredentialKind, source CredentialStorageSource) error {
	return updateCredentialMetadata(stateDir, kind, func(meta *CredentialMetadata) {
		meta.LastCheckedAt = time.Now().UTC()
		meta.StorageSource = source
	})
}

func RecordCredentialFailure(stateDir string, kind CredentialKind, source CredentialStorageSource, failureKind string, failureMessage string) error {
	return updateCredentialMetadata(stateDir, kind, func(meta *CredentialMetadata) {
		meta.LastCheckedAt = time.Now().UTC()
		meta.StorageSource = source
		meta.LastFailureKind = strings.TrimSpace(failureKind)
		meta.LastFailureMessage = strings.TrimSpace(failureMessage)
	})
}

func ClearCredentialFailure(stateDir string, kind CredentialKind) error {
	return updateCredentialMetadata(stateDir, kind, func(meta *CredentialMetadata) {
		meta.LastCheckedAt = time.Now().UTC()
		meta.LastFailureKind = ""
		meta.LastFailureMessage = ""
	})
}

func credentialMetadataPath(stateDir string) (string, error) {
	candidate := strings.TrimSpace(stateDir)
	if candidate == "" {
		candidate = config.DefaultStateDir()
	}
	expanded, err := config.ExpandPath(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve credential metadata state dir: %w", err)
	}
	return filepath.Join(expanded, "credentials.json"), nil
}

func updateCredentialMetadata(stateDir string, kind CredentialKind, update func(*CredentialMetadata)) error {
	store, err := LoadCredentialMetadata(stateDir)
	if err != nil {
		return err
	}
	meta := store.Credentials[kind]
	update(&meta)
	store.Credentials[kind] = meta
	return SaveCredentialMetadata(stateDir, store)
}

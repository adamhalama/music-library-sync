package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type CredentialStatus struct {
	Kind               CredentialKind
	Title              string
	Health             CredentialHealth
	StorageSource      CredentialStorageSource
	Summary            string
	LastCheckedAt      time.Time
	LastFailureKind    string
	LastFailureMessage string
}

func InspectSoundCloudClientID(stateDir string) CredentialStatus {
	status := CredentialStatus{
		Kind:    CredentialKindSoundCloudClientID,
		Title:   "SoundCloud client ID",
		Health:  CredentialHealthMissing,
		Summary: "Missing. Save a SoundCloud client ID in macOS Keychain or set SCDL_CLIENT_ID.",
	}
	value, source, err := ResolveSoundCloudClientIDWithSource()
	if err == nil && strings.TrimSpace(value) != "" {
		status.StorageSource = source
		switch source {
		case CredentialStorageSourceKeychain:
			status.Health = CredentialHealthAvailable
			status.Summary = "Available in macOS Keychain."
		case CredentialStorageSourceEnv:
			status.Health = CredentialHealthExternalOverride
			status.Summary = "Available via environment override."
		default:
			status.Health = CredentialHealthAvailable
			status.Summary = "Available."
		}
	} else if err != nil && !errors.Is(err, ErrSoundCloudClientIDNotFound) {
		status.Summary = err.Error()
	}
	return applyCredentialMetadata(status, stateDir)
}

func InspectDeemixARL(stateDir string) CredentialStatus {
	status := CredentialStatus{
		Kind:    CredentialKindDeemixARL,
		Title:   "Deezer ARL",
		Health:  CredentialHealthMissing,
		Summary: "Missing. Save Deezer ARL in macOS Keychain or set UDL_DEEMIX_ARL.",
	}
	value, source, err := ResolveDeemixARLWithSource()
	if err == nil && strings.TrimSpace(value) != "" {
		status.StorageSource = source
		switch source {
		case CredentialStorageSourceKeychain:
			status.Health = CredentialHealthAvailable
			status.Summary = "Available in macOS Keychain."
		case CredentialStorageSourceEnv:
			status.Health = CredentialHealthExternalOverride
			status.Summary = "Available via environment override."
		default:
			status.Health = CredentialHealthAvailable
			status.Summary = "Available."
		}
	} else if err != nil && !errors.Is(err, ErrDeemixARLNotFound) {
		status.Summary = err.Error()
	}
	return applyCredentialMetadata(status, stateDir)
}

func InspectSpotifyCredentials(stateDir string) CredentialStatus {
	status := CredentialStatus{
		Kind:    CredentialKindSpotifyApp,
		Title:   "Spotify app credentials",
		Health:  CredentialHealthMissing,
		Summary: "Missing. Save Spotify client ID/secret in macOS Keychain, set env vars, or use ~/.spotdl/config.json.",
	}
	creds, source, err := ResolveSpotifyCredentialsWithSource()
	if err == nil && strings.TrimSpace(creds.ClientID) != "" && strings.TrimSpace(creds.ClientSecret) != "" {
		status.StorageSource = source
		switch source {
		case CredentialStorageSourceKeychain:
			status.Health = CredentialHealthAvailable
			status.Summary = "Available in macOS Keychain."
		case CredentialStorageSourceEnv:
			status.Health = CredentialHealthExternalOverride
			status.Summary = "Available via environment override."
		case CredentialStorageSourceSpotDL:
			status.Health = CredentialHealthExternalOverride
			status.Summary = "Available via ~/.spotdl/config.json compatibility fallback."
		default:
			status.Health = CredentialHealthAvailable
			status.Summary = "Available."
		}
	} else if err != nil && !errors.Is(err, ErrSpotifyCredentialsNotFound) {
		status.Summary = err.Error()
	}
	return applyCredentialMetadata(status, stateDir)
}

func applyCredentialMetadata(status CredentialStatus, stateDir string) CredentialStatus {
	store, err := LoadCredentialMetadata(stateDir)
	if err != nil {
		return status
	}
	meta, ok := store.Credentials[status.Kind]
	if !ok {
		return status
	}
	status.LastCheckedAt = meta.LastCheckedAt
	status.LastFailureKind = meta.LastFailureKind
	status.LastFailureMessage = meta.LastFailureMessage
	if status.StorageSource == CredentialStorageSourceNone {
		status.StorageSource = meta.StorageSource
	}
	if strings.TrimSpace(meta.LastFailureKind) == "" {
		return status
	}
	if meta.StorageSource != CredentialStorageSourceNone && status.StorageSource != CredentialStorageSourceNone && meta.StorageSource != status.StorageSource {
		return status
	}
	status.Health = CredentialHealthNeedsRefresh
	if strings.TrimSpace(meta.LastFailureMessage) != "" {
		status.Summary = meta.LastFailureMessage
	} else {
		status.Summary = fmt.Sprintf("%s needs refresh.", status.Title)
	}
	return status
}

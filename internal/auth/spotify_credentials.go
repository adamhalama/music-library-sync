package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrSpotifyCredentialsNotFound = errors.New("spotify credentials not found")

const (
	spotifyKeychainService       = "udl.spotify"
	spotifyKeychainAccountID     = "client_id"
	spotifyKeychainAccountSecret = "client_secret"
)

type SpotifyCredentials struct {
	ClientID     string
	ClientSecret string
}

type spotifySpotDLConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

type SpotifyCredentialsResolver struct {
	Getenv   func(string) string
	ReadFile func(string) ([]byte, error)
	HomeDir  func() (string, error)
	Command  commandRunner
}

func ResolveSpotifyCredentials() (SpotifyCredentials, error) {
	creds, _, err := ResolveSpotifyCredentialsWithSource()
	return creds, err
}

func ResolveSpotifyCredentialsWithSource() (SpotifyCredentials, CredentialStorageSource, error) {
	return SpotifyCredentialsResolver{
		Getenv:   os.Getenv,
		ReadFile: os.ReadFile,
		HomeDir:  os.UserHomeDir,
		Command:  runCommandOutput,
	}.ResolveWithSource()
}

func (r SpotifyCredentialsResolver) Resolve() (SpotifyCredentials, error) {
	creds, _, err := r.ResolveWithSource()
	return creds, err
}

func (r SpotifyCredentialsResolver) ResolveWithSource() (SpotifyCredentials, CredentialStorageSource, error) {
	getenv := r.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	clientID := strings.TrimSpace(getenv("UDL_SPOTIFY_CLIENT_ID"))
	clientSecret := strings.TrimSpace(getenv("UDL_SPOTIFY_CLIENT_SECRET"))
	if clientID != "" && clientSecret != "" {
		return SpotifyCredentials{ClientID: clientID, ClientSecret: clientSecret}, CredentialStorageSourceEnv, nil
	}
	if clientID != "" || clientSecret != "" {
		return SpotifyCredentials{}, CredentialStorageSourceEnv, fmt.Errorf("both UDL_SPOTIFY_CLIENT_ID and UDL_SPOTIFY_CLIENT_SECRET are required")
	}

	command := r.Command
	if command == nil {
		command = runCommandOutput
	}
	keychainClientID := keychainCredential(command, spotifyKeychainService, spotifyKeychainAccountID)
	keychainClientSecret := keychainCredential(command, spotifyKeychainService, spotifyKeychainAccountSecret)
	if keychainClientID != "" && keychainClientSecret != "" {
		return SpotifyCredentials{
			ClientID:     keychainClientID,
			ClientSecret: keychainClientSecret,
		}, CredentialStorageSourceKeychain, nil
	}

	readFile := r.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	homeDir := r.HomeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	home, err := homeDir()
	if err != nil {
		return SpotifyCredentials{}, CredentialStorageSourceNone, ErrSpotifyCredentialsNotFound
	}
	configPath := filepath.Join(home, ".spotdl", "config.json")
	payload, err := readFile(configPath)
	if err != nil {
		return SpotifyCredentials{}, CredentialStorageSourceNone, ErrSpotifyCredentialsNotFound
	}

	var cfg spotifySpotDLConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return SpotifyCredentials{}, CredentialStorageSourceSpotDL, fmt.Errorf("parse spotdl config %s: %w", configPath, err)
	}
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return SpotifyCredentials{}, CredentialStorageSourceSpotDL, ErrSpotifyCredentialsNotFound
	}
	return SpotifyCredentials{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}, CredentialStorageSourceSpotDL, nil
}

func SaveSpotifyCredentials(creds SpotifyCredentials) error {
	if strings.TrimSpace(creds.ClientID) == "" || strings.TrimSpace(creds.ClientSecret) == "" {
		return fmt.Errorf("spotify client id and client secret must not be empty")
	}
	if err := saveKeychainCredential(runCommandOutput, spotifyKeychainService, spotifyKeychainAccountID, creds.ClientID); err != nil {
		return fmt.Errorf("save spotify client id to keychain: %w", err)
	}
	if err := saveKeychainCredential(runCommandOutput, spotifyKeychainService, spotifyKeychainAccountSecret, creds.ClientSecret); err != nil {
		return fmt.Errorf("save spotify client secret to keychain: %w", err)
	}
	return nil
}

func RemoveSpotifyCredentials() error {
	if err := deleteKeychainCredential(runCommandOutput, spotifyKeychainService, spotifyKeychainAccountID); err != nil {
		return fmt.Errorf("remove spotify client id from keychain: %w", err)
	}
	if err := deleteKeychainCredential(runCommandOutput, spotifyKeychainService, spotifyKeychainAccountSecret); err != nil {
		return fmt.Errorf("remove spotify client secret from keychain: %w", err)
	}
	return nil
}

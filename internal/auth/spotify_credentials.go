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
	return SpotifyCredentialsResolver{
		Getenv:   os.Getenv,
		ReadFile: os.ReadFile,
		HomeDir:  os.UserHomeDir,
		Command:  runCommandOutput,
	}.Resolve()
}

func (r SpotifyCredentialsResolver) Resolve() (SpotifyCredentials, error) {
	getenv := r.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	clientID := strings.TrimSpace(getenv("UDL_SPOTIFY_CLIENT_ID"))
	clientSecret := strings.TrimSpace(getenv("UDL_SPOTIFY_CLIENT_SECRET"))
	if clientID != "" && clientSecret != "" {
		return SpotifyCredentials{ClientID: clientID, ClientSecret: clientSecret}, nil
	}
	if clientID != "" || clientSecret != "" {
		return SpotifyCredentials{}, fmt.Errorf("both UDL_SPOTIFY_CLIENT_ID and UDL_SPOTIFY_CLIENT_SECRET are required")
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
		}, nil
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
		return SpotifyCredentials{}, ErrSpotifyCredentialsNotFound
	}
	configPath := filepath.Join(home, ".spotdl", "config.json")
	payload, err := readFile(configPath)
	if err != nil {
		return SpotifyCredentials{}, ErrSpotifyCredentialsNotFound
	}

	var cfg spotifySpotDLConfig
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return SpotifyCredentials{}, fmt.Errorf("parse spotdl config %s: %w", configPath, err)
	}
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return SpotifyCredentials{}, ErrSpotifyCredentialsNotFound
	}
	return SpotifyCredentials{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}, nil
}

func keychainCredential(command commandRunner, service string, account string) string {
	raw, err := command(
		"security",
		"find-generic-password",
		"-s", service,
		"-a", account,
		"-w",
	)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

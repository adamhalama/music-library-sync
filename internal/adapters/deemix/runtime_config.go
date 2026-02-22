package deemix

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
)

type spotifyPluginConfig struct {
	ClientID       string `json:"clientId"`
	ClientSecret   string `json:"clientSecret"`
	FallbackSearch bool   `json:"fallbackSearch"`
}

func PrepareRuntimeConfig(source config.Source) (string, error) {
	arl := strings.TrimSpace(source.DeezerARL)
	if arl == "" {
		return "", fmt.Errorf("missing Deezer ARL for deemix runtime")
	}
	clientID := strings.TrimSpace(source.SpotifyClientID)
	clientSecret := strings.TrimSpace(source.SpotifyClientSecret)
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("missing Spotify client credentials for deemix runtime")
	}

	prefix := "udl-deemix-"
	if strings.TrimSpace(source.ID) != "" {
		prefix += sanitizeIDComponent(source.ID) + "-"
	}
	runtimeDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return "", fmt.Errorf("create deemix runtime dir: %w", err)
	}

	configDir := filepath.Join(runtimeDir, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		_ = os.RemoveAll(runtimeDir)
		return "", fmt.Errorf("create deemix config dir: %w", err)
	}

	arlPath := filepath.Join(configDir, ".arl")
	if err := os.WriteFile(arlPath, []byte(arl), 0o600); err != nil {
		_ = os.RemoveAll(runtimeDir)
		return "", fmt.Errorf("write deemix ARL: %w", err)
	}

	spotifyDir := filepath.Join(configDir, "spotify")
	if err := os.MkdirAll(spotifyDir, 0o700); err != nil {
		_ = os.RemoveAll(runtimeDir)
		return "", fmt.Errorf("create deemix spotify config dir: %w", err)
	}

	payload, err := json.MarshalIndent(spotifyPluginConfig{
		ClientID:       clientID,
		ClientSecret:   clientSecret,
		FallbackSearch: true,
	}, "", "  ")
	if err != nil {
		_ = os.RemoveAll(runtimeDir)
		return "", fmt.Errorf("encode deemix spotify config: %w", err)
	}

	spotifyConfigPath := filepath.Join(spotifyDir, "config.json")
	if err := os.WriteFile(spotifyConfigPath, payload, 0o600); err != nil {
		_ = os.RemoveAll(runtimeDir)
		return "", fmt.Errorf("write deemix spotify config: %w", err)
	}

	return runtimeDir, nil
}

func CleanupRuntimeConfig(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil
	}
	return os.RemoveAll(trimmed)
}

func sanitizeIDComponent(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "source"
	}

	b := strings.Builder{}
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	return b.String()
}

package bridge

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed playlist_sync_helper.py
var helperScript string

type Client struct {
	PythonBin  string
	PythonPath string
}

type Playlist struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Attribute  int      `json:"attribute"`
	ParentID   string   `json:"parent_id"`
	ContentIDs []string `json:"content_ids"`
}

type Content struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	FolderPath string `json:"folder_path"`
}

type InspectResponse struct {
	Playlists []Playlist `json:"playlists"`
	Contents  []Content  `json:"contents"`
}

type ApplyRequest struct {
	DBDir                     string   `json:"db_dir"`
	TargetPlaylistID          string   `json:"target_playlist_id,omitempty"`
	TargetPlaylistName        string   `json:"target_playlist_name"`
	CreatePlaylistIfMissing   bool     `json:"create_playlist_if_missing"`
	ExpectedCurrentContentIDs []string `json:"expected_current_content_ids"`
	FinalContentIDs           []string `json:"final_content_ids"`
}

type ApplyResponse struct {
	PlaylistID      string   `json:"playlist_id"`
	PlaylistName    string   `json:"playlist_name"`
	FinalContentIDs []string `json:"final_content_ids"`
	FinalTrackCount int      `json:"final_track_count"`
	CreatedPlaylist bool     `json:"created_playlist"`
}

func (c Client) Inspect(ctx context.Context, dbDir string) (InspectResponse, error) {
	var resp InspectResponse
	err := c.run(ctx, map[string]any{
		"op":     "inspect",
		"db_dir": dbDir,
	}, &resp)
	return resp, err
}

func (c Client) Apply(ctx context.Context, req ApplyRequest) (ApplyResponse, error) {
	var resp ApplyResponse
	payload := map[string]any{
		"op":      "apply",
		"request": req,
	}
	err := c.run(ctx, payload, &resp)
	return resp, err
}

func (c Client) run(ctx context.Context, payload any, out any) error {
	pythonBin := strings.TrimSpace(c.PythonBin)
	if pythonBin == "" {
		pythonBin = "python3"
	}

	dir, err := os.MkdirTemp("", "udl-rb-helper-*")
	if err != nil {
		return fmt.Errorf("create helper temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	helperPath := filepath.Join(dir, "playlist_sync_helper.py")
	if err := os.WriteFile(helperPath, []byte(helperScript), 0o700); err != nil {
		return fmt.Errorf("write helper script: %w", err)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode helper request: %w", err)
	}

	cmd := exec.CommandContext(ctx, pythonBin, helperPath)
	cmd.Stdin = bytes.NewReader(body)
	cmd.Env = c.env()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			return fmt.Errorf("run pyrekordbox helper: %w", err)
		}
		return fmt.Errorf("run pyrekordbox helper: %w: %s", err, detail)
	}
	if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
		return fmt.Errorf("decode pyrekordbox helper response: %w: %s", err, strings.TrimSpace(stdout.String()))
	}
	return nil
}

func (c Client) env() []string {
	env := os.Environ()
	pythonPath := firstNonEmpty(strings.TrimSpace(c.PythonPath), strings.TrimSpace(os.Getenv("UDL_REKORDBOX_PYTHONPATH")))
	if pythonPath == "" {
		return env
	}
	merged := pythonPath
	if existing := os.Getenv("PYTHONPATH"); strings.TrimSpace(existing) != "" {
		merged += string(os.PathListSeparator) + existing
	}
	result := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, "PYTHONPATH=") {
			continue
		}
		result = append(result, item)
	}
	return append(result, "PYTHONPATH="+merged)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

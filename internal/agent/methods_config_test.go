package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func writeAgentTestConfig(t *testing.T, dir string) string {
	t.Helper()
	for _, name := range []string{"state", "music"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "udl.yaml")
	payload := `version: 1
defaults:
  state_dir: ` + filepath.Join(dir, "state") + `
  archive_file: archive.txt
  threads: 1
  continue_on_error: true
  command_timeout_seconds: 900
sources:
  - id: source-a
    type: soundcloud
    enabled: true
    target_dir: ` + filepath.Join(dir, "music") + `
    url: https://soundcloud.com/user/likes
    state_file: source-a.state
    adapter:
      kind: scdl
`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigMethodsLoadValidateReadAndWriteCanonicalFile(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentTestConfig(t, dir)
	server := &Server{WorkingDir: dir, ConfigPath: path}

	loaded, rpcErr := server.loadConfig()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	loadedPayload, err := json.Marshal(loaded)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"DeezerARL", "spotify_client_secret", "SpotifyClientSecret"} {
		if strings.Contains(string(loadedPayload), forbidden) {
			t.Fatalf("secret-bearing runtime field leaked: %s", loadedPayload)
		}
	}

	readValue, rpcErr := server.readConfigFile(mustJSON(t, map[string]string{"path": path}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	read := readValue.(configFileResult)
	if read.Config.Sources[0].ID != "source-a" || !strings.Contains(read.Content, "source-a") {
		t.Fatalf("unexpected read result: %+v", read)
	}
	read.Config.Defaults.Threads = 3
	writeValue, rpcErr := server.writeConfigFile(mustJSON(t, configFileParams{Path: path, Config: read.Config}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	written := writeValue.(configFileResult)
	if !strings.Contains(written.Content, "threads: 3") {
		t.Fatalf("write did not return canonical YAML:\n%s", written.Content)
	}
	reread, err := config.LoadSingleFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Defaults.Threads != 3 {
		t.Fatalf("canonical write was not persisted: %+v", reread.Defaults)
	}
	if _, rpcErr := server.validateConfig(mustJSON(t, map[string]any{"config": reread})); rpcErr != nil {
		t.Fatalf("valid config rejected: %v", rpcErr)
	}
}

func TestConfigMethodsReturnStructuredValidationAndEnforceScope(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentTestConfig(t, dir)
	server := &Server{WorkingDir: dir, ConfigPath: path}
	invalid := config.DefaultConfig()
	_, rpcErr := server.validateConfig(mustJSON(t, map[string]any{"config": invalid}))
	if rpcErr == nil || rpcErr.Code != CodeInvalidParams || !strings.Contains(string(rpcErr.Data), "problems") {
		t.Fatalf("expected structured validation problems: %+v", rpcErr)
	}
	outside := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outside, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, rpcErr := server.readConfigFile(mustJSON(t, map[string]string{"path": outside})); rpcErr == nil || !strings.Contains(rpcErr.Message, "outside") {
		t.Fatalf("expected scope rejection, got %+v", rpcErr)
	}
}

func TestConfigReadRejectsInvalidYAMLAndSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "udl.yaml")
	if err := os.WriteFile(path, []byte("version: [bad\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := &Server{WorkingDir: dir, ConfigPath: path}
	if _, rpcErr := server.readConfigFile(mustJSON(t, map[string]string{"path": path})); rpcErr == nil {
		t.Fatal("expected invalid YAML error")
	}

	target := filepath.Join(dir, "target.yaml")
	if err := os.WriteFile(target, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	server.ConfigPath = link
	if _, rpcErr := server.readConfigFile(mustJSON(t, map[string]string{"path": link})); rpcErr == nil || !strings.Contains(rpcErr.Message, "symbolic-link") {
		t.Fatalf("expected symlink rejection, got %+v", rpcErr)
	}
}

func TestConfigWriteRejectsExternalModificationAndPreservesIt(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentTestConfig(t, dir)
	server := &Server{WorkingDir: dir, ConfigPath: path}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadSingleFile(path)
	if err != nil {
		t.Fatal(err)
	}
	external := append(append([]byte(nil), original...), []byte("# external edit\n")...)
	if err := os.WriteFile(path, external, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Defaults.Threads = 9
	_, rpcErr := server.writeConfigFile(mustJSON(t, configFileParams{
		Path: path, Config: cfg, ExpectedContentSHA256: contentSHA256(original),
	}))
	if rpcErr == nil || rpcErr.Code != CodeRunConflict {
		t.Fatalf("expected external-modification conflict, got %+v", rpcErr)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(external) {
		t.Fatalf("conflict overwrote external content\n got: %s\nwant: %s", after, external)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

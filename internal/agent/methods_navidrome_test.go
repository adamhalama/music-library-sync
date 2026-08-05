package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/navidrome"
)

func newNavidromeAgentServer(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, navidrome.ProjectConfigName)
	cfg := navidrome.DefaultConfig()
	cfg.Enabled = true
	cfg.Server.Username = "jaa"
	// A closed loopback port, so a machine that really is running Navidrome on
	// 4533 with a password in Keychain cannot turn these into live calls.
	cfg.Server.Port = 1
	cfg.Server.URL = "http://127.0.0.1:1"
	if err := navidrome.Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	server := &Server{
		Conn: NewConn(strings.NewReader(""), &synchronizedBuffer{}), Runs: NewRunRegistry(),
		WorkingDir: dir, NavidromeConfigPath: path, runContext: context.Background(),
	}
	return server, path
}

func TestNavidromeMethodsAreAdvertised(t *testing.T) {
	advertised := map[string]bool{}
	for _, method := range protocolMethods {
		advertised[method] = true
	}
	for _, method := range []string{
		"navidrome.config.read", "navidrome.config.write",
		"navidrome.deps.status", "navidrome.deps.ensure",
		"navidrome.status", "navidrome.service.control",
		"navidrome.setup.plan", "navidrome.setup.apply",
		"navidrome.playlists.refresh", "navidrome.playlists.deriveGenres",
		"navidrome.playlists.saveGenres",
		"navidrome.favorites.plan", "navidrome.favorites.apply",
		"navidrome.backup.create",
	} {
		if !advertised[method] {
			t.Fatalf("method %q is not advertised in initialize", method)
		}
	}
}

func TestNavidromeConfigReadWriteRoundTrip(t *testing.T) {
	server, path := newNavidromeAgentServer(t)

	value, rpcErr := server.readNavidromeConfig()
	if rpcErr != nil {
		t.Fatalf("readNavidromeConfig: %v", rpcErr)
	}
	read := value.(navidromeConfigResult)
	if read.Path != path {
		t.Fatalf("path = %q, want %q", read.Path, path)
	}
	if read.Config.Server.Username != "jaa" {
		t.Fatalf("config = %+v", read.Config)
	}
	if strings.Contains(strings.ToLower(read.Content), "password") {
		t.Fatalf("the config frame must never mention a password:\n%s", read.Content)
	}

	read.Config.Scan.Schedule = "@every 2h"
	written, rpcErr := server.writeNavidromeConfig(mustJSON(t, map[string]any{"config": read.Config}))
	if rpcErr != nil {
		t.Fatalf("writeNavidromeConfig: %v", rpcErr)
	}
	if written.(navidromeConfigResult).Config.Scan.Schedule != "@every 2h" {
		t.Fatalf("write did not persist: %+v", written)
	}
}

func TestNavidromeConfigWriteRejectsInvalidConfig(t *testing.T) {
	server, _ := newNavidromeAgentServer(t)
	invalid := navidrome.DefaultConfig()
	// Port 0 would be normalized back to the default (0 means "unset"), so an
	// out-of-range value is what actually exercises validation.
	invalid.Server.Port = 70000
	_, rpcErr := server.writeNavidromeConfig(mustJSON(t, map[string]any{"config": invalid}))
	if rpcErr == nil {
		t.Fatalf("an invalid config must be refused")
	}
	if !strings.Contains(rpcErr.Message, "invalid") {
		t.Fatalf("message = %q", rpcErr.Message)
	}
}

func TestNavidromeDepsEnsureRequiresConfirmation(t *testing.T) {
	server, _ := newNavidromeAgentServer(t)
	_, rpcErr := server.ensureNavidromeDeps(mustJSON(t, map[string]any{"confirm": false}))
	if rpcErr == nil || !strings.Contains(rpcErr.Message, "confirmation") {
		t.Fatalf("error = %+v, want an explicit-confirmation refusal", rpcErr)
	}
}

func TestNavidromeSetupApplyRejectsAnUnsignedPlan(t *testing.T) {
	server, _ := newNavidromeAgentServer(t)
	_, rpcErr := server.navidromeSetupApply(mustJSON(t, map[string]any{
		"plan": navidrome.SetupPlan{Version: navidrome.SetupPlanVersion},
	}))
	if rpcErr == nil || !strings.Contains(rpcErr.Message, "stale or modified") {
		t.Fatalf("error = %+v", rpcErr)
	}
}

func TestNavidromeFavoritesApplyRejectsAnUnsignedPlan(t *testing.T) {
	server, _ := newNavidromeAgentServer(t)
	_, rpcErr := server.navidromeFavoritesApply(mustJSON(t, map[string]any{
		"plan": navidrome.FavoritePlan{Version: navidrome.FavoritePlanVersion},
	}))
	if rpcErr == nil || !strings.Contains(rpcErr.Message, "stale or modified") {
		t.Fatalf("error = %+v", rpcErr)
	}
}

func TestNavidromeSaveGenresRefusesAnEmptyAllowlist(t *testing.T) {
	server, _ := newNavidromeAgentServer(t)
	_, rpcErr := server.navidromeSaveGenres(mustJSON(t, map[string]any{"genres": []string{" ", ""}}))
	if rpcErr == nil || !strings.Contains(rpcErr.Message, "must not be empty") {
		t.Fatalf("error = %+v", rpcErr)
	}
}

func TestNavidromeServiceControlRejectsAnUnknownAction(t *testing.T) {
	server, _ := newNavidromeAgentServer(t)
	_, rpcErr := server.navidromeServiceControl(mustJSON(t, map[string]any{"action": "delete"}))
	if rpcErr == nil || !strings.Contains(rpcErr.Message, "start, stop, or restart") {
		t.Fatalf("error = %+v", rpcErr)
	}
}

func TestNavidromeStatusFrameToleratesNoCredentials(t *testing.T) {
	server, _ := newNavidromeAgentServer(t)
	value, rpcErr := server.navidromeStatus()
	if rpcErr != nil {
		t.Fatalf("status must render without credentials: %v", rpcErr)
	}
	status := value.(navidrome.Status)
	// Collections must serialize as arrays, never Go nil, so the Swift DTOs
	// never have to special-case JSON null.
	payload, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"problems", "managed_playlists"} {
		if decoded[key] == nil {
			t.Fatalf("status.%s encoded as null; Swift decoding expects an array:\n%s", key, payload)
		}
	}
	if strings.Contains(strings.ToLower(string(payload)), `"password":`) {
		t.Fatalf("the status frame must never carry a password field:\n%s", payload)
	}
}

func TestNavidromeConfigReadFallsBackToDefaultsWhenNoFileExists(t *testing.T) {
	dir := t.TempDir()
	// Isolate discovery from the developer's real home. Without this the test
	// reads ~/.config/udl/navidrome.yaml and passes or fails depending on
	// whether the machine happens to have Phone Library set up.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	server := &Server{
		Conn: NewConn(strings.NewReader(""), &synchronizedBuffer{}), Runs: NewRunRegistry(),
		WorkingDir: dir, NavidromeConfigPath: filepath.Join(dir, "missing.yaml"),
		runContext: context.Background(),
	}
	// An explicit path that does not exist is a configuration error, not a
	// silent fallback: the user asked for that file.
	if _, rpcErr := server.readNavidromeConfig(); rpcErr == nil {
		t.Fatalf("a missing explicit config must be reported")
	}

	server.NavidromeConfigPath = ""
	value, rpcErr := server.readNavidromeConfig()
	if rpcErr != nil {
		t.Fatalf("discovery with no file must yield defaults: %v", rpcErr)
	}
	read := value.(navidromeConfigResult)
	if read.Config.Version != navidrome.ConfigVersion {
		t.Fatalf("config = %+v", read.Config)
	}
	if read.Content == "" {
		t.Fatalf("content must fall back to the rendered defaults")
	}
	if _, err := os.Stat(read.Path); err == nil {
		t.Fatalf("reading the config must not create %s", read.Path)
	}
}

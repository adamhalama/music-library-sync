package agent

import (
	"encoding/json"
	"testing"

	"github.com/jaa/update-downloads/internal/navidrome"
)

func TestConfigWriteKeepsRemoteSettingsAnOlderClientDoesNotKnow(t *testing.T) {
	current := navidrome.DefaultConfig()
	current.Mode = navidrome.ModeRemote
	current.Remote = &navidrome.RemoteConfig{SSHTarget: "udl-mirror", RsyncPath: "/opt/homebrew/bin/rsync", SSHPath: "/usr/bin/ssh", MaxDelete: 250}

	// What a pre-remote native build sends: the full config minus the new keys.
	edited := navidrome.DefaultConfig()
	edited.Phone.Connected = true
	payload := map[string]any{"config": map[string]any{
		"version": edited.Version, "enabled": edited.Enabled, "server": edited.Server,
		"paths": edited.Paths, "scan": edited.Scan, "backup": edited.Backup,
		"playlists": edited.Playlists, "phone": edited.Phone,
	}}
	params, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Config *navidrome.Config `json:"config"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		t.Fatal(err)
	}
	if err := keepOmittedRemoteSettings(params, request.Config, current); err != nil {
		t.Fatal(err)
	}
	if request.Config.Mode != navidrome.ModeRemote || request.Config.Remote == nil || request.Config.Remote.SSHTarget != "udl-mirror" {
		t.Fatalf("remote settings were dropped: mode=%q remote=%+v", request.Config.Mode, request.Config.Remote)
	}
	if !request.Config.Phone.Connected {
		t.Fatalf("the client's own edit was lost")
	}
	request.Config.Remote.SSHTarget = "changed"
	if current.Remote.SSHTarget != "udl-mirror" {
		t.Fatalf("the stored config must not alias the request")
	}
}

func TestConfigWriteHonoursAnExplicitSwitchBackToLocal(t *testing.T) {
	current := navidrome.DefaultConfig()
	current.Mode = navidrome.ModeRemote
	current.Remote = &navidrome.RemoteConfig{SSHTarget: "udl-mirror"}

	params := []byte(`{"config":{"version":1,"mode":"local","remote":null}}`)
	var request struct {
		Config *navidrome.Config `json:"config"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		t.Fatal(err)
	}
	if err := keepOmittedRemoteSettings(params, request.Config, current); err != nil {
		t.Fatal(err)
	}
	if request.Config.Mode != navidrome.ModeLocal || request.Config.Remote != nil {
		t.Fatalf("an explicit local switch must win: mode=%q remote=%+v", request.Config.Mode, request.Config.Remote)
	}
}

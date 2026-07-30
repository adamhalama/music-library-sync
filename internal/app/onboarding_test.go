package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectOnboardingStateNoSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nsources: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, needed := DetectOnboardingState(OnboardingOptions{
		ConfigPath: path, FreeDLConfigPath: filepath.Join(dir, "missing-freedl.yaml"),
		WorkingDir: dir, Env: map[string]string{},
	})
	if !needed || state.Reason != OnboardingReasonNoSources || !state.AutoStarted {
		t.Fatalf("expected automatic no-sources onboarding: needed=%v state=%+v", needed, state)
	}
}

func TestDetectOnboardingStateInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("version: [invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, needed := DetectOnboardingState(OnboardingOptions{
		ConfigPath: path, FreeDLConfigPath: filepath.Join(dir, "missing-freedl.yaml"),
		WorkingDir: dir, Env: map[string]string{},
	})
	if !needed || state.Reason != OnboardingReasonInvalidConfig || len(state.DetailLines) == 0 {
		t.Fatalf("expected invalid-config onboarding: needed=%v state=%+v", needed, state)
	}
}

func TestDetectOnboardingStateConfiguredSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	payload := `version: 1
defaults:
  state_dir: ` + filepath.Join(dir, "state") + `
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
	state, needed := DetectOnboardingState(OnboardingOptions{
		ConfigPath: path, FreeDLConfigPath: filepath.Join(dir, "missing-freedl.yaml"),
		WorkingDir: dir, Env: map[string]string{},
	})
	if needed {
		t.Fatalf("did not expect onboarding for valid source: %+v", state)
	}
}

func TestOnboardingStateJSONFieldNames(t *testing.T) {
	got, err := json.Marshal(OnboardingState{
		Reason: OnboardingReasonNoSources, AutoStarted: true,
		ConfigPath: "/config", ConfigContextLabel: "context",
		DetailLines: []string{"detail"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"reason":"no_sources","auto_started":true,"config_path":"/config","config_context_label":"context","detail_lines":["detail"],"defaults":{"state_dir":"","archive_file":"","threads":0,"continue_on_error":false,"command_timeout_seconds":0}}`
	if string(got) != want {
		t.Fatalf("unexpected JSON\n got: %s\nwant: %s", got, want)
	}
}

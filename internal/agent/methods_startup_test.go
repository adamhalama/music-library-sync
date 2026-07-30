package agent

import (
	"encoding/json"
	"testing"

	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/engine"
)

func TestStartupAndSourceCapabilitiesMatchApplicationRules(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentTestConfig(t, dir)
	available := func(kind auth.CredentialKind, title string) auth.CredentialStatus {
		return auth.CredentialStatus{Kind: kind, Title: title, Health: auth.CredentialHealthAvailable}
	}
	inspectors := app.CredentialInspectors{
		SoundCloudClientID: func(string) auth.CredentialStatus {
			return available(auth.CredentialKindSoundCloudClientID, "SoundCloud client ID")
		},
		DeemixARL: func(string) auth.CredentialStatus {
			return available(auth.CredentialKindDeemixARL, "Deezer ARL")
		},
		SpotifyCredentials: func(string) auth.CredentialStatus {
			return available(auth.CredentialKindSpotifyApp, "Spotify app credentials")
		},
	}
	server := &Server{
		WorkingDir: dir, ConfigPath: path, StartupInspectors: &inspectors,
	}

	onboardingValue, rpcErr := server.onboardingState()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	onboarding := onboardingValue.(onboardingStateResult)
	if onboarding.Needed {
		t.Fatalf("valid configured source unexpectedly needs onboarding: %+v", onboarding)
	}

	attentionValue, rpcErr := server.startupAttention()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	attention := attentionValue.(startupAttentionResult)
	if attention.Status != "ready" || attention.Attention != nil {
		t.Fatalf("healthy credentials should be ready: %+v", attention)
	}

	capabilityValue, rpcErr := server.sourceCapabilities()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	payload, err := json.Marshal(capabilityValue)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Sources []sourceCapability `json:"sources"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Sources) != 1 {
		t.Fatalf("unexpected capabilities: %+v", decoded.Sources)
	}
	got := decoded.Sources[0]
	if !got.SupportsPlan || !got.SupportsDownloadOrder || got.SupportsPlanWindow {
		t.Fatalf("SoundCloud capabilities drifted from engine rules: %+v", got)
	}
	if got.DefaultPlanWindow != engine.PlanWindowFirst || got.DefaultDownloadOrder != engine.DownloadOrderNewestFirst {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestStartupAttentionReportsBlockedCredential(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentTestConfig(t, dir)
	inspectors := app.CredentialInspectors{
		SoundCloudClientID: func(string) auth.CredentialStatus {
			return auth.CredentialStatus{
				Kind: auth.CredentialKindSoundCloudClientID, Title: "SoundCloud client ID",
				Health: auth.CredentialHealthNeedsRefresh,
			}
		},
	}
	server := &Server{WorkingDir: dir, ConfigPath: path, StartupInspectors: &inspectors}
	value, rpcErr := server.startupAttention()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	result := value.(startupAttentionResult)
	if result.Status != "blocked" || result.Attention == nil || result.Attention.PrimarySourceID != "source-a" {
		t.Fatalf("expected blocked startup attention: %+v", result)
	}
}

func TestStartupAttentionReportsMissingCredentialAsAttention(t *testing.T) {
	dir := t.TempDir()
	path := writeAgentTestConfig(t, dir)
	inspectors := app.CredentialInspectors{
		SoundCloudClientID: func(string) auth.CredentialStatus {
			return auth.CredentialStatus{
				Kind: auth.CredentialKindSoundCloudClientID, Title: "SoundCloud client ID",
				Health: auth.CredentialHealthMissing,
			}
		},
	}
	server := &Server{WorkingDir: dir, ConfigPath: path, StartupInspectors: &inspectors}
	value, rpcErr := server.startupAttention()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	result := value.(startupAttentionResult)
	if result.Status != "attention" || result.Attention == nil ||
		result.Attention.Severity != app.StartupAttentionSeverityAttention {
		t.Fatalf("expected attention routing: %+v", result)
	}
}

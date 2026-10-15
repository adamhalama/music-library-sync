package app

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

func TestBuildPlanSourceDetails(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "state")
	got := BuildPlanSourceDetails(config.Source{
		ID: "source-a", Type: config.SourceTypeSoundCloud,
		URL: "https://soundcloud.com/user/likes?utm=test", TargetDir: "~/Music",
		StateFile: "source-a.state", Adapter: config.AdapterSpec{Kind: "scdl"},
	}, config.Defaults{StateDir: stateDir}, 10, engine.PlanWindowFirst, true)
	if got.SourceID != "source-a" || got.URL != "https://soundcloud.com/user/likes" {
		t.Fatalf("unexpected details: %+v", got)
	}
	if filepath.Base(got.StateFile) != "source-a.state" || got.PlanWindow != engine.PlanWindowFirst {
		t.Fatalf("paths or plan window were not resolved: %+v", got)
	}
	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"source_id":"source-a","source_type":"soundcloud","adapter":"scdl","url":"https://soundcloud.com/user/likes","target_dir":"` + got.TargetDir + `","state_file":"` + got.StateFile + `","plan_limit":10,"plan_window":"first","dry_run":true}`
	if string(payload) != want {
		t.Fatalf("unexpected JSON\n got: %s\nwant: %s", payload, want)
	}
}

func TestDetectStartupAttentionScopesEnabledSources(t *testing.T) {
	status := func(kind auth.CredentialKind, health auth.CredentialHealth) func(string) auth.CredentialStatus {
		return func(string) auth.CredentialStatus {
			return auth.CredentialStatus{Kind: kind, Title: string(kind), Health: health}
		}
	}
	got := DetectStartupAttention(config.Config{
		Defaults: config.Defaults{StateDir: "/tmp/state"},
		Sources: []config.Source{
			{ID: "soundcloud", Type: config.SourceTypeSoundCloud, Enabled: true, Adapter: config.AdapterSpec{Kind: "scdl"}},
			{ID: "spotify", Type: config.SourceTypeSpotify, Enabled: true, Adapter: config.AdapterSpec{Kind: "deemix"}},
			{ID: "disabled", Type: config.SourceTypeSpotify, Enabled: false, Adapter: config.AdapterSpec{Kind: "deemix"}},
		},
	}, CredentialInspectors{
		SoundCloudClientID: status(auth.CredentialKindSoundCloudClientID, auth.CredentialHealthNeedsRefresh),
		DeemixARL:          status(auth.CredentialKindDeemixARL, auth.CredentialHealthAvailable),
		SpotifyCredentials: status(auth.CredentialKindSpotifyApp, auth.CredentialHealthMissing),
	})
	if got == nil || got.Severity != StartupAttentionSeverityBlocked {
		t.Fatalf("expected blocked startup state: %+v", got)
	}
	if got.IssueCount != 2 || len(got.AffectedSourceIDs) != 2 {
		t.Fatalf("unexpected issue aggregation: %+v", got)
	}
	if got.PrimaryKind != auth.CredentialKindSoundCloudClientID || got.PrimarySourceID != "soundcloud" {
		t.Fatalf("unexpected primary issue: %+v", got)
	}
	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"severity":"blocked","primary_kind":"soundcloud_client_id","primary_source_id":"soundcloud","affected_source_ids":["soundcloud","spotify"],"issue_count":2,"primary_action_label":"Press ` + "`c`" + ` to open Credentials","headline":"Startup Blocked","summary_text":"soundcloud is blocked by a stale soundcloud_client_id. 2 credential blockers affect 2 enabled sources."}`
	if string(payload) != want {
		t.Fatalf("unexpected JSON\n got: %s\nwant: %s", payload, want)
	}
}

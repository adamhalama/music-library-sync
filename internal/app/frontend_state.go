package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

// PlanSourceDetails is the frontend-neutral context displayed while reviewing
// a source plan.
type PlanSourceDetails struct {
	SourceID   string            `json:"source_id"`
	SourceType string            `json:"source_type"`
	Adapter    string            `json:"adapter"`
	URL        string            `json:"url"`
	TargetDir  string            `json:"target_dir"`
	StateFile  string            `json:"state_file"`
	PlanLimit  int               `json:"plan_limit"`
	PlanWindow engine.PlanWindow `json:"plan_window"`
	DryRun     bool              `json:"dry_run"`
}

func BuildPlanSourceDetails(source config.Source, defaults config.Defaults, planLimit int, planWindow engine.PlanWindow, dryRun bool) PlanSourceDetails {
	targetDir := strings.TrimSpace(source.TargetDir)
	if expanded, err := config.ExpandPath(targetDir); err == nil && strings.TrimSpace(expanded) != "" {
		targetDir = expanded
	}
	stateFile := strings.TrimSpace(source.StateFile)
	if resolved, err := config.ResolveStateFile(defaults.StateDir, source.StateFile); err == nil && strings.TrimSpace(resolved) != "" {
		stateFile = resolved
	}
	if targetDir != "" {
		targetDir = filepath.Clean(targetDir)
	}
	if stateFile != "" {
		stateFile = filepath.Clean(stateFile)
	}
	return PlanSourceDetails{
		SourceID: strings.TrimSpace(source.ID), SourceType: string(source.Type),
		Adapter: strings.TrimSpace(source.Adapter.Kind), URL: sanitizePlanURL(source.URL),
		TargetDir: targetDir, StateFile: stateFile, PlanLimit: planLimit,
		PlanWindow: engine.NormalizePlanWindow(planWindow), DryRun: dryRun,
	}
}

func sanitizePlanURL(raw string) string {
	return strings.SplitN(strings.TrimSpace(raw), "?", 2)[0]
}

type StartupAttentionSeverity string

const (
	StartupAttentionSeverityAttention StartupAttentionSeverity = "attention"
	StartupAttentionSeverityBlocked   StartupAttentionSeverity = "blocked"
)

type StartupAttention struct {
	Severity           StartupAttentionSeverity `json:"severity"`
	PrimaryKind        auth.CredentialKind      `json:"primary_kind"`
	PrimarySourceID    string                   `json:"primary_source_id"`
	AffectedSourceIDs  []string                 `json:"affected_source_ids"`
	IssueCount         int                      `json:"issue_count"`
	PrimaryActionLabel string                   `json:"primary_action_label"`
	Headline           string                   `json:"headline"`
	SummaryText        string                   `json:"summary_text"`
}

type CredentialInspectors struct {
	SoundCloudClientID func(string) auth.CredentialStatus
	DeemixARL          func(string) auth.CredentialStatus
	SpotifyCredentials func(string) auth.CredentialStatus
}

func DefaultCredentialInspectors() CredentialInspectors {
	return CredentialInspectors{
		SoundCloudClientID: auth.InspectSoundCloudClientID,
		DeemixARL:          auth.InspectDeemixARL,
		SpotifyCredentials: auth.InspectSpotifyCredentials,
	}
}

type startupCredentialIssue struct {
	kind      auth.CredentialKind
	health    auth.CredentialHealth
	sourceIDs []string
}

func DetectStartupAttention(cfg config.Config, inspectors CredentialInspectors) *StartupAttention {
	if len(cfg.Sources) == 0 {
		return nil
	}
	defaults := DefaultCredentialInspectors()
	if inspectors.SoundCloudClientID == nil {
		inspectors.SoundCloudClientID = defaults.SoundCloudClientID
	}
	if inspectors.DeemixARL == nil {
		inspectors.DeemixARL = defaults.DeemixARL
	}
	if inspectors.SpotifyCredentials == nil {
		inspectors.SpotifyCredentials = defaults.SpotifyCredentials
	}

	stateDir := strings.TrimSpace(cfg.Defaults.StateDir)
	if stateDir == "" {
		stateDir = config.DefaultStateDir()
	}
	statuses := map[auth.CredentialKind]auth.CredentialStatus{
		auth.CredentialKindSoundCloudClientID: inspectors.SoundCloudClientID(stateDir),
		auth.CredentialKindDeemixARL:          inspectors.DeemixARL(stateDir),
		auth.CredentialKindSpotifyApp:         inspectors.SpotifyCredentials(stateDir),
	}
	issues := map[auth.CredentialKind]*startupCredentialIssue{}
	order := []auth.CredentialKind{}
	add := func(kind auth.CredentialKind, sourceID string) {
		status := statuses[kind]
		if status.Health != auth.CredentialHealthMissing && status.Health != auth.CredentialHealthNeedsRefresh {
			return
		}
		if issues[kind] == nil {
			issues[kind] = &startupCredentialIssue{kind: kind, health: status.Health}
			order = append(order, kind)
		}
		if !slices.Contains(issues[kind].sourceIDs, sourceID) {
			issues[kind].sourceIDs = append(issues[kind].sourceIDs, sourceID)
		}
	}
	for _, source := range cfg.Sources {
		if !source.Enabled {
			continue
		}
		switch {
		case source.Type == config.SourceTypeSoundCloud && source.Adapter.Kind == "scdl":
			add(auth.CredentialKindSoundCloudClientID, source.ID)
		case source.Type == config.SourceTypeSpotify && source.Adapter.Kind == "deemix":
			add(auth.CredentialKindDeemixARL, source.ID)
			add(auth.CredentialKindSpotifyApp, source.ID)
		}
	}
	if len(order) == 0 {
		return nil
	}
	primary := issues[order[0]]
	affected := []string{}
	severity := StartupAttentionSeverityAttention
	for _, kind := range order {
		issue := issues[kind]
		if issue.health == auth.CredentialHealthNeedsRefresh {
			severity = StartupAttentionSeverityBlocked
		}
		for _, id := range issue.sourceIDs {
			if !slices.Contains(affected, id) {
				affected = append(affected, id)
			}
		}
	}
	headline := "Startup Attention"
	if severity == StartupAttentionSeverityBlocked {
		headline = "Startup Blocked"
	}
	primaryID := ""
	if len(primary.sourceIDs) > 0 {
		primaryID = primary.sourceIDs[0]
	}
	if primaryID == "" && len(affected) > 0 {
		primaryID = affected[0]
	}
	return &StartupAttention{
		Severity: severity, PrimaryKind: primary.kind, PrimarySourceID: primaryID,
		AffectedSourceIDs: affected, IssueCount: len(order),
		PrimaryActionLabel: "Press `c` to open Credentials", Headline: headline,
		SummaryText: startupAttentionSummary(primary, statuses[primary.kind], len(order), affected),
	}
}

func startupAttentionSummary(primary *startupCredentialIssue, status auth.CredentialStatus, issueCount int, sourceIDs []string) string {
	sourceID := ""
	if primary != nil && len(primary.sourceIDs) > 0 {
		sourceID = primary.sourceIDs[0]
	}
	label := strings.TrimSpace(status.Title)
	if label == "" {
		label = credentialKindLabel(primary.kind)
	}
	var summary string
	switch status.Health {
	case auth.CredentialHealthNeedsRefresh:
		summary = fmt.Sprintf("%s is blocked by a stale %s.", sourceID, strings.ToLower(label))
	case auth.CredentialHealthMissing:
		summary = fmt.Sprintf("%s is missing %s.", sourceID, strings.ToLower(label))
	default:
		summary = fmt.Sprintf("%s needs %s.", sourceID, strings.ToLower(label))
	}
	if issueCount > 1 {
		summary = fmt.Sprintf("%s %d credential blockers affect %d enabled sources.", summary, issueCount, len(sourceIDs))
	}
	return summary
}

func credentialKindLabel(kind auth.CredentialKind) string {
	switch kind {
	case auth.CredentialKindSoundCloudClientID:
		return "SoundCloud client ID"
	case auth.CredentialKindDeemixARL:
		return "Deezer ARL"
	case auth.CredentialKindSpotifyApp:
		return "Spotify app credentials"
	default:
		return "credential"
	}
}

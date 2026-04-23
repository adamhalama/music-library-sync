package cli

import (
	"fmt"
	"slices"
	"strings"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
)

var (
	tuiInspectSoundCloudClientIDStatusFn = auth.InspectSoundCloudClientID
	tuiInspectDeemixARLStatusFn          = auth.InspectDeemixARL
	tuiInspectSpotifyCredentialsStatusFn = auth.InspectSpotifyCredentials
	tuiDetectStartupAttentionFn          = tuiDetectStartupAttention
)

type tuiStartupAttentionSeverity string

const (
	tuiStartupAttentionSeverityAttention tuiStartupAttentionSeverity = "attention"
	tuiStartupAttentionSeverityBlocked   tuiStartupAttentionSeverity = "blocked"
)

type tuiStartupAttentionState struct {
	Severity           tuiStartupAttentionSeverity
	PrimaryKind        auth.CredentialKind
	PrimarySourceID    string
	AffectedSourceIDs  []string
	IssueCount         int
	PrimaryActionLabel string
	Headline           string
	SummaryText        string
}

type tuiStartupCredentialIssue struct {
	Kind      auth.CredentialKind
	Health    auth.CredentialHealth
	SourceIDs []string
}

func tuiDetectStartupAttention(app *AppContext) *tuiStartupAttentionState {
	cfg, err := loadConfig(app)
	if err != nil {
		return nil
	}
	if err := config.Validate(cfg); err != nil {
		return nil
	}
	return tuiDetectStartupAttentionForConfig(cfg)
}

func tuiDetectStartupAttentionForConfig(cfg config.Config) *tuiStartupAttentionState {
	if len(cfg.Sources) == 0 {
		return nil
	}

	stateDir := strings.TrimSpace(cfg.Defaults.StateDir)
	if stateDir == "" {
		stateDir = config.DefaultStateDir()
	}

	statusByKind := map[auth.CredentialKind]auth.CredentialStatus{
		auth.CredentialKindSoundCloudClientID: tuiInspectSoundCloudClientIDStatusFn(stateDir),
		auth.CredentialKindDeemixARL:          tuiInspectDeemixARLStatusFn(stateDir),
		auth.CredentialKindSpotifyApp:         tuiInspectSpotifyCredentialsStatusFn(stateDir),
	}

	issuesByKind := map[auth.CredentialKind]*tuiStartupCredentialIssue{}
	issueOrder := []auth.CredentialKind{}
	for _, source := range cfg.Sources {
		if !source.Enabled {
			continue
		}
		switch {
		case source.Type == config.SourceTypeSoundCloud && source.Adapter.Kind == "scdl":
			if status := statusByKind[auth.CredentialKindSoundCloudClientID]; tuiCredentialStatusBlocksStartup(status) {
				if _, ok := issuesByKind[auth.CredentialKindSoundCloudClientID]; !ok {
					issuesByKind[auth.CredentialKindSoundCloudClientID] = &tuiStartupCredentialIssue{
						Kind:   auth.CredentialKindSoundCloudClientID,
						Health: status.Health,
					}
					issueOrder = append(issueOrder, auth.CredentialKindSoundCloudClientID)
				}
				issue := issuesByKind[auth.CredentialKindSoundCloudClientID]
				if !slices.Contains(issue.SourceIDs, source.ID) {
					issue.SourceIDs = append(issue.SourceIDs, source.ID)
				}
			}
		case source.Type == config.SourceTypeSpotify && source.Adapter.Kind == "deemix":
			for _, kind := range []auth.CredentialKind{auth.CredentialKindDeemixARL, auth.CredentialKindSpotifyApp} {
				status := statusByKind[kind]
				if !tuiCredentialStatusBlocksStartup(status) {
					continue
				}
				if _, ok := issuesByKind[kind]; !ok {
					issuesByKind[kind] = &tuiStartupCredentialIssue{
						Kind:   kind,
						Health: status.Health,
					}
					issueOrder = append(issueOrder, kind)
				}
				issue := issuesByKind[kind]
				if !slices.Contains(issue.SourceIDs, source.ID) {
					issue.SourceIDs = append(issue.SourceIDs, source.ID)
				}
			}
		}
	}

	if len(issueOrder) == 0 {
		return nil
	}

	primary := issuesByKind[issueOrder[0]]
	affectedSourceIDs := []string{}
	severity := tuiStartupAttentionSeverityAttention
	for _, kind := range issueOrder {
		issue := issuesByKind[kind]
		if issue.Health == auth.CredentialHealthNeedsRefresh {
			severity = tuiStartupAttentionSeverityBlocked
		}
		for _, sourceID := range issue.SourceIDs {
			if !slices.Contains(affectedSourceIDs, sourceID) {
				affectedSourceIDs = append(affectedSourceIDs, sourceID)
			}
		}
	}

	headline := "Startup Attention"
	if severity == tuiStartupAttentionSeverityBlocked {
		headline = "Startup Blocked"
	}

	return &tuiStartupAttentionState{
		Severity:           severity,
		PrimaryKind:        primary.Kind,
		PrimarySourceID:    firstNonEmpty(primary.firstSourceID(), firstSourceID(affectedSourceIDs)),
		AffectedSourceIDs:  affectedSourceIDs,
		IssueCount:         len(issueOrder),
		PrimaryActionLabel: "Press `c` to open Credentials",
		Headline:           headline,
		SummaryText:        tuiStartupAttentionSummary(primary, statusByKind[primary.Kind], len(issueOrder), affectedSourceIDs),
	}
}

func tuiCredentialStatusBlocksStartup(status auth.CredentialStatus) bool {
	switch status.Health {
	case auth.CredentialHealthMissing, auth.CredentialHealthNeedsRefresh:
		return true
	default:
		return false
	}
}

func tuiStartupAttentionSummary(primary *tuiStartupCredentialIssue, status auth.CredentialStatus, issueCount int, sourceIDs []string) string {
	sourceID := primary.firstSourceID()
	credentialLabel := status.Title
	if strings.TrimSpace(credentialLabel) == "" {
		credentialLabel = tuiCredentialKindLabel(primary.Kind)
	}

	summary := ""
	switch status.Health {
	case auth.CredentialHealthNeedsRefresh:
		summary = fmt.Sprintf("%s is blocked by a stale %s.", sourceID, strings.ToLower(credentialLabel))
	case auth.CredentialHealthMissing:
		summary = fmt.Sprintf("%s is missing %s.", sourceID, strings.ToLower(credentialLabel))
	default:
		summary = fmt.Sprintf("%s needs %s.", sourceID, strings.ToLower(credentialLabel))
	}

	if issueCount > 1 {
		summary = fmt.Sprintf("%s %d credential blockers affect %d enabled sources.", summary, issueCount, len(sourceIDs))
	}
	return summary
}

func (s *tuiStartupAttentionState) tone() string {
	if s == nil {
		return "success"
	}
	if s.Severity == tuiStartupAttentionSeverityBlocked {
		return "danger"
	}
	return "warning"
}

func (s *tuiStartupAttentionState) badgeLabel() string {
	if s == nil {
		return "READY"
	}
	if s.Severity == tuiStartupAttentionSeverityBlocked {
		return "BLOCKED"
	}
	return "ATTENTION"
}

func (s *tuiStartupAttentionState) footerStateLabel() string {
	if s == nil {
		return "ready"
	}
	if s.Severity == tuiStartupAttentionSeverityBlocked {
		return "blocked"
	}
	return "attention"
}

func (s *tuiStartupAttentionState) banner() *tuiBanner {
	if s == nil {
		return nil
	}
	text := s.Headline + "\n" + s.SummaryText + " " + s.PrimaryActionLabel
	return &tuiBanner{Text: text, Tone: s.tone()}
}

func (s *tuiStartupAttentionState) panelLines() []string {
	if s == nil {
		return nil
	}
	lines := []string{
		"Severity: " + strings.ToUpper(string(s.Severity)),
		"Primary source: " + firstNonEmpty(s.PrimarySourceID, "unknown"),
		"Summary: " + s.SummaryText,
		"Next action: " + s.PrimaryActionLabel,
	}
	if s.IssueCount > 1 {
		lines = append(lines, fmt.Sprintf("Affected sources: %s", strings.Join(s.AffectedSourceIDs, ", ")))
	}
	return lines
}

func (i *tuiStartupCredentialIssue) firstSourceID() string {
	if i == nil || len(i.SourceIDs) == 0 {
		return ""
	}
	return i.SourceIDs[0]
}

func tuiCredentialKindLabel(kind auth.CredentialKind) string {
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

func firstSourceID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

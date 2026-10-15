package cli

import (
	"fmt"
	"strings"

	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
)

var (
	tuiInspectSoundCloudClientIDStatusFn = auth.InspectSoundCloudClientID
	tuiInspectDeemixARLStatusFn          = auth.InspectDeemixARL
	tuiInspectSpotifyCredentialsStatusFn = auth.InspectSpotifyCredentials
	tuiDetectStartupAttentionFn          = tuiDetectStartupAttention
)

type tuiStartupAttentionSeverity = workflows.StartupAttentionSeverity

const (
	tuiStartupAttentionSeverityAttention = workflows.StartupAttentionSeverityAttention
	tuiStartupAttentionSeverityBlocked   = workflows.StartupAttentionSeverityBlocked
)

type tuiStartupAttentionState workflows.StartupAttention

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
	state := workflows.DetectStartupAttention(cfg, workflows.CredentialInspectors{
		SoundCloudClientID: tuiInspectSoundCloudClientIDStatusFn,
		DeemixARL:          tuiInspectDeemixARLStatusFn,
		SpotifyCredentials: tuiInspectSpotifyCredentialsStatusFn,
	})
	if state == nil {
		return nil
	}
	return (*tuiStartupAttentionState)(state)
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

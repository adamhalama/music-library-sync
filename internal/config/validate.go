package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

var sourceIDPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return "invalid config"
	}
	return fmt.Sprintf("invalid config: %s", strings.Join(e.Problems, "; "))
}

func Validate(cfg Config) error {
	problems := []string{}

	if cfg.Version != 1 {
		problems = append(problems, "version must be 1")
	}

	stateDir, err := ExpandPath(cfg.Defaults.StateDir)
	if err != nil || strings.TrimSpace(stateDir) == "" {
		problems = append(problems, "defaults.state_dir must be a valid path")
	} else if !filepath.IsAbs(stateDir) {
		problems = append(problems, "defaults.state_dir must resolve to an absolute path")
	}

	if strings.TrimSpace(cfg.Defaults.ArchiveFile) == "" {
		problems = append(problems, "defaults.archive_file must be set")
	}
	if cfg.Defaults.Threads <= 0 {
		problems = append(problems, "defaults.threads must be > 0")
	}
	if cfg.Defaults.CommandTimeoutSeconds <= 0 {
		problems = append(problems, "defaults.command_timeout_seconds must be > 0")
	}

	if len(cfg.Sources) == 0 {
		problems = append(problems, "at least one source must be configured")
	}

	seenIDs := map[string]struct{}{}
	for _, source := range cfg.Sources {
		if strings.TrimSpace(source.ID) == "" {
			problems = append(problems, "source.id must not be empty")
		} else {
			if !sourceIDPattern.MatchString(source.ID) {
				problems = append(problems, fmt.Sprintf("source %q has invalid id format", source.ID))
			}
			if _, exists := seenIDs[source.ID]; exists {
				problems = append(problems, fmt.Sprintf("duplicate source id %q", source.ID))
			}
			seenIDs[source.ID] = struct{}{}
		}

		switch source.Type {
		case SourceTypeSpotify, SourceTypeSoundCloud:
		default:
			problems = append(problems, fmt.Sprintf("source %q has unsupported type %q", source.ID, source.Type))
		}

		if strings.TrimSpace(source.TargetDir) == "" {
			problems = append(problems, fmt.Sprintf("source %q target_dir must be set", source.ID))
		} else {
			targetDir, targetErr := ExpandPath(source.TargetDir)
			if targetErr != nil {
				problems = append(problems, fmt.Sprintf("source %q target_dir is invalid", source.ID))
			} else if !filepath.IsAbs(targetDir) {
				problems = append(problems, fmt.Sprintf("source %q target_dir must resolve to an absolute path", source.ID))
			}
		}

		if strings.TrimSpace(source.URL) == "" {
			problems = append(problems, fmt.Sprintf("source %q url must be set", source.ID))
		} else if err := validateURL(source.URL); err != nil {
			problems = append(problems, fmt.Sprintf("source %q has invalid url: %v", source.ID, err))
		}

		if strings.TrimSpace(source.Adapter.Kind) == "" {
			problems = append(problems, fmt.Sprintf("source %q adapter.kind must be set", source.ID))
			if source.Type == SourceTypeSpotify {
				problems = append(problems, fmt.Sprintf("source %q spotify type requires explicit adapter.kind (deemix or spotdl)", source.ID))
			}
		} else {
			switch source.Adapter.Kind {
			case "spotdl", "scdl", "deemix":
			default:
				problems = append(problems, fmt.Sprintf("source %q has unsupported adapter.kind %q", source.ID, source.Adapter.Kind))
			}
			if source.Type == SourceTypeSpotify && source.Adapter.Kind != "spotdl" && source.Adapter.Kind != "deemix" {
				problems = append(problems, fmt.Sprintf("source %q spotify type requires spotdl or deemix adapter", source.ID))
			}
			if source.Type == SourceTypeSoundCloud && source.Adapter.Kind != "scdl" {
				problems = append(problems, fmt.Sprintf("source %q soundcloud type requires scdl adapter", source.ID))
			}
		}

		if source.Type == SourceTypeSpotify && strings.TrimSpace(source.StateFile) == "" {
			problems = append(problems, fmt.Sprintf("source %q state_file is required for spotify", source.ID))
		}
		if source.Type == SourceTypeSoundCloud && strings.TrimSpace(source.StateFile) == "" {
			problems = append(problems, fmt.Sprintf("source %q state_file is required for soundcloud", source.ID))
		}
		supportsSyncPolicy := source.Type == SourceTypeSoundCloud ||
			(source.Type == SourceTypeSpotify && source.Adapter.Kind == "deemix")
		if !supportsSyncPolicy {
			if source.Sync.BreakOnExisting != nil {
				problems = append(problems, fmt.Sprintf("source %q sync.break_on_existing is only supported for soundcloud or spotify+deemix", source.ID))
			}
			if source.Sync.AskOnExisting != nil {
				problems = append(problems, fmt.Sprintf("source %q sync.ask_on_existing is only supported for soundcloud or spotify+deemix", source.ID))
			}
		}
	}

	if len(problems) > 0 {
		return &ValidationError{Problems: problems}
	}
	return nil
}

func validateURL(raw string) error {
	parsed, err := url.ParseRequestURI(raw)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if strings.TrimSpace(parsed.Host) == "" {
		return fmt.Errorf("host is required")
	}
	return nil
}

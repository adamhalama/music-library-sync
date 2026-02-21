package scdl

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

type Adapter struct{}

const defaultYTDLPArgs = "--embed-thumbnail --embed-metadata"

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Kind() string {
	return "scdl"
}

func (a *Adapter) Binary() string {
	return "scdl"
}

func (a *Adapter) MinVersion() string {
	return "3.0.0"
}

func (a *Adapter) RequiredEnv(source config.Source) []string {
	return []string{"SCDL_CLIENT_ID"}
}

func (a *Adapter) Validate(source config.Source) error {
	if source.Type != config.SourceTypeSoundCloud {
		return fmt.Errorf("scdl adapter only supports soundcloud sources")
	}
	return nil
}

func (a *Adapter) BuildExecSpec(source config.Source, defaults config.Defaults, timeout time.Duration) (engine.ExecSpec, error) {
	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return engine.ExecSpec{}, err
	}

	args := []string{"-l", source.URL}
	displayArgs := []string{"-l", sanitizeURL(source.URL)}
	if !containsArg(source.Adapter.ExtraArgs, "-f") {
		args = append(args, "-f")
		displayArgs = append(displayArgs, "-f")
	}

	if clientID := strings.TrimSpace(os.Getenv("SCDL_CLIENT_ID")); clientID != "" {
		args = append(args, "--client-id", clientID)
		displayArgs = append(displayArgs, "--client-id", "***")
	}

	args = append(args, source.Adapter.ExtraArgs...)
	displayArgs = append(displayArgs, source.Adapter.ExtraArgs...)
	if !containsYTDLPArgs(source.Adapter.ExtraArgs) {
		args = append(args, "--yt-dlp-args", defaultYTDLPArgs)
		displayArgs = append(displayArgs, "--yt-dlp-args", defaultYTDLPArgs)
	}

	return engine.ExecSpec{
		Bin:            a.Binary(),
		Args:           args,
		Dir:            targetDir,
		Timeout:        timeout,
		DisplayCommand: formatCommand(a.Binary(), displayArgs),
	}, nil
}

func formatCommand(bin string, args []string) string {
	parts := []string{bin}
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}

func sanitizeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func containsArg(args []string, needle string) bool {
	for _, candidate := range args {
		if strings.TrimSpace(candidate) == needle {
			return true
		}
	}
	return false
}

func containsYTDLPArgs(args []string) bool {
	for _, candidate := range args {
		trimmed := strings.TrimSpace(candidate)
		if trimmed == "--yt-dlp-args" || strings.HasPrefix(trimmed, "--yt-dlp-args=") {
			return true
		}
	}
	return false
}

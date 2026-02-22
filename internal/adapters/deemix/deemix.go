package deemix

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

func New() *Adapter {
	return &Adapter{}
}

func (a *Adapter) Kind() string {
	return "deemix"
}

func (a *Adapter) Binary() string {
	if override := strings.TrimSpace(os.Getenv("UDL_DEEMIX_BIN")); override != "" {
		return override
	}
	return "deemix"
}

func (a *Adapter) MinVersion() string {
	return "0.1.0"
}

func (a *Adapter) RequiredEnv(source config.Source) []string {
	return nil
}

func (a *Adapter) Validate(source config.Source) error {
	if source.Type != config.SourceTypeSpotify {
		return fmt.Errorf("deemix adapter only supports spotify sources")
	}
	if strings.TrimSpace(source.StateFile) == "" {
		return fmt.Errorf("state_file is required for spotify source")
	}
	return nil
}

func (a *Adapter) BuildExecSpec(source config.Source, defaults config.Defaults, timeout time.Duration) (engine.ExecSpec, error) {
	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return engine.ExecSpec{}, err
	}

	if _, err := config.ResolveStateFile(defaults.StateDir, source.StateFile); err != nil {
		return engine.ExecSpec{}, err
	}

	runtimeDir := strings.TrimSpace(source.DeemixRuntimeDir)
	if runtimeDir == "" {
		runtimeDir, err = PrepareRuntimeConfig(source)
		if err != nil {
			return engine.ExecSpec{}, err
		}
	} else {
		runtimeDir, err = config.ExpandPath(runtimeDir)
		if err != nil {
			return engine.ExecSpec{}, err
		}
	}

	sourceURL := strings.TrimSpace(source.URL)
	if sourceURL == "" {
		return engine.ExecSpec{}, fmt.Errorf("spotify source url must be set")
	}

	args := []string{sourceURL}
	displayArgs := []string{sanitizeURL(sourceURL)}

	if !containsArg(source.Adapter.ExtraArgs, "--path") {
		args = append(args, "--path", targetDir)
		displayArgs = append(displayArgs, "--path", targetDir)
	}
	if !containsArg(source.Adapter.ExtraArgs, "--portable") {
		args = append(args, "--portable")
		displayArgs = append(displayArgs, "--portable")
	}

	args = append(args, source.Adapter.ExtraArgs...)
	displayArgs = append(displayArgs, source.Adapter.ExtraArgs...)

	bin := a.Binary()
	return engine.ExecSpec{
		Bin:            bin,
		Args:           args,
		Dir:            runtimeDir,
		Timeout:        timeout,
		DisplayCommand: formatCommand(bin, displayArgs),
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
		trimmed := strings.TrimSpace(candidate)
		if trimmed == needle {
			return true
		}
		if strings.HasPrefix(trimmed, needle+"=") {
			return true
		}
	}
	return false
}

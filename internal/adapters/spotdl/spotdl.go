package spotdl

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
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
	return "spotdl"
}

func (a *Adapter) Binary() string {
	return "spotdl"
}

func (a *Adapter) MinVersion() string {
	return "4.0.0"
}

func (a *Adapter) RequiredEnv(source config.Source) []string {
	return nil
}

func (a *Adapter) Validate(source config.Source) error {
	if source.Type != config.SourceTypeSpotify {
		return fmt.Errorf("spotdl adapter only supports spotify sources")
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

	stateFilePath, err := config.ResolveStateFile(defaults.StateDir, source.StateFile)
	if err != nil {
		return engine.ExecSpec{}, err
	}

	args := []string{"sync"}
	displayArgs := []string{"sync"}
	if _, err := os.Stat(stateFilePath); err == nil {
		args = append(args, stateFilePath)
		displayArgs = append(displayArgs, stateFilePath)
	} else {
		args = append(args, source.URL, "--save-file", stateFilePath)
		displayArgs = append(displayArgs, sanitizeURL(source.URL), "--save-file", stateFilePath)
	}

	args = append(args,
		"--threads", strconv.Itoa(defaults.Threads),
		"--archive", defaults.ArchiveFile,
	)
	displayArgs = append(displayArgs,
		"--threads", strconv.Itoa(defaults.Threads),
		"--archive", defaults.ArchiveFile,
	)
	args = append(args, source.Adapter.ExtraArgs...)
	displayArgs = append(displayArgs, source.Adapter.ExtraArgs...)

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

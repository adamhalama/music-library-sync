package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
)

type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

type Check struct {
	Severity Severity `json:"severity"`
	Name     string   `json:"name"`
	Message  string   `json:"message"`
}

type Report struct {
	Checks []Check `json:"checks"`
}

func (r Report) HasErrors() bool {
	for _, check := range r.Checks {
		if check.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r Report) ErrorCount() int {
	count := 0
	for _, check := range r.Checks {
		if check.Severity == SeverityError {
			count++
		}
	}
	return count
}

type Checker struct {
	LookPath      func(string) (string, error)
	ReadVersion   func(context.Context, string) (string, error)
	Getenv        func(string) string
	CheckWritable func(string) error
}

func NewChecker() *Checker {
	return &Checker{
		LookPath:    exec.LookPath,
		ReadVersion: defaultReadVersion,
		Getenv:      os.Getenv,
		CheckWritable: func(path string) error {
			return checkDirWritable(path)
		},
	}
}

func (c *Checker) Check(ctx context.Context, cfg config.Config) Report {
	report := Report{Checks: []Check{}}

	requiredBinaries := requiredBinaries(cfg)
	for _, dep := range requiredBinaries {
		location, err := c.LookPath(dep.Binary)
		if err != nil {
			report.Checks = append(report.Checks, Check{
				Severity: SeverityError,
				Name:     "dependency",
				Message:  fmt.Sprintf("%s not found in PATH", dep.Binary),
			})
			continue
		}

		report.Checks = append(report.Checks, Check{
			Severity: SeverityInfo,
			Name:     "dependency",
			Message:  fmt.Sprintf("%s found at %s", dep.Binary, location),
		})

		output, versionErr := c.ReadVersion(ctx, dep.Binary)
		if versionErr != nil {
			report.Checks = append(report.Checks, Check{
				Severity: SeverityWarn,
				Name:     "dependency",
				Message:  fmt.Sprintf("%s version could not be read: %v", dep.Binary, versionErr),
			})
			continue
		}

		version, parseErr := extractVersion(output)
		if parseErr != nil {
			report.Checks = append(report.Checks, Check{
				Severity: SeverityWarn,
				Name:     "dependency",
				Message:  fmt.Sprintf("%s version output is unrecognized: %q", dep.Binary, strings.TrimSpace(output)),
			})
			continue
		}

		if compareVersions(version, dep.MinVersion) < 0 {
			report.Checks = append(report.Checks, Check{
				Severity: SeverityError,
				Name:     "dependency",
				Message:  fmt.Sprintf("%s version %s is below minimum %s", dep.Binary, version, dep.MinVersion),
			})
			continue
		}

		report.Checks = append(report.Checks, Check{
			Severity: SeverityInfo,
			Name:     "dependency",
			Message:  fmt.Sprintf("%s version %s is compatible", dep.Binary, version),
		})
	}

	for _, source := range cfg.Sources {
		if !source.Enabled {
			continue
		}

		if source.Type == config.SourceTypeSoundCloud {
			if strings.TrimSpace(c.Getenv("SCDL_CLIENT_ID")) == "" {
				report.Checks = append(report.Checks, Check{
					Severity: SeverityError,
					Name:     "auth",
					Message:  "SCDL_CLIENT_ID is required for soundcloud sources",
				})
			} else {
				report.Checks = append(report.Checks, Check{
					Severity: SeverityInfo,
					Name:     "auth",
					Message:  "SCDL_CLIENT_ID is present",
				})
			}
		}

		targetDir, err := config.ExpandPath(source.TargetDir)
		if err != nil {
			report.Checks = append(report.Checks, Check{Severity: SeverityError, Name: "filesystem", Message: fmt.Sprintf("source %s target_dir is invalid: %v", source.ID, err)})
			continue
		}
		if err := c.CheckWritable(targetDir); err != nil {
			report.Checks = append(report.Checks, Check{Severity: SeverityError, Name: "filesystem", Message: fmt.Sprintf("source %s target_dir is not writable: %v", source.ID, err)})
		} else {
			report.Checks = append(report.Checks, Check{Severity: SeverityInfo, Name: "filesystem", Message: fmt.Sprintf("source %s target_dir is writable", source.ID)})
		}

		if source.Type == config.SourceTypeSpotify {
			stateFile, stateErr := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
			if stateErr != nil {
				report.Checks = append(report.Checks, Check{Severity: SeverityError, Name: "filesystem", Message: fmt.Sprintf("source %s state_file is invalid: %v", source.ID, stateErr)})
				continue
			}
			stateDir := filepath.Dir(stateFile)
			if err := c.CheckWritable(stateDir); err != nil {
				report.Checks = append(report.Checks, Check{Severity: SeverityError, Name: "filesystem", Message: fmt.Sprintf("source %s state directory is not writable: %v", source.ID, err)})
			} else {
				report.Checks = append(report.Checks, Check{Severity: SeverityInfo, Name: "filesystem", Message: fmt.Sprintf("source %s state directory is writable", source.ID)})
			}
		}
	}

	if len(cfg.Sources) == 0 {
		report.Checks = append(report.Checks, Check{Severity: SeverityWarn, Name: "config", Message: "no sources configured"})
	}

	return report
}

type dependency struct {
	Binary     string
	MinVersion string
}

func requiredBinaries(cfg config.Config) []dependency {
	seen := map[string]dependency{}
	for _, source := range cfg.Sources {
		if !source.Enabled {
			continue
		}
		switch source.Adapter.Kind {
		case "spotdl":
			seen["spotdl"] = dependency{Binary: "spotdl", MinVersion: minVersionOrDefault(source.Adapter.MinVersion, "4.0.0")}
		case "scdl":
			seen["scdl"] = dependency{Binary: "scdl", MinVersion: minVersionOrDefault(source.Adapter.MinVersion, "3.0.0")}
		}
	}

	if len(seen) == 0 {
		seen["spotdl"] = dependency{Binary: "spotdl", MinVersion: "4.0.0"}
		seen["scdl"] = dependency{Binary: "scdl", MinVersion: "3.0.0"}
	}

	result := make([]dependency, 0, len(seen))
	for _, dep := range seen {
		result = append(result, dep)
	}
	return result
}

func minVersionOrDefault(candidate string, fallback string) string {
	if strings.TrimSpace(candidate) == "" {
		return fallback
	}
	return candidate
}

func defaultReadVersion(ctx context.Context, binary string) (string, error) {
	cmd := exec.CommandContext(ctx, binary, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func checkDirWritable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	file, err := os.CreateTemp(path, ".udl-write-check-*")
	if err != nil {
		return err
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return nil
}

var versionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func extractVersion(raw string) (string, error) {
	matches := versionPattern.FindStringSubmatch(raw)
	if len(matches) != 4 {
		return "", fmt.Errorf("no semantic version found")
	}
	return fmt.Sprintf("%s.%s.%s", matches[1], matches[2], matches[3]), nil
}

func compareVersions(lhs string, rhs string) int {
	leftParts := strings.Split(lhs, ".")
	rightParts := strings.Split(rhs, ".")
	for i := 0; i < 3; i++ {
		leftValue := 0
		rightValue := 0
		if i < len(leftParts) {
			leftValue, _ = strconv.Atoi(leftParts[i])
		}
		if i < len(rightParts) {
			rightValue, _ = strconv.Atoi(rightParts[i])
		}
		if leftValue > rightValue {
			return 1
		}
		if leftValue < rightValue {
			return -1
		}
	}
	return 0
}

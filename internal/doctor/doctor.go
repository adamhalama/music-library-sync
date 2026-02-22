package doctor

import (
	"context"
	"encoding/json"
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
	ReadFile      func(string) ([]byte, error)
	HomeDir       func() (string, error)
	Matrix        map[string]dependencyMatrixRule
}

func NewChecker() *Checker {
	return &Checker{
		LookPath:    exec.LookPath,
		ReadVersion: defaultReadVersion,
		Getenv:      os.Getenv,
		CheckWritable: func(path string) error {
			return checkDirWritable(path)
		},
		ReadFile: os.ReadFile,
		HomeDir:  os.UserHomeDir,
		Matrix:   defaultDependencyMatrix(),
	}
}

func (c *Checker) Check(ctx context.Context, cfg config.Config) Report {
	report := Report{Checks: []Check{}}

	requiredBinaries := requiredBinaries(cfg, c.matrix())
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

		if dep.Matrix != nil {
			if reason, knownBad := dep.Matrix.KnownBad[version]; knownBad {
				message := fmt.Sprintf("%s version %s is blocked by compatibility matrix", dep.Binary, version)
				if strings.TrimSpace(reason) != "" {
					message = fmt.Sprintf("%s: %s", message, reason)
				}
				report.Checks = append(report.Checks, Check{
					Severity: SeverityError,
					Name:     "dependency",
					Message:  message,
				})
				continue
			}
			if strings.TrimSpace(dep.Matrix.MaxVersionExclusive) != "" &&
				compareVersions(version, dep.Matrix.MaxVersionExclusive) >= 0 {
				report.Checks = append(report.Checks, Check{
					Severity: SeverityError,
					Name:     "dependency",
					Message: fmt.Sprintf(
						"%s version %s is outside supported matrix >=%s and <%s",
						dep.Binary,
						version,
						dep.MinVersion,
						dep.Matrix.MaxVersionExclusive,
					),
				})
				continue
			}
			report.Checks = append(report.Checks, Check{
				Severity: SeverityInfo,
				Name:     "dependency",
				Message: fmt.Sprintf(
					"%s version %s is compatible with supported matrix >=%s and <%s",
					dep.Binary,
					version,
					dep.MinVersion,
					dep.Matrix.MaxVersionExclusive,
				),
			})
			continue
		}

		report.Checks = append(report.Checks, Check{
			Severity: SeverityInfo,
			Name:     "dependency",
			Message:  fmt.Sprintf("%s version %s is compatible", dep.Binary, version),
		})
	}

	if hasEnabledSpotifySource(cfg.Sources) {
		if check, ok := c.sharedSpotDLCredentialsCheck(); ok {
			report.Checks = append(report.Checks, check)
		}
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
		if source.Type == config.SourceTypeSoundCloud {
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

type spotDLConfig struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func hasEnabledSpotifySource(sources []config.Source) bool {
	for _, source := range sources {
		if source.Enabled && source.Type == config.SourceTypeSpotify {
			return true
		}
	}
	return false
}

func (c *Checker) sharedSpotDLCredentialsCheck() (Check, bool) {
	readFile := c.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	homeDir := c.HomeDir
	if homeDir == nil {
		homeDir = os.UserHomeDir
	}
	home, err := homeDir()
	if err != nil {
		return Check{}, false
	}
	configPath := filepath.Join(home, ".spotdl", "config.json")
	raw, err := readFile(configPath)
	if err != nil {
		return Check{}, false
	}

	var cfg spotDLConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Check{}, false
	}

	if !usesSharedSpotDLCredentials(cfg.ClientID, cfg.ClientSecret) {
		return Check{}, false
	}

	return Check{
		Severity: SeverityWarn,
		Name:     "auth",
		Message:  fmt.Sprintf("spotdl config at %s is using shared default Spotify credentials; set your own app client_id/client_secret to avoid API throttling", configPath),
	}, true
}

func usesSharedSpotDLCredentials(clientID string, clientSecret string) bool {
	clientID = strings.TrimSpace(clientID)
	clientSecret = strings.TrimSpace(clientSecret)
	if clientID == "" || clientSecret == "" {
		return false
	}

	knownPairs := [][2]string{
		{"5f573c9620494bae87890c0f08a60293", "212476d9b0f3472eaa762d90b19b0ba8"},
		{"f8a606e5583643beaa27ce62c48e3fc1", "f6f4c8f73f0649939286cf417c811607"},
	}
	for _, pair := range knownPairs {
		if clientID == pair[0] && clientSecret == pair[1] {
			return true
		}
	}
	return false
}

type dependency struct {
	Key        string
	Binary     string
	MinVersion string
	Matrix     *dependencyMatrixRule
}

type dependencyMatrixRule struct {
	MinVersion          string
	MaxVersionExclusive string
	KnownBad            map[string]string
}

func defaultDependencyMatrix() map[string]dependencyMatrixRule {
	return map[string]dependencyMatrixRule{
		"scdl": {
			MinVersion:          "3.0.0",
			MaxVersionExclusive: "4.0.0",
			KnownBad:            map[string]string{},
		},
		"yt-dlp": {
			MinVersion:          "2024.1.0",
			MaxVersionExclusive: "2027.0.0",
			KnownBad:            map[string]string{},
		},
	}
}

func cloneDependencyMatrix(input map[string]dependencyMatrixRule) map[string]dependencyMatrixRule {
	cloned := make(map[string]dependencyMatrixRule, len(input))
	for key, rule := range input {
		bad := make(map[string]string, len(rule.KnownBad))
		for version, reason := range rule.KnownBad {
			bad[version] = reason
		}
		cloned[key] = dependencyMatrixRule{
			MinVersion:          rule.MinVersion,
			MaxVersionExclusive: rule.MaxVersionExclusive,
			KnownBad:            bad,
		}
	}
	return cloned
}

func (c *Checker) matrix() map[string]dependencyMatrixRule {
	if len(c.Matrix) == 0 {
		return defaultDependencyMatrix()
	}
	return cloneDependencyMatrix(c.Matrix)
}

func requiredBinaries(cfg config.Config, matrix map[string]dependencyMatrixRule) []dependency {
	seen := map[string]dependency{}

	scdlRule, hasSCDLRule := matrix["scdl"]
	ytdlpRule, hasYTDLPRule := matrix["yt-dlp"]
	for _, source := range cfg.Sources {
		if !source.Enabled {
			continue
		}
		switch source.Adapter.Kind {
		case "spotdl":
			seen["spotdl"] = dependency{
				Key:        "spotdl",
				Binary:     resolveSpotDLBinaryForDoctor(),
				MinVersion: minVersionOrDefault(source.Adapter.MinVersion, "4.0.0"),
			}
		case "scdl":
			scdlMin := "3.0.0"
			if hasSCDLRule && strings.TrimSpace(scdlRule.MinVersion) != "" {
				scdlMin = scdlRule.MinVersion
			}
			scdlMin = maxVersion(scdlMin, minVersionOrDefault(source.Adapter.MinVersion, scdlMin))
			seen["scdl"] = dependency{
				Key:        "scdl",
				Binary:     "scdl",
				MinVersion: scdlMin,
				Matrix:     matrixRulePointer(matrix, "scdl"),
			}

			ytdlpMin := "0.0.0"
			if hasYTDLPRule && strings.TrimSpace(ytdlpRule.MinVersion) != "" {
				ytdlpMin = ytdlpRule.MinVersion
			}
			seen["yt-dlp"] = dependency{
				Key:        "yt-dlp",
				Binary:     "yt-dlp",
				MinVersion: ytdlpMin,
				Matrix:     matrixRulePointer(matrix, "yt-dlp"),
			}
		}
	}

	if len(seen) == 0 {
		scdlMin := "3.0.0"
		if rule := matrixRulePointer(matrix, "scdl"); rule != nil && strings.TrimSpace(rule.MinVersion) != "" {
			scdlMin = rule.MinVersion
		}
		ytdlpMin := "0.0.0"
		if rule := matrixRulePointer(matrix, "yt-dlp"); rule != nil && strings.TrimSpace(rule.MinVersion) != "" {
			ytdlpMin = rule.MinVersion
		}
		seen["spotdl"] = dependency{Key: "spotdl", Binary: resolveSpotDLBinaryForDoctor(), MinVersion: "4.0.0"}
		seen["scdl"] = dependency{
			Key:        "scdl",
			Binary:     "scdl",
			MinVersion: scdlMin,
			Matrix:     matrixRulePointer(matrix, "scdl"),
		}
		seen["yt-dlp"] = dependency{
			Key:        "yt-dlp",
			Binary:     "yt-dlp",
			MinVersion: ytdlpMin,
			Matrix:     matrixRulePointer(matrix, "yt-dlp"),
		}
	}

	result := make([]dependency, 0, len(seen))
	for _, dep := range seen {
		result = append(result, dep)
	}
	return result
}

func matrixRulePointer(matrix map[string]dependencyMatrixRule, key string) *dependencyMatrixRule {
	rule, ok := matrix[key]
	if !ok {
		return nil
	}
	cloned := dependencyMatrixRule{
		MinVersion:          rule.MinVersion,
		MaxVersionExclusive: rule.MaxVersionExclusive,
		KnownBad:            map[string]string{},
	}
	for version, reason := range rule.KnownBad {
		cloned.KnownBad[version] = reason
	}
	return &cloned
}

func maxVersion(lhs string, rhs string) string {
	if compareVersions(lhs, rhs) >= 0 {
		return lhs
	}
	return rhs
}

func resolveSpotDLBinaryForDoctor() string {
	if override := strings.TrimSpace(os.Getenv("UDL_SPOTDL_BIN")); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "spotdl"
	}
	candidate := filepath.Join(home, ".venvs", "udl-spotdl", "bin", "spotdl")
	info, err := os.Stat(candidate)
	if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
		return candidate
	}
	return "spotdl"
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

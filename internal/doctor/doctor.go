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

	"github.com/jaa/update-downloads/internal/auth"
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
	LookPath                  func(string) (string, error)
	ReadVersion               func(context.Context, string) (string, error)
	Getenv                    func(string) string
	CheckWritable             func(string) error
	CheckDirAccess            func(string) dirAccessResult
	ReadFile                  func(string) ([]byte, error)
	HomeDir                   func() (string, error)
	ResolveSpotifyCredentials func() (auth.SpotifyCredentials, error)
	ResolveSpotifyWithSource  func() (auth.SpotifyCredentials, auth.CredentialStorageSource, error)
	ResolveDeemixARL          func() (string, error)
	ResolveDeemixWithSource   func() (string, auth.CredentialStorageSource, error)
	ResolveSoundCloudClientID func() (string, auth.CredentialStorageSource, error)
	LoadCredentialMetadata    func(string) (auth.CredentialMetadataStore, error)
	Matrix                    map[string]dependencyMatrixRule
}

type dirAccessResult struct {
	Creatable bool
	Err       error
}

func NewChecker() *Checker {
	return &Checker{
		LookPath:    exec.LookPath,
		ReadVersion: defaultReadVersion,
		Getenv:      os.Getenv,
		CheckWritable: func(path string) error {
			return checkDirWritable(path)
		},
		CheckDirAccess:            inspectDirAccess,
		ReadFile:                  os.ReadFile,
		HomeDir:                   os.UserHomeDir,
		ResolveSpotifyCredentials: auth.ResolveSpotifyCredentials,
		ResolveSpotifyWithSource:  auth.ResolveSpotifyCredentialsWithSource,
		ResolveDeemixARL:          auth.ResolveDeemixARL,
		ResolveDeemixWithSource:   auth.ResolveDeemixARLWithSource,
		ResolveSoundCloudClientID: auth.ResolveSoundCloudClientIDWithSource,
		LoadCredentialMetadata:    auth.LoadCredentialMetadata,
		Matrix:                    defaultDependencyMatrix(),
	}
}

func (c *Checker) Check(ctx context.Context, cfg config.Config) Report {
	report := Report{Checks: []Check{}}

	if len(cfg.Sources) == 0 {
		report.Checks = append(report.Checks,
			Check{
				Severity: SeverityWarn,
				Name:     "config",
				Message:  "no sources configured yet; open `udl tui` and choose Get Started to set up your first library",
			},
			Check{
				Severity: SeverityInfo,
				Name:     "config",
				Message:  "doctor will start dependency and auth checks after at least one source is configured",
			},
		)
		return report
	}

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
	if hasEnabledSpotifyDeemixSource(cfg.Sources) {
		report.Checks = append(report.Checks, Check{
			Severity: SeverityWarn,
			Name:     "security",
			Message:  "deemix/deezer-sdk upstream transport may use insecure request paths; treat Deezer ARL and Spotify credentials as sensitive and run only on trusted networks",
		})
	}

	for _, source := range cfg.Sources {
		if !source.Enabled {
			continue
		}

		if source.Type == config.SourceTypeSoundCloud && source.Adapter.Kind == "scdl" {
			check := c.soundCloudClientIDCheck(cfg.Defaults.StateDir)
			if check.Name != "" {
				report.Checks = append(report.Checks, check)
			}
		}

		targetDir, err := config.ExpandPath(source.TargetDir)
		if err != nil {
			report.Checks = append(report.Checks, Check{Severity: SeverityError, Name: "filesystem", Message: fmt.Sprintf("source %s target_dir is invalid: %v", source.ID, err)})
			continue
		}
		targetAccess := c.inspectDir(targetDir)
		if targetAccess.Err != nil {
			report.Checks = append(report.Checks, Check{Severity: SeverityError, Name: "filesystem", Message: fmt.Sprintf("source %s target_dir is not writable: %v", source.ID, targetAccess.Err)})
		} else if targetAccess.Creatable {
			report.Checks = append(report.Checks, Check{Severity: SeverityInfo, Name: "filesystem", Message: fmt.Sprintf("source %s target directory will be created on first sync", source.ID)})
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
			stateAccess := c.inspectDir(stateDir)
			if stateAccess.Err != nil {
				report.Checks = append(report.Checks, Check{Severity: SeverityError, Name: "filesystem", Message: fmt.Sprintf("source %s state directory is not writable: %v", source.ID, stateAccess.Err)})
			} else if stateAccess.Creatable {
				report.Checks = append(report.Checks, Check{Severity: SeverityInfo, Name: "filesystem", Message: fmt.Sprintf("source %s state directory will be created on first sync", source.ID)})
			} else {
				report.Checks = append(report.Checks, Check{Severity: SeverityInfo, Name: "filesystem", Message: fmt.Sprintf("source %s state directory is writable", source.ID)})
			}

			if source.Adapter.Kind == "deemix" {
				report.Checks = append(report.Checks, c.deemixARLCheck()...)
				report.Checks = append(report.Checks, c.spotifyCredentialsCheck()...)
			}
		}
		if source.Type == config.SourceTypeSoundCloud {
			stateFile, stateErr := config.ResolveStateFile(cfg.Defaults.StateDir, source.StateFile)
			if stateErr != nil {
				report.Checks = append(report.Checks, Check{Severity: SeverityError, Name: "filesystem", Message: fmt.Sprintf("source %s state_file is invalid: %v", source.ID, stateErr)})
				continue
			}
			stateDir := filepath.Dir(stateFile)
			stateAccess := c.inspectDir(stateDir)
			if stateAccess.Err != nil {
				report.Checks = append(report.Checks, Check{Severity: SeverityError, Name: "filesystem", Message: fmt.Sprintf("source %s state directory is not writable: %v", source.ID, stateAccess.Err)})
			} else if stateAccess.Creatable {
				report.Checks = append(report.Checks, Check{Severity: SeverityInfo, Name: "filesystem", Message: fmt.Sprintf("source %s state directory will be created on first sync", source.ID)})
			} else {
				report.Checks = append(report.Checks, Check{Severity: SeverityInfo, Name: "filesystem", Message: fmt.Sprintf("source %s state directory is writable", source.ID)})
			}
		}
	}
	return report
}

func (c *Checker) soundCloudClientIDCheck(stateDir string) Check {
	resolve := c.ResolveSoundCloudClientID
	if resolve == nil {
		resolve = auth.ResolveSoundCloudClientIDWithSource
	}
	value, source, err := resolve()
	if err == nil && strings.TrimSpace(value) != "" {
		if stale, message := c.soundCloudCredentialFailure(stateDir, source); stale {
			return Check{
				Severity: SeverityError,
				Name:     "auth",
				Message:  message,
			}
		}
		switch source {
		case auth.CredentialStorageSourceEnv:
			return Check{
				Severity: SeverityInfo,
				Name:     "auth",
				Message:  "SoundCloud client ID is available via environment override",
			}
		case auth.CredentialStorageSourceKeychain:
			return Check{
				Severity: SeverityInfo,
				Name:     "auth",
				Message:  "SoundCloud client ID is available in macOS Keychain",
			}
		default:
			return Check{
				Severity: SeverityInfo,
				Name:     "auth",
				Message:  "SoundCloud client ID is available",
			}
		}
	}
	if stale, message := c.soundCloudCredentialFailure(stateDir, source); stale {
		return Check{
			Severity: SeverityError,
			Name:     "auth",
			Message:  message,
		}
	}
	return Check{
		Severity: SeverityError,
		Name:     "auth",
		Message:  "SoundCloud client ID is missing; open `udl tui`, choose Credentials, and save it to Keychain or set SCDL_CLIENT_ID",
	}
}

func (c *Checker) soundCloudCredentialFailure(stateDir string, source auth.CredentialStorageSource) (bool, string) {
	load := c.LoadCredentialMetadata
	if load == nil {
		load = auth.LoadCredentialMetadata
	}
	store, err := load(stateDir)
	if err != nil {
		return false, ""
	}
	meta, ok := store.Credentials[auth.CredentialKindSoundCloudClientID]
	if !ok || strings.TrimSpace(meta.LastFailureKind) == "" {
		return false, ""
	}
	if meta.StorageSource != auth.CredentialStorageSourceNone && source != auth.CredentialStorageSourceNone && meta.StorageSource != source {
		return false, ""
	}
	message := strings.TrimSpace(meta.LastFailureMessage)
	if message == "" {
		message = "SoundCloud client ID needs refresh; open `udl tui`, choose Credentials, and update it"
	}
	return true, message
}

func (c *Checker) deemixARLCheck() []Check {
	resolveWithSource := c.ResolveDeemixWithSource
	if resolveWithSource == nil && c.ResolveDeemixARL != nil {
		resolve := c.ResolveDeemixARL
		resolveWithSource = func() (string, auth.CredentialStorageSource, error) {
			value, err := resolve()
			return value, auth.CredentialStorageSourceKeychain, err
		}
	}
	if resolveWithSource == nil {
		resolveWithSource = auth.ResolveDeemixARLWithSource
	}
	value, source, err := resolveWithSource()
	if err != nil || strings.TrimSpace(value) == "" {
		return []Check{{
			Severity: SeverityError,
			Name:     "auth",
			Message:  "deemix Spotify sources require Deezer ARL; open `udl tui`, choose Credentials, and save it to Keychain or set UDL_DEEMIX_ARL",
		}}
	}
	switch source {
	case auth.CredentialStorageSourceEnv:
		return []Check{{Severity: SeverityInfo, Name: "auth", Message: "Deezer ARL is available via environment override"}}
	case auth.CredentialStorageSourceKeychain:
		return []Check{{Severity: SeverityInfo, Name: "auth", Message: "Deezer ARL is available in macOS Keychain"}}
	default:
		return []Check{{Severity: SeverityInfo, Name: "auth", Message: "Deezer ARL is available"}}
	}
}

func (c *Checker) spotifyCredentialsCheck() []Check {
	resolveWithSource := c.ResolveSpotifyWithSource
	if resolveWithSource == nil && c.ResolveSpotifyCredentials != nil {
		resolve := c.ResolveSpotifyCredentials
		resolveWithSource = func() (auth.SpotifyCredentials, auth.CredentialStorageSource, error) {
			creds, err := resolve()
			return creds, auth.CredentialStorageSourceKeychain, err
		}
	}
	if resolveWithSource == nil {
		resolveWithSource = auth.ResolveSpotifyCredentialsWithSource
	}
	_, source, err := resolveWithSource()
	if err != nil {
		return []Check{{
			Severity: SeverityError,
			Name:     "auth",
			Message:  "deemix Spotify sources require Spotify app credentials; open `udl tui`, choose Credentials, and save them to Keychain or set UDL_SPOTIFY_CLIENT_ID and UDL_SPOTIFY_CLIENT_SECRET",
		}}
	}
	switch source {
	case auth.CredentialStorageSourceEnv:
		return []Check{{Severity: SeverityInfo, Name: "auth", Message: "Spotify app credentials are available via environment override"}}
	case auth.CredentialStorageSourceKeychain:
		return []Check{{Severity: SeverityInfo, Name: "auth", Message: "Spotify app credentials are available in macOS Keychain"}}
	case auth.CredentialStorageSourceSpotDL:
		return []Check{{Severity: SeverityInfo, Name: "auth", Message: "Spotify app credentials are available via ~/.spotdl/config.json compatibility fallback"}}
	default:
		return []Check{{Severity: SeverityInfo, Name: "auth", Message: "Spotify app credentials are available"}}
	}
}

func (c *Checker) inspectDir(path string) dirAccessResult {
	if c.CheckDirAccess != nil {
		return c.CheckDirAccess(path)
	}
	if c.CheckWritable != nil {
		return dirAccessResult{Err: c.CheckWritable(path)}
	}
	return inspectDirAccess(path)
}

func inspectDirAccess(path string) dirAccessResult {
	info, err := os.Stat(path)
	if err == nil {
		if !info.IsDir() {
			return dirAccessResult{Err: fmt.Errorf("%s is not a directory", path)}
		}
		file, createErr := os.CreateTemp(path, ".udl-write-check-*")
		if createErr != nil {
			return dirAccessResult{Err: createErr}
		}
		name := file.Name()
		_ = file.Close()
		_ = os.Remove(name)
		return dirAccessResult{}
	}
	if !os.IsNotExist(err) {
		return dirAccessResult{Err: err}
	}
	parent := filepath.Dir(path)
	if strings.TrimSpace(parent) == "" || parent == "." {
		parent = path
	}
	parentInfo, parentErr := os.Stat(parent)
	if parentErr != nil {
		return dirAccessResult{Err: parentErr}
	}
	if !parentInfo.IsDir() {
		return dirAccessResult{Err: fmt.Errorf("%s is not a directory", parent)}
	}
	file, createErr := os.CreateTemp(parent, ".udl-write-check-*")
	if createErr != nil {
		return dirAccessResult{Err: createErr}
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(name)
	return dirAccessResult{Creatable: true}
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

func hasEnabledSpotifyDeemixSource(sources []config.Source) bool {
	for _, source := range sources {
		if source.Enabled && source.Type == config.SourceTypeSpotify && source.Adapter.Kind == "deemix" {
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
		case "deemix":
			seen["deemix"] = dependency{
				Key:        "deemix",
				Binary:     resolveDeemixBinaryForDoctor(),
				MinVersion: minVersionOrDefault(source.Adapter.MinVersion, "0.1.0"),
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
		case "scdl-freedl":
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

func resolveDeemixBinaryForDoctor() string {
	if override := strings.TrimSpace(os.Getenv("UDL_DEEMIX_BIN")); override != "" {
		return override
	}
	return "deemix"
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

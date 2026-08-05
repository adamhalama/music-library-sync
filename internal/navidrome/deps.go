package navidrome

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// CommandRunner executes an external command and returns its combined output.
// Every dependency and service operation goes through one so tests never touch
// the real system.
type CommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// DefaultCommandRunner runs the command for real.
func DefaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// DependencyStatus is a read-only report. Producing it never mutates anything.
type DependencyStatus struct {
	HomebrewInstalled bool     `json:"homebrew_installed"`
	HomebrewPath      string   `json:"homebrew_path,omitempty"`
	Installed         bool     `json:"installed"`
	BinaryPath        string   `json:"binary_path,omitempty"`
	Version           string   `json:"version,omitempty"`
	MinimumVersion    string   `json:"minimum_version"`
	VersionSupported  bool     `json:"version_supported"`
	FFmpegInstalled   bool     `json:"ffmpeg_installed"`
	FFmpegPath        string   `json:"ffmpeg_path,omitempty"`
	Problems          []string `json:"problems"`
}

// Ready reports whether Navidrome can be managed as-is.
func (s DependencyStatus) Ready() bool {
	return s.Installed && s.VersionSupported
}

// DependencyChecker inspects the system without changing it.
type DependencyChecker struct {
	Run      CommandRunner
	LookPath func(string) (string, error)
	// SearchPaths are checked when the binary is not on PATH, so a Homebrew
	// install still resolves inside a GUI app's minimal environment.
	SearchPaths []string
	// FFmpegSearchPaths does the same for ffmpeg.
	FFmpegSearchPaths []string
}

// DefaultSearchPaths covers both Homebrew prefixes.
func DefaultSearchPaths() []string {
	return []string{"/opt/homebrew/bin/navidrome", "/usr/local/bin/navidrome"}
}

// DefaultFFmpegSearchPaths covers both Homebrew prefixes.
func DefaultFFmpegSearchPaths() []string {
	return []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg"}
}

func (c DependencyChecker) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.Run != nil {
		return c.Run(ctx, name, args...)
	}
	return DefaultCommandRunner(ctx, name, args...)
}

func (c DependencyChecker) lookPath(name string) (string, error) {
	if c.LookPath != nil {
		return c.LookPath(name)
	}
	return exec.LookPath(name)
}

// Status reports Homebrew and Navidrome availability. It never installs.
func (c DependencyChecker) Status(ctx context.Context) DependencyStatus {
	status := DependencyStatus{MinimumVersion: MinimumServerVersion, Problems: []string{}}

	if path, err := c.lookPath("brew"); err == nil && strings.TrimSpace(path) != "" {
		status.HomebrewInstalled = true
		status.HomebrewPath = path
	} else {
		for _, candidate := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				status.HomebrewInstalled = true
				status.HomebrewPath = candidate
				break
			}
		}
	}
	if !status.HomebrewInstalled {
		status.Problems = append(status.Problems, "Homebrew is not installed; install it from https://brew.sh before installing Navidrome")
	}

	// Navidrome transcodes through ffmpeg, and it only ever finds it on PATH or
	// at the configured FFmpegPath. Without it every format the client will not
	// take raw fails with "Internal Server Error: invalid argument", which reads
	// as a broken library rather than a missing dependency — so it is a stated
	// problem, not a silent one.
	status.FFmpegPath = c.findFFmpeg()
	status.FFmpegInstalled = status.FFmpegPath != ""
	if !status.FFmpegInstalled {
		status.Problems = append(status.Problems,
			"ffmpeg is not installed; transcoded formats (m4a, wav, flac) will fail to stream or download. Run `brew install ffmpeg`")
	}

	status.BinaryPath = c.findBinary()
	if status.BinaryPath == "" {
		status.Problems = append(status.Problems, "Navidrome is not installed; run the Install action or `brew install navidrome`")
		return status
	}
	status.Installed = true

	raw, err := c.run(ctx, status.BinaryPath, "--version")
	if err != nil && len(raw) == 0 {
		status.Problems = append(status.Problems, fmt.Sprintf("could not read Navidrome version from %s: %v", status.BinaryPath, err))
		return status
	}
	version, ok := ParseVersion(string(raw))
	if !ok {
		status.Problems = append(status.Problems, fmt.Sprintf("could not parse Navidrome version output %q", strings.TrimSpace(string(raw))))
		return status
	}
	status.Version = version
	if CompareVersions(version, MinimumServerVersion) < 0 {
		status.Problems = append(status.Problems, fmt.Sprintf("Navidrome %s is older than the required %s; run `brew upgrade navidrome`", version, MinimumServerVersion))
		return status
	}
	status.VersionSupported = true
	return status
}

func (c DependencyChecker) findBinary() string {
	if path, err := c.lookPath("navidrome"); err == nil && strings.TrimSpace(path) != "" {
		return path
	}
	candidates := c.SearchPaths
	if candidates == nil {
		candidates = DefaultSearchPaths()
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func (c DependencyChecker) findFFmpeg() string {
	if path, err := c.lookPath("ffmpeg"); err == nil && strings.TrimSpace(path) != "" {
		return path
	}
	candidates := c.FFmpegSearchPaths
	if candidates == nil {
		candidates = DefaultFFmpegSearchPaths()
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// EnsureResult records what an explicitly confirmed install attempt did.
type EnsureResult struct {
	Ran     bool             `json:"ran"`
	Command string           `json:"command,omitempty"`
	Output  string           `json:"output,omitempty"`
	Status  DependencyStatus `json:"status"`
	Message string           `json:"message"`
}

// Ensure installs Navidrome through Homebrew. It refuses without an explicit
// confirmation because it mutates the machine.
func (c DependencyChecker) Ensure(ctx context.Context, confirmed bool) (EnsureResult, error) {
	status := c.Status(ctx)
	if !confirmed {
		return EnsureResult{Status: status, Message: "install not confirmed; no changes were made"},
			fmt.Errorf("navidrome install requires explicit confirmation")
	}
	if status.Ready() && status.FFmpegInstalled {
		return EnsureResult{Status: status, Message: fmt.Sprintf("Navidrome %s is already installed", status.Version)}, nil
	}
	if !status.HomebrewInstalled {
		return EnsureResult{Status: status, Message: "Homebrew is required to install Navidrome"},
			fmt.Errorf("homebrew is not installed")
	}
	brew := status.HomebrewPath

	// ffmpeg is a hard requirement for transcoding, and `brew install navidrome`
	// does not pull it in.
	if !status.FFmpegInstalled {
		raw, err := c.run(ctx, brew, "install", "ffmpeg")
		if err != nil {
			return EnsureResult{
					Ran: true, Command: brew + " install ffmpeg",
					Output: strings.TrimSpace(string(raw)), Status: c.Status(ctx),
					Message: "Homebrew could not install ffmpeg",
				},
				fmt.Errorf("%s install ffmpeg: %w", brew, err)
		}
		status = c.Status(ctx)
		if status.Ready() {
			return EnsureResult{
				Ran: true, Command: brew + " install ffmpeg",
				Output: strings.TrimSpace(string(raw)), Status: status,
				Message: fmt.Sprintf("ffmpeg is installed; Navidrome %s was already present", status.Version),
			}, nil
		}
	}

	args := []string{"install", "navidrome"}
	if status.Installed {
		args = []string{"upgrade", "navidrome"}
	}
	raw, err := c.run(ctx, brew, args...)
	result := EnsureResult{
		Ran:     true,
		Command: strings.TrimSpace(brew + " " + strings.Join(args, " ")),
		Output:  strings.TrimSpace(string(raw)),
	}
	if err != nil {
		result.Status = c.Status(ctx)
		result.Message = "Homebrew command failed"
		return result, fmt.Errorf("%s: %w", result.Command, err)
	}
	result.Status = c.Status(ctx)
	if !result.Status.Ready() {
		result.Message = "Homebrew reported success but Navidrome is still unavailable"
		return result, fmt.Errorf("navidrome is still unavailable after %s", result.Command)
	}
	result.Message = fmt.Sprintf("Navidrome %s is installed", result.Status.Version)
	return result, nil
}

var versionPattern = regexp.MustCompile(`\b(\d+)\.(\d+)\.(\d+)\b`)

// ParseVersion extracts the first dotted triple from `navidrome --version`
// output, which also carries a name and a commit hash.
func ParseVersion(raw string) (string, bool) {
	match := versionPattern.FindString(strings.TrimSpace(raw))
	if match == "" {
		return "", false
	}
	return match, true
}

// CompareVersions returns -1, 0, or 1 comparing dotted numeric versions.
func CompareVersions(left, right string) int {
	leftParts := versionParts(left)
	rightParts := versionParts(right)
	for i := 0; i < 3; i++ {
		if leftParts[i] != rightParts[i] {
			if leftParts[i] < rightParts[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(value string) [3]int {
	out := [3]int{}
	fields := strings.SplitN(strings.TrimSpace(value), ".", 4)
	for i := 0; i < 3 && i < len(fields); i++ {
		digits := strings.TrimLeftFunc(fields[i], func(r rune) bool { return r < '0' || r > '9' })
		numeric := ""
		for _, r := range digits {
			if r < '0' || r > '9' {
				break
			}
			numeric += string(r)
		}
		parsed, err := strconv.Atoi(numeric)
		if err != nil {
			continue
		}
		out[i] = parsed
	}
	return out
}

// BinaryPath resolves the navidrome executable, preferring PATH.
func (c DependencyChecker) BinaryPath() string {
	return c.findBinary()
}

// LogTail returns the last n lines of the managed log file. A missing log is
// not an error: the service may simply never have started.
func LogTail(path string, lines int) ([]string, error) {
	if lines <= 0 {
		lines = 50
	}
	expanded := filepath.Clean(path)
	payload, err := os.ReadFile(expanded)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read navidrome log %s: %w", expanded, err)
	}
	all := strings.Split(strings.TrimRight(string(payload), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return all, nil
}

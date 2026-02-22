package scdl

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
)

type Adapter struct{}

var defaultYTDLPArgTokens = []string{"--embed-thumbnail", "--embed-metadata"}

type runtimeInfo struct {
	Bin               string
	SupportsYTDLPArgs bool
}

var (
	detectRuntimeOnce sync.Once
	detectedRuntime   runtimeInfo
)

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
	runtimeInfo := resolveRuntimeInfo()
	targetDir, err := config.ExpandPath(source.TargetDir)
	if err != nil {
		return engine.ExecSpec{}, err
	}

	if strings.TrimSpace(source.StateFile) == "" {
		return engine.ExecSpec{}, fmt.Errorf("state_file is required for soundcloud source")
	}
	syncFilePath, err := config.ResolveStateFile(defaults.StateDir, source.StateFile)
	if err != nil {
		return engine.ExecSpec{}, err
	}

	args := []string{"-l", source.URL}
	displayArgs := []string{"-l", sanitizeURL(source.URL)}
	if !containsArg(source.Adapter.ExtraArgs, "-f") {
		args = append(args, "-f")
		displayArgs = append(displayArgs, "-f")
	}
	args = append(args, "--sync", syncFilePath)
	displayArgs = append(displayArgs, "--sync", syncFilePath)

	if clientID := strings.TrimSpace(os.Getenv("SCDL_CLIENT_ID")); clientID != "" {
		args = append(args, "--client-id", clientID)
		displayArgs = append(displayArgs, "--client-id", "***")
	}

	extraArgs := stripManagedArgs(source.Adapter.ExtraArgs)
	args = append(args, extraArgs...)
	displayArgs = append(displayArgs, extraArgs...)

	breakOnExisting := true
	if source.Sync.BreakOnExisting != nil {
		breakOnExisting = *source.Sync.BreakOnExisting
	}

	archivePath := strings.TrimSpace(source.DownloadArchivePath)
	if archivePath == "" {
		archivePath, err = config.ResolveArchiveFile(defaults.StateDir, defaults.ArchiveFile, source.ID)
		if err != nil {
			return engine.ExecSpec{}, err
		}
	}

	ytdlpArgs, foundCustomYTDLP := extractYTDLPArgs(source.Adapter.ExtraArgs)
	if !foundCustomYTDLP {
		tokens := append([]string{}, defaultYTDLPArgTokens...)
		tokens = append(tokens, "--download-archive", archivePath)
		ytdlpArgs = strings.Join(tokens, " ")
	} else if !hasDownloadArchive(ytdlpArgs) {
		ytdlpArgs = strings.TrimSpace(ytdlpArgs + " --download-archive " + archivePath)
	}
	ytdlpArgs = normalizeYTDLPBreakArgs(ytdlpArgs, breakOnExisting)
	if !runtimeInfo.SupportsYTDLPArgs {
		return engine.ExecSpec{}, fmt.Errorf(
			"scdl binary %q does not support --yt-dlp-args (requires scdl >= 3.0.0); set PATH or UDL_SCDL_BIN to a compatible binary",
			runtimeInfo.Bin,
		)
	}
	args = append(args, "--yt-dlp-args", ytdlpArgs)
	displayArgs = append(displayArgs, "--yt-dlp-args", ytdlpArgs)

	return engine.ExecSpec{
		Bin:            runtimeInfo.Bin,
		Args:           args,
		Dir:            targetDir,
		Timeout:        timeout,
		DisplayCommand: formatCommand(runtimeInfo.Bin, displayArgs),
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

func extractYTDLPArgs(args []string) (string, bool) {
	for i := 0; i < len(args); i++ {
		trimmed := strings.TrimSpace(args[i])
		if trimmed == "--yt-dlp-args" {
			if i+1 >= len(args) {
				return "", false
			}
			return strings.TrimSpace(args[i+1]), true
		}
		if strings.HasPrefix(trimmed, "--yt-dlp-args=") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "--yt-dlp-args=")), true
		}
	}
	return "", false
}

func stripManagedArgs(args []string) []string {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		trimmed := strings.TrimSpace(args[i])
		if trimmed == "--yt-dlp-args" || trimmed == "--sync" {
			i++
			continue
		}
		if strings.HasPrefix(trimmed, "--yt-dlp-args=") || strings.HasPrefix(trimmed, "--sync=") {
			continue
		}
		filtered = append(filtered, args[i])
	}
	return filtered
}

func normalizeYTDLPBreakArgs(raw string, breakOnExisting bool) string {
	parts := strings.Fields(strings.TrimSpace(raw))
	filtered := make([]string, 0, len(parts)+1)
	for _, token := range parts {
		if token == "--break-on-existing" || token == "--no-break-on-existing" {
			continue
		}
		filtered = append(filtered, token)
	}
	if breakOnExisting {
		filtered = append(filtered, "--break-on-existing")
	} else {
		filtered = append(filtered, "--no-break-on-existing")
	}
	return strings.Join(filtered, " ")
}

func hasDownloadArchive(raw string) bool {
	tokens := strings.Fields(strings.TrimSpace(raw))
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token == "--download-archive" {
			return i+1 < len(tokens)
		}
		if strings.HasPrefix(token, "--download-archive=") {
			return strings.TrimSpace(strings.TrimPrefix(token, "--download-archive=")) != ""
		}
	}
	return false
}

func resolveRuntimeInfo() runtimeInfo {
	detectRuntimeOnce.Do(func() {
		detectedRuntime = detectRuntimeInfo()
	})
	if strings.TrimSpace(detectedRuntime.Bin) == "" {
		return runtimeInfo{
			Bin:               "scdl",
			SupportsYTDLPArgs: true,
		}
	}
	return detectedRuntime
}

func detectRuntimeInfo() runtimeInfo {
	if override := strings.TrimSpace(os.Getenv("UDL_SCDL_BIN")); override != "" {
		return runtimeInfo{
			Bin:               override,
			SupportsYTDLPArgs: supportsYTDLPArgs(override),
		}
	}

	candidates := discoverSCDLCandidates()
	if len(candidates) == 0 {
		return runtimeInfo{
			Bin:               "scdl",
			SupportsYTDLPArgs: true,
		}
	}

	fallback := candidates[0]
	for _, candidate := range candidates {
		if supportsYTDLPArgs(candidate) {
			return runtimeInfo{
				Bin:               candidate,
				SupportsYTDLPArgs: true,
			}
		}
	}

	return runtimeInfo{
		Bin:               fallback,
		SupportsYTDLPArgs: false,
	}
}

func discoverSCDLCandidates() []string {
	seen := map[string]struct{}{}
	candidates := []string{}
	addCandidate := func(path string) {
		cleaned := filepath.Clean(strings.TrimSpace(path))
		if cleaned == "" {
			return
		}
		if _, ok := seen[cleaned]; ok {
			return
		}
		info, err := os.Stat(cleaned)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			return
		}
		seen[cleaned] = struct{}{}
		candidates = append(candidates, cleaned)
	}

	if lookup, err := exec.LookPath("scdl"); err == nil {
		addCandidate(lookup)
	}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		addCandidate(filepath.Join(dir, "scdl"))
	}

	return candidates
}

func supportsYTDLPArgs(binary string) bool {
	cmd := exec.Command(binary, "-h")
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) == 0 {
		return false
	}
	return strings.Contains(strings.ToLower(string(output)), "--yt-dlp-args")
}

func resetRuntimeDetectionForTests() {
	detectRuntimeOnce = sync.Once{}
	detectedRuntime = runtimeInfo{}
}

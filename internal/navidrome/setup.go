package navidrome

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SetupPlanVersion is bumped whenever the plan shape changes incompatibly.
const SetupPlanVersion = "1"

// File actions in a setup plan.
const (
	FileActionCreate    = "create"
	FileActionReplace   = "replace"
	FileActionUnchanged = "unchanged"
)

// Service actions in a setup plan.
const (
	ServiceActionNone    = "none"
	ServiceActionLoad    = "load"
	ServiceActionRestart = "restart"
)

// PlannedFile is one managed file mutation.
type PlannedFile struct {
	Label   string `json:"label"`
	Path    string `json:"path"`
	Action  string `json:"action"`
	Exists  bool   `json:"exists"`
	Owned   bool   `json:"owned"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
	// ExistingSHA256 pins the on-disk state the plan was built against so a
	// concurrent edit cannot be silently overwritten at apply time.
	ExistingSHA256 string `json:"existing_sha256,omitempty"`
	ContentSHA256  string `json:"content_sha256"`
}

// PlannedDirectory is a managed directory that apply will create.
type PlannedDirectory struct {
	Label  string `json:"label"`
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// PlannedService describes the launchd change apply will make.
type PlannedService struct {
	Label   string `json:"label"`
	Action  string `json:"action"`
	Running bool   `json:"running"`
}

// SetupPlan is the immutable, checksummed description of a setup apply.
type SetupPlan struct {
	Version        string             `json:"version"`
	GeneratedAt    string             `json:"generated_at"`
	ConfigPath     string             `json:"config_path,omitempty"`
	BinaryPath     string             `json:"binary_path"`
	ServerVersion  string             `json:"server_version"`
	MusicDir       string             `json:"music_dir"`
	DataDir        string             `json:"data_dir"`
	Port           int                `json:"port"`
	Directories    []PlannedDirectory `json:"directories"`
	Files          []PlannedFile      `json:"files"`
	Service        PlannedService     `json:"service"`
	Blockers       []string           `json:"blockers"`
	Warnings       []string           `json:"warnings"`
	ChecksumSHA256 string             `json:"checksum_sha256"`
}

// Applicable reports whether apply may proceed.
func (p SetupPlan) Applicable() bool { return len(p.Blockers) == 0 }

// Changes reports whether apply would mutate anything.
func (p SetupPlan) Changes() bool {
	for _, file := range p.Files {
		if file.Action != FileActionUnchanged {
			return true
		}
	}
	for _, dir := range p.Directories {
		if !dir.Exists {
			return true
		}
	}
	return p.Service.Action != ServiceActionNone
}

// Planner builds and applies setup plans.
type Planner struct {
	Deps    DependencyChecker
	Service Service
	Now     func() time.Time
}

func (p Planner) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

// Plan inspects the system and produces a checksummed plan. It performs no
// writes and no service changes.
func (p Planner) Plan(ctx context.Context, cfg Config, configPath string) (SetupPlan, error) {
	if err := Validate(cfg); err != nil {
		return SetupPlan{}, err
	}
	resolved, err := Resolve(cfg)
	if err != nil {
		return SetupPlan{}, err
	}

	plan := SetupPlan{
		Version:     SetupPlanVersion,
		GeneratedAt: p.now().Format(time.RFC3339),
		ConfigPath:  configPath,
		MusicDir:    resolved.MusicDir,
		DataDir:     resolved.DataDir,
		Port:        cfg.Server.Port,
		Blockers:    []string{},
		Warnings:    []string{},
	}

	deps := p.Deps.Status(ctx)
	plan.BinaryPath = deps.BinaryPath
	plan.ServerVersion = deps.Version
	if !deps.Ready() {
		plan.Blockers = append(plan.Blockers, deps.Problems...)
	} else if !deps.FFmpegInstalled {
		// Navidrome runs without ffmpeg, so this does not block setup — but a
		// library that is mostly m4a is unplayable until it is installed.
		plan.Warnings = append(plan.Warnings,
			"ffmpeg is not installed; transcoded formats (m4a, wav, flac) will fail to stream or download. Run `brew install ffmpeg`")
	}

	if info, statErr := os.Stat(resolved.MusicDir); statErr != nil || !info.IsDir() {
		plan.Blockers = append(plan.Blockers,
			fmt.Sprintf("music directory %s does not exist", resolved.MusicDir))
	}

	// The log file and the data folder normally share a directory, so the same
	// path would otherwise be listed twice in a plan the user reads.
	seenDirs := map[string]struct{}{}
	for _, dir := range []struct {
		label string
		path  string
	}{
		{"data", resolved.DataDir},
		{"cache", resolved.CacheDir},
		{"backups", resolved.BackupDir},
		{"playlists", resolved.PlaylistsDir},
		{"logs", filepath.Dir(resolved.LogFile)},
		{"launch agents", filepath.Dir(resolved.LaunchAgent)},
	} {
		if dir.path == "" {
			continue
		}
		if _, exists := seenDirs[dir.path]; exists {
			continue
		}
		seenDirs[dir.path] = struct{}{}
		info, statErr := os.Stat(dir.path)
		plan.Directories = append(plan.Directories, PlannedDirectory{
			Label: dir.label, Path: dir.path, Exists: statErr == nil && info.IsDir(),
		})
	}

	tomlFile, err := plannedFile("navidrome.toml", resolved.ConfigFile, RenderTOML(cfg, resolved, deps.FFmpegPath), "0600")
	if err != nil {
		return SetupPlan{}, err
	}
	plan.Files = append(plan.Files, tomlFile)

	agentFile, err := plannedFile("LaunchAgent", resolved.LaunchAgent, RenderLaunchAgent(deps.BinaryPath, deps.FFmpegPath, resolved), "0644")
	if err != nil {
		return SetupPlan{}, err
	}
	plan.Files = append(plan.Files, agentFile)

	for _, file := range plan.Files {
		if file.Exists && !file.Owned {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf(
				"%s already exists at %s and is not UDL-managed; UDL will not overwrite it",
				file.Label, file.Path))
		}
	}

	serviceStatus := p.Service.Status(ctx, cfg, resolved)
	plan.Service = PlannedService{Label: LaunchAgentLabel, Running: serviceStatus.State == ServiceRunning}
	switch {
	case serviceStatus.State == ServiceRunning:
		plan.Service.Action = ServiceActionRestart
	default:
		plan.Service.Action = ServiceActionLoad
	}

	if pid, ok := p.Service.PortListener(ctx, cfg.Server.Port); ok && pid > 0 && pid != serviceStatus.PID {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf(
			"port %d is already in use by PID %d; stop it or choose another port", cfg.Server.Port, pid))
	}

	if strings.TrimSpace(cfg.Server.Username) == "" {
		plan.Warnings = append(plan.Warnings,
			"no account username is configured yet; create the first admin account after the server starts")
	}

	if err := SignSetupPlan(&plan); err != nil {
		return SetupPlan{}, err
	}
	return plan, nil
}

func plannedFile(label, path, content, mode string) (PlannedFile, error) {
	file := PlannedFile{
		Label:         label,
		Path:          path,
		Content:       content,
		Mode:          mode,
		ContentSHA256: hashString(content),
		Action:        FileActionCreate,
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return file, nil
		}
		return PlannedFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	file.Exists = true
	file.Owned = IsUDLOwned(string(existing))
	file.ExistingSHA256 = hashString(string(existing))
	if file.ExistingSHA256 == file.ContentSHA256 {
		file.Action = FileActionUnchanged
	} else {
		file.Action = FileActionReplace
	}
	return file, nil
}

// ApplyResult records what a setup apply actually did.
type ApplyResult struct {
	WrittenFiles       []string `json:"written_files"`
	CreatedDirectories []string `json:"created_directories"`
	ServiceAction      string   `json:"service_action"`
	Message            string   `json:"message"`
}

// Apply revalidates the plan against the live system and then performs it.
func (p Planner) Apply(ctx context.Context, cfg Config, plan SetupPlan) (ApplyResult, error) {
	result := ApplyResult{WrittenFiles: []string{}, CreatedDirectories: []string{}}
	if err := VerifySetupPlanChecksum(plan); err != nil {
		return result, err
	}
	if plan.Version != SetupPlanVersion {
		return result, fmt.Errorf("setup plan version %q is not supported; regenerate the plan", plan.Version)
	}
	if !plan.Applicable() {
		return result, fmt.Errorf("setup plan has unresolved blockers: %s", strings.Join(plan.Blockers, "; "))
	}
	resolved, err := Resolve(cfg)
	if err != nil {
		return result, err
	}

	// Revalidate: the world may have changed since the plan was written.
	for _, file := range plan.Files {
		existing, readErr := os.ReadFile(file.Path)
		switch {
		case readErr != nil && os.IsNotExist(readErr):
			if file.ExistingSHA256 != "" {
				return result, fmt.Errorf("%s disappeared since the plan was generated; regenerate the plan", file.Path)
			}
		case readErr != nil:
			return result, fmt.Errorf("read %s: %w", file.Path, readErr)
		default:
			if !IsUDLOwned(string(existing)) {
				return result, fmt.Errorf("%s is not UDL-managed; UDL will not overwrite it", file.Path)
			}
			if hashString(string(existing)) != file.ExistingSHA256 {
				return result, fmt.Errorf("%s changed since the plan was generated; regenerate the plan", file.Path)
			}
		}
		if hashString(file.Content) != file.ContentSHA256 {
			return result, fmt.Errorf("planned content for %s does not match its checksum", file.Path)
		}
	}

	for _, dir := range plan.Directories {
		if dir.Exists {
			continue
		}
		if err := os.MkdirAll(dir.Path, 0o755); err != nil {
			return result, fmt.Errorf("create %s directory %s: %w", dir.Label, dir.Path, err)
		}
		result.CreatedDirectories = append(result.CreatedDirectories, dir.Path)
	}

	for _, file := range plan.Files {
		if file.Action == FileActionUnchanged {
			continue
		}
		mode := os.FileMode(0o644)
		if file.Mode == "0600" {
			mode = 0o600
		}
		if err := AtomicWrite(file.Path, []byte(file.Content), mode); err != nil {
			return result, err
		}
		result.WrittenFiles = append(result.WrittenFiles, file.Path)
	}

	switch plan.Service.Action {
	case ServiceActionNone:
	default:
		if err := p.Service.Restart(ctx, resolved); err != nil {
			return result, err
		}
		result.ServiceAction = plan.Service.Action
	}
	result.Message = fmt.Sprintf("Navidrome is managed by UDL at %s", resolved.ConfigFile)
	return result, nil
}

// SignSetupPlan computes and stores the plan checksum.
func SignSetupPlan(plan *SetupPlan) error {
	plan.ChecksumSHA256 = ""
	sum, err := setupPlanChecksum(*plan)
	if err != nil {
		return err
	}
	plan.ChecksumSHA256 = sum
	return nil
}

// VerifySetupPlanChecksum rejects a modified or unsigned plan.
func VerifySetupPlanChecksum(plan SetupPlan) error {
	expected := strings.TrimSpace(plan.ChecksumSHA256)
	if expected == "" {
		return fmt.Errorf("setup plan checksum is missing")
	}
	plan.ChecksumSHA256 = ""
	actual, err := setupPlanChecksum(plan)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("setup plan checksum mismatch; regenerate the plan")
	}
	return nil
}

func setupPlanChecksum(plan SetupPlan) (string, error) {
	plan.ChecksumSHA256 = ""
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode setup plan: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// WriteSetupPlan stores a plan for a later apply.
func WriteSetupPlan(path string, plan SetupPlan) error {
	payload, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode setup plan: %w", err)
	}
	payload = append(payload, '\n')
	return AtomicWrite(path, payload, 0o600)
}

// ReadSetupPlan loads and verifies a stored plan.
func ReadSetupPlan(path string) (SetupPlan, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return SetupPlan{}, fmt.Errorf("read setup plan %s: %w", path, err)
	}
	var plan SetupPlan
	if err := json.Unmarshal(payload, &plan); err != nil {
		return SetupPlan{}, fmt.Errorf("parse setup plan %s: %w", path, err)
	}
	if err := VerifySetupPlanChecksum(plan); err != nil {
		return SetupPlan{}, err
	}
	return plan, nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

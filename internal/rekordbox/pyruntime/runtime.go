package pyruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
)

const (
	PackageName        = "pyrekordbox"
	PackageVersion     = "0.4.4"
	RequirementsText   = PackageName + "==" + PackageVersion + "\n"
	ManifestFileName   = "udl-rekordbox-runtime.json"
	BootstrapEnvVar    = "UDL_REKORDBOX_BOOTSTRAP_PYTHON"
	DefaultPythonMajor = 3
	DefaultPythonMinor = 8
)

type Resolver struct {
	LookPath func(string) (string, error)
	Run      func(context.Context, string, ...string) ([]byte, []byte, error)
	Now      func() time.Time
}

type Request struct {
	Config         config.Config
	PythonBin      string
	PythonPath     string
	ExplicitPython bool
}

type Runtime struct {
	PythonBin  string `json:"python_bin"`
	PythonPath string `json:"python_path,omitempty"`
	Managed    bool   `json:"managed"`
	VenvDir    string `json:"venv_dir,omitempty"`
	Version    string `json:"pyrekordbox_version,omitempty"`
}

type Status struct {
	Runtime
	Installed bool   `json:"installed"`
	Healthy   bool   `json:"healthy"`
	Message   string `json:"message"`
}

type manifest struct {
	RequirementsSHA256 string `json:"requirements_sha256"`
	PythonBin          string `json:"python_bin"`
	PythonVersion      string `json:"python_version"`
	PyrekordboxVersion string `json:"pyrekordbox_version"`
	InstalledAt        string `json:"installed_at"`
}

func (r Resolver) Status(ctx context.Context, req Request) Status {
	rt, explicit := r.initialRuntime(req)
	if explicit {
		version, err := r.verify(ctx, rt.PythonBin, rt.PythonPath)
		if err != nil {
			return Status{Runtime: rt, Installed: false, Healthy: false, Message: missingMessage(rt.PythonBin, err)}
		}
		rt.Version = version
		return Status{Runtime: rt, Installed: true, Healthy: true, Message: "selected Python runtime can import pyrekordbox"}
	}

	venvDir, err := ManagedVenvDir(req.Config)
	if err != nil {
		return Status{Runtime: rt, Installed: false, Healthy: false, Message: err.Error()}
	}
	rt = Runtime{PythonBin: ManagedPythonBin(venvDir), Managed: true, VenvDir: venvDir}
	if _, err := os.Stat(rt.PythonBin); err != nil {
		return Status{Runtime: rt, Installed: false, Healthy: false, Message: "managed Rekordbox Python runtime is not installed"}
	}
	if ok, message := r.manifestCurrent(venvDir); !ok {
		return Status{Runtime: rt, Installed: true, Healthy: false, Message: message}
	}
	version, err := r.verify(ctx, rt.PythonBin, "")
	if err != nil {
		return Status{Runtime: rt, Installed: true, Healthy: false, Message: missingMessage(rt.PythonBin, err)}
	}
	rt.Version = version
	return Status{Runtime: rt, Installed: true, Healthy: true, Message: "managed Rekordbox Python runtime is ready"}
}

func (r Resolver) Ensure(ctx context.Context, req Request) (Runtime, error) {
	rt, explicit := r.initialRuntime(req)
	if explicit {
		version, err := r.verify(ctx, rt.PythonBin, rt.PythonPath)
		if err != nil {
			return Runtime{}, fmt.Errorf("%s", missingMessage(rt.PythonBin, err))
		}
		rt.Version = version
		return rt, nil
	}

	status := r.Status(ctx, req)
	if status.Healthy {
		return status.Runtime, nil
	}

	venvDir, err := ManagedVenvDir(req.Config)
	if err != nil {
		return Runtime{}, err
	}
	if status.Installed {
		if err := os.RemoveAll(venvDir); err != nil {
			return Runtime{}, fmt.Errorf("reset stale Rekordbox Python runtime: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(venvDir), 0o755); err != nil {
		return Runtime{}, fmt.Errorf("create Rekordbox runtime directory: %w", err)
	}

	basePython, err := r.findBootstrapPython()
	if err != nil {
		return Runtime{}, err
	}
	if _, stderr, err := r.run(ctx, basePython, "-m", "venv", venvDir); err != nil {
		return Runtime{}, fmt.Errorf("create Rekordbox Python venv: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	managedPython := ManagedPythonBin(venvDir)
	if _, stderr, err := r.run(ctx, managedPython, "-m", "pip", "install", "--disable-pip-version-check", "--upgrade", "pip"); err != nil {
		return Runtime{}, fmt.Errorf("upgrade Rekordbox Python pip: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	if _, stderr, err := r.run(ctx, managedPython, "-m", "pip", "install", "--disable-pip-version-check", PackageName+"=="+PackageVersion); err != nil {
		return Runtime{}, fmt.Errorf("install Rekordbox Python dependencies: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	version, err := r.verify(ctx, managedPython, "")
	if err != nil {
		return Runtime{}, fmt.Errorf("verify Rekordbox Python runtime: %w", err)
	}
	if err := r.writeManifest(ctx, venvDir, managedPython, version); err != nil {
		return Runtime{}, err
	}
	return Runtime{PythonBin: managedPython, Managed: true, VenvDir: venvDir, Version: version}, nil
}

func (r Resolver) Reset(req Request) error {
	if req.ExplicitPython || strings.TrimSpace(req.PythonBin) != "" || strings.TrimSpace(req.PythonPath) != "" {
		return fmt.Errorf("reset only applies to the managed Rekordbox Python runtime")
	}
	venvDir, err := ManagedVenvDir(req.Config)
	if err != nil {
		return err
	}
	return os.RemoveAll(venvDir)
}

func ManagedVenvDir(cfg config.Config) (string, error) {
	stateDir, err := config.ExpandPath(cfg.Defaults.StateDir)
	if err != nil {
		return "", fmt.Errorf("resolve state dir for Rekordbox Python runtime: %w", err)
	}
	if strings.TrimSpace(stateDir) == "" {
		return "", fmt.Errorf("defaults.state_dir must be set for managed Rekordbox Python runtime")
	}
	return filepath.Join(stateDir, "rekordbox", "python-venv"), nil
}

func ManagedPythonBin(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python")
}

func RequiresManaged(req Request) bool {
	if req.ExplicitPython {
		return false
	}
	if strings.TrimSpace(req.PythonBin) != "" || strings.TrimSpace(req.PythonPath) != "" {
		return false
	}
	if rb := req.Config.Rekordbox; rb != nil {
		if strings.TrimSpace(rb.PythonBin) != "" || strings.TrimSpace(rb.PythonPath) != "" {
			return false
		}
	}
	if strings.TrimSpace(os.Getenv("UDL_REKORDBOX_PYTHON_BIN")) != "" || strings.TrimSpace(os.Getenv("UDL_REKORDBOX_PYTHONPATH")) != "" {
		return false
	}
	return true
}

func (r Resolver) initialRuntime(req Request) (Runtime, bool) {
	explicit := !RequiresManaged(req)
	pythonBin := strings.TrimSpace(req.PythonBin)
	pythonPath := strings.TrimSpace(req.PythonPath)
	if pythonBin == "" && req.Config.Rekordbox != nil {
		pythonBin = strings.TrimSpace(req.Config.Rekordbox.PythonBin)
	}
	if pythonPath == "" && req.Config.Rekordbox != nil {
		pythonPath = strings.TrimSpace(req.Config.Rekordbox.PythonPath)
	}
	if pythonBin == "" {
		pythonBin = "python3"
	}
	return Runtime{PythonBin: pythonBin, PythonPath: pythonPath}, explicit
}

func (r Resolver) findBootstrapPython() (string, error) {
	candidates := []string{}
	if env := strings.TrimSpace(os.Getenv(BootstrapEnvVar)); env != "" {
		candidates = append(candidates, env)
	}
	candidates = append(candidates,
		"python3.12",
		"/opt/homebrew/opt/python@3.12/bin/python3.12",
		"/usr/local/opt/python@3.12/bin/python3.12",
		"python3",
	)
	for _, candidate := range candidates {
		path := candidate
		if !strings.Contains(candidate, string(os.PathSeparator)) {
			found, err := r.lookPath(candidate)
			if err != nil {
				continue
			}
			path = found
		}
		if r.pythonVersionOK(path) {
			return path, nil
		}
	}
	return "", fmt.Errorf("no compatible Python >=%d.%d found for managed Rekordbox runtime", DefaultPythonMajor, DefaultPythonMinor)
}

func (r Resolver) pythonVersionOK(pythonBin string) bool {
	out, _, err := r.run(context.Background(), pythonBin, "-c", "import sys; raise SystemExit(0 if sys.version_info >= (3, 8) else 1)")
	return err == nil && out != nil
}

func (r Resolver) verify(ctx context.Context, pythonBin, pythonPath string) (string, error) {
	code := "import importlib.metadata as m; import pyrekordbox; import sqlcipher3; print(m.version('pyrekordbox'))"
	stdout, stderr, err := r.runWithEnv(ctx, pythonBin, pythonPath, "-c", code)
	if err != nil {
		detail := strings.TrimSpace(string(stderr))
		if detail == "" {
			detail = strings.TrimSpace(string(stdout))
		}
		return "", fmt.Errorf("%w: %s", err, detail)
	}
	return strings.TrimSpace(string(stdout)), nil
}

func (r Resolver) manifestCurrent(venvDir string) (bool, string) {
	body, err := os.ReadFile(filepath.Join(venvDir, ManifestFileName))
	if err != nil {
		return false, "managed Rekordbox Python runtime manifest is missing"
	}
	var mf manifest
	if err := json.Unmarshal(body, &mf); err != nil {
		return false, "managed Rekordbox Python runtime manifest is invalid"
	}
	if mf.RequirementsSHA256 != requirementsDigest() {
		return false, "managed Rekordbox Python requirements changed"
	}
	return true, ""
}

func (r Resolver) writeManifest(ctx context.Context, venvDir, pythonBin, version string) error {
	pythonVersion, stderr, err := r.run(ctx, pythonBin, "-c", "import sys; print('.'.join(map(str, sys.version_info[:3])))")
	if err != nil {
		return fmt.Errorf("read Rekordbox Python version: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	now := time.Now()
	if r.Now != nil {
		now = r.Now()
	}
	body, err := json.MarshalIndent(manifest{
		RequirementsSHA256: requirementsDigest(),
		PythonBin:          pythonBin,
		PythonVersion:      strings.TrimSpace(string(pythonVersion)),
		PyrekordboxVersion: version,
		InstalledAt:        now.Format(time.RFC3339),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(venvDir, ManifestFileName), append(body, '\n'), 0o644)
}

func requirementsDigest() string {
	sum := sha256.Sum256([]byte(RequirementsText))
	return hex.EncodeToString(sum[:])
}

func missingMessage(pythonBin string, err error) string {
	return fmt.Sprintf("pyrekordbox is missing from the selected Python runtime %q; run `udl rekordbox deps ensure` or configure rekordbox.python_bin/python_path: %v", pythonBin, err)
}

func (r Resolver) lookPath(name string) (string, error) {
	if r.LookPath != nil {
		return r.LookPath(name)
	}
	return exec.LookPath(name)
}

func (r Resolver) run(ctx context.Context, binary string, args ...string) ([]byte, []byte, error) {
	return r.runWithEnv(ctx, binary, "", args...)
}

func (r Resolver) runWithEnv(ctx context.Context, binary, pythonPath string, args ...string) ([]byte, []byte, error) {
	if r.Run != nil && strings.TrimSpace(pythonPath) == "" {
		return r.Run(ctx, binary, args...)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = os.Environ()
	if strings.TrimSpace(pythonPath) != "" {
		cmd.Env = mergePythonPath(cmd.Env, pythonPath)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func mergePythonPath(env []string, pythonPath string) []string {
	merged := pythonPath
	if existing := os.Getenv("PYTHONPATH"); strings.TrimSpace(existing) != "" {
		merged += string(os.PathListSeparator) + existing
	}
	result := make([]string, 0, len(env)+1)
	for _, item := range env {
		if strings.HasPrefix(item, "PYTHONPATH=") {
			continue
		}
		result = append(result, item)
	}
	return append(result, "PYTHONPATH="+merged)
}

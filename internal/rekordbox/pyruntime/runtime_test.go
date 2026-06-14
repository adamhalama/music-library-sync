package pyruntime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/config"
)

func TestRequiresManagedWhenNoExplicitPython(t *testing.T) {
	t.Setenv("UDL_REKORDBOX_PYTHON_BIN", "")
	t.Setenv("UDL_REKORDBOX_PYTHONPATH", "")
	cfg := config.DefaultConfig()
	cfg.Defaults.StateDir = t.TempDir()

	if !RequiresManaged(Request{Config: cfg}) {
		t.Fatalf("expected managed runtime by default")
	}
}

func TestRequiresManagedRespectsExplicitConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Defaults.StateDir = t.TempDir()
	cfg.Rekordbox = &config.RekordboxConfig{PythonBin: "/custom/python"}

	if RequiresManaged(Request{Config: cfg}) {
		t.Fatalf("expected explicit config python to disable managed runtime")
	}
}

func TestStatusReportsMissingManagedVenv(t *testing.T) {
	t.Setenv("UDL_REKORDBOX_PYTHON_BIN", "")
	t.Setenv("UDL_REKORDBOX_PYTHONPATH", "")
	cfg := config.DefaultConfig()
	cfg.Defaults.StateDir = t.TempDir()

	status := (Resolver{}).Status(context.Background(), Request{Config: cfg})
	if status.Healthy || status.Installed {
		t.Fatalf("expected missing managed venv, got %+v", status)
	}
	if !status.Managed || !strings.Contains(status.VenvDir, filepath.Join("rekordbox", "python-venv")) {
		t.Fatalf("expected managed runtime path, got %+v", status)
	}
}

func TestStatusAcceptsHealthyManagedVenv(t *testing.T) {
	t.Setenv("UDL_REKORDBOX_PYTHON_BIN", "")
	t.Setenv("UDL_REKORDBOX_PYTHONPATH", "")
	cfg := config.DefaultConfig()
	cfg.Defaults.StateDir = t.TempDir()
	venvDir, err := ManagedVenvDir(cfg)
	if err != nil {
		t.Fatalf("ManagedVenvDir: %v", err)
	}
	pythonBin := ManagedPythonBin(venvDir)
	if err := os.MkdirAll(filepath.Dir(pythonBin), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(pythonBin, []byte("# fake"), 0o755); err != nil {
		t.Fatalf("write python: %v", err)
	}
	body := []byte(`{"requirements_sha256":"` + requirementsDigest() + `"}` + "\n")
	if err := os.WriteFile(filepath.Join(venvDir, ManifestFileName), body, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	resolver := Resolver{
		Run: func(ctx context.Context, binary string, args ...string) ([]byte, []byte, error) {
			if strings.Contains(strings.Join(args, " "), "pyrekordbox") {
				return []byte("0.4.4\n"), nil, nil
			}
			return []byte("3.12.0\n"), nil, nil
		},
	}
	status := resolver.Status(context.Background(), Request{Config: cfg})
	if !status.Healthy || !status.Installed || status.Version != "0.4.4" {
		t.Fatalf("expected healthy managed venv, got %+v", status)
	}
}

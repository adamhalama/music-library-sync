package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jaa/update-downloads/internal/doctor"
	"github.com/jaa/update-downloads/internal/exitcode"
)

func TestDoctorRunSupportsZeroSourceOnboardingState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "udl.yaml")
	payload := `version: 1
defaults:
  state_dir: ` + filepath.Join(dir, "state") + `
  archive_file: archive.txt
  threads: 1
  continue_on_error: true
  command_timeout_seconds: 900
sources: []
`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	value, rpcErr := (&Server{WorkingDir: dir, ConfigPath: path}).runDoctor()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	result := value.(doctorRunResult)
	if len(result.Checks) < 2 {
		t.Fatalf("unexpected zero-source doctor result: %+v", result)
	}
	if result.Checks[0].Name != "config" || result.Checks[1].Name != "config" {
		t.Fatalf("zero-source guidance must lead the report: %+v", result.Checks)
	}
	if result.ExitCode != exitcode.Success && result.ExitCode != exitcode.MissingDependency {
		t.Fatalf("unexpected doctor exit code: %d", result.ExitCode)
	}
	if result.EffectivePATH == "" {
		t.Fatal("effective PATH was not reported")
	}
}

func TestDoctorStructuredStatusRemediationAndDependencyPath(t *testing.T) {
	check := doctor.Check{Severity: doctor.SeverityError, Name: "dependency", Message: "scdl not found in PATH"}
	if got := doctorRemediation(check); got == "" {
		t.Fatal("missing dependency remediation")
	}
	binary, path, ok := resolvedDependency("yt-dlp found at /opt/homebrew/bin/yt-dlp")
	if !ok || binary != "yt-dlp" || path != "/opt/homebrew/bin/yt-dlp" {
		t.Fatalf("unexpected dependency parse: %q %q %v", binary, path, ok)
	}
}

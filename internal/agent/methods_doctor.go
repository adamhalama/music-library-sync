package agent

import (
	"context"
	"os"
	"strings"

	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/doctor"
	"github.com/jaa/update-downloads/internal/exitcode"
)

type doctorCheckResult struct {
	Name        string          `json:"name"`
	Severity    doctor.Severity `json:"severity"`
	Status      string          `json:"status"`
	Detail      string          `json:"detail"`
	Remediation string          `json:"remediation,omitempty"`
}

type doctorRunResult struct {
	Checks               []doctorCheckResult `json:"checks"`
	EffectivePATH        string              `json:"effective_path"`
	ResolvedDependencies map[string]string   `json:"resolved_dependencies"`
	ExitCode             int                 `json:"exit_code"`
}

func (s *Server) runDoctor() (any, *RPCError) {
	cfg, err := config.Load(config.LoadOptions{ExplicitPath: s.ConfigPath, WorkingDir: s.WorkingDir})
	if err != nil {
		return nil, configRPCError(err)
	}
	if len(cfg.Sources) > 0 {
		if err := config.Validate(cfg); err != nil {
			return nil, configRPCError(err)
		}
	}
	report := app.DoctorUseCase{Checker: s.DoctorChecker}.Run(context.Background(), cfg)
	result := doctorRunResult{
		Checks: make([]doctorCheckResult, 0, len(report.Checks)),
		EffectivePATH: os.Getenv("PATH"),
		ResolvedDependencies: map[string]string{},
		ExitCode: exitcode.Success,
	}
	for _, check := range report.Checks {
		status := "ok"
		if check.Severity == doctor.SeverityWarn {
			status = "warning"
		} else if check.Severity == doctor.SeverityError {
			status = "blocked"
			result.ExitCode = exitcode.MissingDependency
		}
		structured := doctorCheckResult{
			Name: check.Name, Severity: check.Severity, Status: status, Detail: check.Message,
			Remediation: doctorRemediation(check),
		}
		result.Checks = append(result.Checks, structured)
		if binary, path, ok := resolvedDependency(check.Message); ok {
			result.ResolvedDependencies[binary] = path
		}
	}
	return result, nil
}

func doctorRemediation(check doctor.Check) string {
	if check.Severity != doctor.SeverityError {
		return ""
	}
	switch check.Name {
	case "dependency":
		return "Install or repair the dependency, then run Doctor again."
	case "auth":
		return "Open Credentials and replace or clear the affected credential."
	case "filesystem":
		return "Choose an accessible writable folder and verify its permissions."
	case "rekordbox":
		return "Open Rekordbox settings, repair dependencies, and retry after closing Rekordbox."
	default:
		return "Resolve the reported issue, then run Doctor again."
	}
}

func resolvedDependency(message string) (string, string, bool) {
	const marker = " found at "
	index := strings.Index(message, marker)
	if index <= 0 {
		return "", "", false
	}
	binary := strings.TrimSpace(message[:index])
	path := strings.TrimSpace(message[index+len(marker):])
	if binary == "" || path == "" {
		return "", "", false
	}
	return binary, path, true
}

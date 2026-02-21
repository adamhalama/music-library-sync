package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/doctor"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/spf13/cobra"
)

func newDoctorCommand(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check dependencies, auth, and filesystem readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}

			if len(cfg.Sources) > 0 {
				if err := config.Validate(cfg); err != nil {
					return withExitCode(exitcode.InvalidConfig, err)
				}
			}

			report := doctor.NewChecker().Check(context.Background(), cfg)

			if app.Opts.JSON {
				encoder := json.NewEncoder(app.IO.Out)
				if err := encoder.Encode(report); err != nil {
					return withExitCode(exitcode.RuntimeFailure, err)
				}
			} else {
				checks := append([]doctor.Check{}, report.Checks...)
				sort.SliceStable(checks, func(i, j int) bool {
					return checks[i].Name < checks[j].Name
				})
				for _, check := range checks {
					fmt.Fprintf(app.IO.Out, "[%s] %s: %s\n", check.Severity, check.Name, check.Message)
				}
			}

			if report.HasErrors() {
				return withExitCode(exitcode.MissingDependency, fmt.Errorf("doctor found %d error(s)", report.ErrorCount()))
			}
			return nil
		},
	}
}

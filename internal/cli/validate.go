package cli

import (
	"encoding/json"
	"fmt"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/spf13/cobra"
)

func newValidateCommand(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate config schema and source definitions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}

			if err := config.Validate(cfg); err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}

			if app.Opts.JSON {
				payload := map[string]any{"valid": true}
				encoded, _ := json.Marshal(payload)
				fmt.Fprintln(app.IO.Out, string(encoded))
			} else {
				fmt.Fprintln(app.IO.Out, "Config is valid.")
			}
			return nil
		},
	}
}

package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/spf13/cobra"
)

func newInitCommand(app *AppContext) *cobra.Command {
	force := false

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create starter config and state directories",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := strings.TrimSpace(app.Opts.ConfigPath)
			if path == "" {
				userPath, err := config.UserConfigPath()
				if err != nil {
					return withExitCode(exitcode.RuntimeFailure, err)
				}
				path = userPath
			}

			if err := config.EnsureConfigDir(path); err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}

			if _, err := os.Stat(path); err == nil && !force {
				if app.Opts.NoInput || !isTTY(os.Stdin) {
					return withExitCode(exitcode.RuntimeFailure, fmt.Errorf("config already exists at %s (rerun with --force)", path))
				}
				confirmed, confirmErr := promptYesNo(app, fmt.Sprintf("Config already exists at %s. Overwrite?", path))
				if confirmErr != nil {
					return withExitCode(exitcode.RuntimeFailure, confirmErr)
				}
				if !confirmed {
					fmt.Fprintln(app.IO.Out, "Initialization canceled.")
					return nil
				}
			}

			if err := os.WriteFile(path, []byte(config.DefaultTemplate()), 0o644); err != nil {
				return withExitCode(exitcode.RuntimeFailure, fmt.Errorf("write config file: %w", err))
			}

			stateDir, err := config.ExpandPath(config.DefaultConfig().Defaults.StateDir)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, fmt.Errorf("resolve state directory: %w", err))
			}
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				return withExitCode(exitcode.RuntimeFailure, fmt.Errorf("create state directory %s: %w", stateDir, err))
			}

			fmt.Fprintf(app.IO.Out, "Wrote config: %s\n", path)
			fmt.Fprintf(app.IO.Out, "Ensured state dir: %s\n", stateDir)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config file")
	return cmd
}

func promptYesNo(app *AppContext, prompt string) (bool, error) {
	fmt.Fprintf(app.IO.Out, "%s [y/N]: ", prompt)
	reader := bufio.NewReader(app.IO.In)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	response := strings.ToLower(strings.TrimSpace(line))
	return response == "y" || response == "yes", nil
}

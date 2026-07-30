package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/agent"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/spf13/cobra"
)

func newAgentCommand(app *AppContext) *cobra.Command {
	var workingDir string
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the JSON-RPC backend for the native frontend",
		Long:  "Run the persistent JSON-RPC 2.0 backend used by the native macOS frontend.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := resolveAgentWorkingDir(workingDir)
			if err != nil {
				return err
			}
			previous, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve current working directory: %w", err)
			}
			if err := os.Chdir(resolved); err != nil {
				return fmt.Errorf("use agent working directory %s: %w", resolved, err)
			}
			defer func() { _ = os.Chdir(previous) }()

			server := &agent.Server{
				Conn: agent.NewConn(cmd.InOrStdin(), cmd.OutOrStdout()),
				Runs: agent.NewRunRegistry(),
				Build: agent.BuildInfo{
					Version: app.Build.Version,
					Commit:  app.Build.Commit,
					Date:    app.Build.Date,
				},
				WorkingDir:          resolved,
				ConfigPath:          app.Opts.ConfigPath,
				FreeDLConfigPath:    app.Opts.FreeDLConfigPath,
				PlaylistsConfigPath: app.Opts.PlaylistsConfigPath,
				RekordboxConfigPath: app.Opts.RekordboxConfigPath,
				ErrOut:              cmd.ErrOrStderr(),
			}
			return server.Serve(context.Background())
		},
	}
	cmd.Flags().StringVar(&workingDir, "working-dir", "", "Project working directory (defaults to the current directory)")
	return cmd
}

func resolveAgentWorkingDir(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve agent working directory: %w", err)
		}
		candidate = wd
	}
	expanded, err := config.ExpandPath(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve agent working directory: %w", err)
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve agent working directory: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("inspect agent working directory %s: %w", absolute, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("agent working directory is not a directory: %s", absolute)
	}
	return filepath.Clean(absolute), nil
}

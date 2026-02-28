package cli

import (
	"fmt"
	"os"

	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/spf13/cobra"
)

func Execute(build BuildInfo, streams IOStreams) int {
	if wd, err := os.Getwd(); err == nil {
		if envErr := loadDotEnvFiles(wd, os.Environ(), os.Setenv); envErr != nil {
			fmt.Fprintln(streams.ErrOut, "WARN:", envErr)
		}
	}

	app := &AppContext{Build: build, IO: streams}
	root := newRootCommand(app)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(streams.ErrOut, "ERROR:", err)
		return mapExitCode(err)
	}
	return exitcode.Success
}

func newRootCommand(app *AppContext) *cobra.Command {
	showVersion := false

	root := &cobra.Command{
		Use:   "udl",
		Short: "Sync local downloads from configured music sources",
		Long:  "udl is a config-driven CLI for syncing SoundCloud and Spotify sources through external adapters.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				printVersion(app)
				return nil
			}
			return cmd.Help()
		},
		SilenceErrors:     true,
		SilenceUsage:      true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}

	defaultConfigPath := os.Getenv("UDL_CONFIG")
	root.PersistentFlags().StringVarP(&app.Opts.ConfigPath, "config", "c", defaultConfigPath, "Path to config file")
	root.PersistentFlags().BoolVar(&app.Opts.JSON, "json", false, "Emit newline-delimited JSON events")
	root.PersistentFlags().BoolVarP(&app.Opts.Quiet, "quiet", "q", false, "Reduce output to errors and summary")
	root.PersistentFlags().BoolVarP(&app.Opts.Verbose, "verbose", "v", false, "Increase diagnostic output")
	root.PersistentFlags().BoolVar(&app.Opts.NoColor, "no-color", false, "Disable color output")
	root.PersistentFlags().BoolVar(&app.Opts.NoInput, "no-input", false, "Disable interactive prompts")
	root.PersistentFlags().BoolVarP(&app.Opts.DryRun, "dry-run", "n", false, "Validate and plan execution without running adapters")
	root.Flags().BoolVar(&showVersion, "version", false, "Print version info")

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return withExitCode(exitcode.InvalidUsage, err)
	})

	root.AddCommand(newInitCommand(app))
	root.AddCommand(newValidateCommand(app))
	root.AddCommand(newDoctorCommand(app))
	root.AddCommand(newSyncCommand(app))
	root.AddCommand(newPromoteFreeDLCommand(app))
	root.AddCommand(newVersionCommand(app))

	return root
}

func printVersion(app *AppContext) {
	version := app.Build.Version
	if version == "" {
		version = "dev"
	}
	commit := app.Build.Commit
	if commit == "" {
		commit = "unknown"
	}
	date := app.Build.Date
	if date == "" {
		date = "unknown"
	}

	fmt.Fprintf(app.IO.Out, "udl version %s\ncommit: %s\nbuild_date: %s\n", version, commit, date)
}

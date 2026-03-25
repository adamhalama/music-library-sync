package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	workflows "github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/spf13/cobra"
)

func newInitCommand(app *AppContext) *cobra.Command {
	force := false

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create starter config and state directories",
		RunE: func(cmd *cobra.Command, args []string) error {
			useCase := workflows.InitUseCase{}
			result, err := useCase.Run(workflows.InitRequest{
				ConfigPath: app.Opts.ConfigPath,
				Force:      force,
				NoInput:    app.Opts.NoInput,
				IsTTY:      isTTY(os.Stdin),
			}, initInteraction{app: app})
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if result.Canceled {
				fmt.Fprintln(app.IO.Out, "Initialization canceled.")
				return nil
			}
			fmt.Fprintf(app.IO.Out, "Wrote config: %s\n", result.ConfigPath)
			fmt.Fprintf(app.IO.Out, "Ensured state dir: %s\n", result.StateDir)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config file")
	return cmd
}

type initInteraction struct {
	app *AppContext
}

func (i initInteraction) Confirm(prompt string, defaultYes bool) (bool, error) {
	return promptYesNoDefault(i.app, prompt, defaultYes)
}

func (i initInteraction) Input(prompt string) (string, error) {
	return promptLine(i.app, prompt)
}

func (i initInteraction) SelectRows(sourceID string, rows []engine.PlanRow) (engine.PlanSelectionResult, error) {
	selected := make([]int, 0, len(rows))
	for _, row := range rows {
		if row.Toggleable && row.SelectedByDefault {
			selected = append(selected, row.Index)
		}
	}
	return engine.PlanSelectionResult{
		SelectedIndices: selected,
		DownloadOrder:   engine.DownloadOrderNewestFirst,
	}, nil
}

func promptYesNo(app *AppContext, prompt string) (bool, error) {
	return promptYesNoDefault(app, prompt, false)
}

func promptYesNoDefault(app *AppContext, prompt string, defaultYes bool) (bool, error) {
	suffix := "[y/N]"
	if defaultYes {
		suffix = "[Y/n]"
	}
	fmt.Fprintf(app.IO.Out, "%s %s: ", prompt, suffix)
	reader := bufio.NewReader(app.IO.In)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	response := strings.ToLower(strings.TrimSpace(line))
	if response == "" {
		return defaultYes, nil
	}
	return response == "y" || response == "yes", nil
}

func promptLine(app *AppContext, prompt string) (string, error) {
	fmt.Fprintf(app.IO.Out, "%s: ", prompt)
	reader := bufio.NewReader(app.IO.In)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

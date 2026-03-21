package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func newTUICommand(app *AppContext) *cobra.Command {
	debugMessages := false
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the full-screen TUI shell",
		RunE: func(cmd *cobra.Command, args []string) (runErr error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					_, _ = fmt.Fprint(app.IO.Out, "\x1b[?25h")
					runErr = fmt.Errorf("tui panic recovered: %v", recovered)
				}
			}()
			model := newTUIRootModel(app, debugMessages)
			program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithInput(app.IO.In), tea.WithOutput(app.IO.Out))
			if _, err := program.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&debugMessages, "debug-messages", false, "Show Bubble Tea message tracing footer")
	return cmd
}

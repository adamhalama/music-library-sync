package cli

import "github.com/spf13/cobra"

func newVersionCommand(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version/build metadata",
		Run: func(cmd *cobra.Command, args []string) {
			printVersion(app)
		},
	}
}

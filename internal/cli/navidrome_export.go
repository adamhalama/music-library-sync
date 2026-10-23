package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/spf13/cobra"
)

// newNavidromeExportDBCommand pulls the remote server's database to this Mac.
// In remote mode stars, plays and playlists exist only on the server; this is
// their off-server copy, run weekly by launchd.
func newNavidromeExportDBCommand(app *AppContext) *cobra.Command {
	var outDir string
	var keep int
	cmd := &cobra.Command{
		Use:   "export-db",
		Short: "Copy the remote Navidrome server's database to this Mac",
		Long: strings.TrimSpace(`
Only for navidrome.mode: remote. Asks the server for a fresh, verified backup
of its Navidrome database (stars, plays, playlists) over the mirror's SSH key,
checks its size, sha256 and SQLite header, and stores it as
navidrome-server-<UTC time>.db (mode 0600). The newest --keep copies are kept;
other files in the folder are never touched.

The destination filesystem must support hard links; otherwise the export is
refused rather than risking replacement of an existing copy. The export lock
is per data_dir, not per Mac.

` + "`udl navidrome status`" + ` warns when the last copy failed or is older than 14 days.
`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if keep < 1 {
				return withExitCode(exitcode.InvalidUsage, errors.New("--keep must be at least 1"))
			}
			manager, err := navidromeManager(app, true)
			if err != nil {
				return err
			}
			if !manager.Config.IsRemote() {
				return withExitCode(exitcode.InvalidConfig,
					errors.New("navidrome export-db needs navidrome.mode: remote; a local server's database is already on this Mac (see `udl navidrome backup`)"))
			}
			result, err := manager.ExportServerDB(cmd.Context(), navidrome.ServerDBExportOptions{OutDir: outDir, Keep: keep})
			if app.Opts.JSON {
				if encodeErr := json.NewEncoder(app.IO.Out).Encode(result); encodeErr != nil {
					return encodeErr
				}
			} else if err == nil {
				fmt.Fprintf(app.IO.Out, "Exported %s (%d bytes, sha256 %s)\n", result.Path, result.SizeBytes, result.SHA256)
				fmt.Fprintf(app.IO.Out, "Kept: %d, removed: %d\n", result.Kept, len(result.Removed))
				if app.Opts.Verbose {
					for _, path := range result.Removed {
						fmt.Fprintf(app.IO.Out, "  removed %s\n", path)
					}
				}
			}
			if !app.Opts.JSON {
				printProblems(app, result.Warnings)
			}
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&outDir, "out", "", "folder for the copies (default: <data_dir>/"+navidrome.ServerDBExportDirName+")")
	cmd.Flags().IntVar(&keep, "keep", navidrome.DefaultServerDBExportKeep, "number of newest copies to keep")
	return cmd
}

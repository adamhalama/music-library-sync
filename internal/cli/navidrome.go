package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/spf13/cobra"
)

func newNavidromeCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "navidrome",
		Short: "Manage the local Navidrome server that serves the phone library",
		Long: strings.TrimSpace(`
Manage the UDL-owned Navidrome service that publishes ~/Music/downloaded to
Amperfy on the home network.

Status and doctor checks never change the system. Installing, applying setup,
and importing favorites are explicit, confirmed actions.
`),
	}
	cmd.AddCommand(newNavidromeConfigCommand(app))
	cmd.AddCommand(newNavidromeDepsCommand(app))
	cmd.AddCommand(newNavidromeStatusCommand(app))
	cmd.AddCommand(newNavidromeSetupCommand(app))
	cmd.AddCommand(newNavidromePlaylistsCommand(app))
	cmd.AddCommand(newNavidromeFavoritesCommand(app))
	cmd.AddCommand(newNavidromePhoneCommand(app))
	cmd.AddCommand(newNavidromeBackupCommand(app))
	cmd.AddCommand(newNavidromeDatesCommand(app))
	return cmd
}

func navidromeManager(app *AppContext, skipCredentials bool) (*navidrome.Manager, error) {
	manager, err := navidrome.NewManager(navidrome.ManagerOptions{
		ConfigPath:      app.Opts.NavidromeConfigPath,
		SkipCredentials: skipCredentials,
	})
	if err != nil {
		return nil, withExitCode(exitcode.InvalidConfig, err)
	}
	return manager, nil
}

func newNavidromeConfigCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect the Navidrome feature configuration"}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the configuration file path that would be written",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := navidrome.ResolveWritePath(app.Opts.NavidromeConfigPath, "")
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			fmt.Fprintln(app.IO.Out, path)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the effective Navidrome configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, true)
			if err != nil {
				return err
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(manager.Config)
			}
			payload, err := navidrome.Marshal(manager.Config)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			fmt.Fprint(app.IO.Out, string(payload))
			return nil
		},
	})
	return cmd
}

func newNavidromeDepsCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{Use: "deps", Short: "Inspect or install the Navidrome dependency"}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Report Homebrew and Navidrome availability without changing anything",
		RunE: func(cmd *cobra.Command, args []string) error {
			status := navidrome.DependencyChecker{}.Status(cmd.Context())
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(status)
			}
			fmt.Fprintf(app.IO.Out, "Homebrew: %s\n", yesNo(status.HomebrewInstalled))
			fmt.Fprintf(app.IO.Out, "Navidrome: %s\n", yesNo(status.Installed))
			if status.Version != "" {
				fmt.Fprintf(app.IO.Out, "Version: %s (minimum %s)\n", status.Version, status.MinimumVersion)
			}
			printProblems(app, status.Problems)
			return nil
		},
	})

	confirm := false
	ensure := &cobra.Command{
		Use:   "ensure",
		Short: "Install or upgrade Navidrome through Homebrew",
		Long:  "This changes your machine. It requires --confirm.",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := navidrome.DependencyChecker{}.Ensure(cmd.Context(), confirm)
			if app.Opts.JSON {
				_ = json.NewEncoder(app.IO.Out).Encode(result)
			} else {
				fmt.Fprintln(app.IO.Out, result.Message)
			}
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			return nil
		},
	}
	ensure.Flags().BoolVar(&confirm, "confirm", false, "Confirm that Homebrew may install or upgrade Navidrome")
	cmd.AddCommand(ensure)
	return cmd
}

func newNavidromeStatusCommand(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report dependency, service, account, and library state",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, false)
			if err != nil {
				return err
			}
			status := manager.Status(cmd.Context())
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(status)
			}
			fmt.Fprintf(app.IO.Out, "Enabled: %s\n", yesNo(status.Enabled))
			fmt.Fprintf(app.IO.Out, "Music: %s\n", status.MusicDir)
			fmt.Fprintf(app.IO.Out, "Service: %s\n", status.Service.State)
			fmt.Fprintf(app.IO.Out, "Transcoder: %s\n",
				orDash(status.Dependency.FFmpegPath))
			if status.Service.LocalURL != "" {
				fmt.Fprintf(app.IO.Out, "Local: %s\n", status.Service.LocalURL)
			}
			if status.Service.LANURL != "" {
				fmt.Fprintf(app.IO.Out, "LAN: %s\n", status.Service.LANURL)
			}
			fmt.Fprintf(app.IO.Out, "Account: %s (password stored: %s)\n",
				orDash(status.Username), yesNo(status.PasswordStored))
			fmt.Fprintf(app.IO.Out, "Reachable: %s\n", yesNo(status.Reachable))
			if status.Reachable {
				fmt.Fprintf(app.IO.Out, "Library: %d tracks (scanning: %s)\n", status.LibraryTracks, yesNo(status.Scanning))
				for _, playlist := range status.ManagedPlaylists {
					fmt.Fprintf(app.IO.Out, "Playlist: %s (%d tracks)\n", playlist.Name, playlist.TrackCount)
				}
			}
			fmt.Fprintf(app.IO.Out, "Phone connected: %s (self-reported)\n", yesNo(status.PhoneConnected))
			fmt.Fprintf(app.IO.Out, "Backups: %d\n", status.BackupCount)
			printProblems(app, status.Problems)
			return nil
		},
	}
}

// newNavidromePhoneCommand records the one setup step that happens on the
// phone. Nothing here can observe Amperfy, so the acknowledgement is stored
// where every surface reads it from: the feature config.
func newNavidromePhoneCommand(app *AppContext) *cobra.Command {
	connected := true
	cmd := &cobra.Command{
		Use:   "phone",
		Short: "Record whether Amperfy on the phone reaches this Mac",
		Long: strings.TrimSpace(`
Mark the phone as connected once Amperfy can reach this Mac over home Wi-Fi.

This is self-reported. UDL cannot observe the phone, and marking it changes
nothing about the server — only the setup checklist.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, true)
			if err != nil {
				return err
			}
			if err := manager.SetPhoneConnected(connected); err != nil {
				return err
			}
			if connected {
				fmt.Fprintln(app.IO.Out, "Marked the phone as connected.")
			} else {
				fmt.Fprintln(app.IO.Out, "Marked the phone as not connected.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&connected, "connected", true, "Whether Amperfy on the phone reaches this Mac")
	return cmd
}

func newNavidromeSetupCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{Use: "setup", Short: "Plan or apply the managed Navidrome service"}

	planFile := ""
	plan := &cobra.Command{
		Use:   "plan",
		Short: "Build a checksummed setup plan without changing anything",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, true)
			if err != nil {
				return err
			}
			built, err := manager.SetupPlan(cmd.Context())
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if strings.TrimSpace(planFile) != "" {
				if err := navidrome.WriteSetupPlan(planFile, built); err != nil {
					return withExitCode(exitcode.RuntimeFailure, err)
				}
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(built)
			}
			printSetupPlan(app, built, planFile)
			return nil
		},
	}
	plan.Flags().StringVar(&planFile, "plan-file", "", "Write the plan to this path for a later apply")
	cmd.AddCommand(plan)

	applyPlanFile := ""
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Apply a previously generated setup plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(applyPlanFile) == "" {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--plan-file is required"))
			}
			manager, err := navidromeManager(app, true)
			if err != nil {
				return err
			}
			stored, err := navidrome.ReadSetupPlan(applyPlanFile)
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, err)
			}
			result, err := manager.ApplySetup(cmd.Context(), stored)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			// A managed definition nobody wrote down cannot be refreshed, so
			// setup registers them. This is append-only and idempotent; the
			// existing Apple Music entries are never touched.
			added, defErr := playlists.WriteNavidromeDefinitions(app.Opts.PlaylistsConfigPath, "")
			if defErr != nil {
				fmt.Fprintf(app.IO.ErrOut, "WARN: managed playlist definitions were not registered: %v\n", defErr)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(result)
			}
			for _, id := range added {
				fmt.Fprintf(app.IO.Out, "registered playlist definition %s\n", id)
			}
			for _, path := range result.CreatedDirectories {
				fmt.Fprintf(app.IO.Out, "created directory %s\n", path)
			}
			for _, path := range result.WrittenFiles {
				fmt.Fprintf(app.IO.Out, "wrote %s\n", path)
			}
			if result.ServiceAction != "" {
				fmt.Fprintf(app.IO.Out, "service: %s\n", result.ServiceAction)
			}
			fmt.Fprintln(app.IO.Out, result.Message)
			return nil
		},
	}
	apply.Flags().StringVar(&applyPlanFile, "plan-file", "", "Path to the plan produced by `udl navidrome setup plan`")
	cmd.AddCommand(apply)
	return cmd
}

func newNavidromePlaylistsCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{Use: "playlists", Short: "Manage the generated smart playlists"}
	cmd.AddCommand(&cobra.Command{
		Use:   "refresh",
		Short: "Regenerate the managed .nsp files and ask Navidrome to re-import them",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, false)
			if err != nil {
				return err
			}
			result, err := manager.RefreshPlaylists(cmd.Context())
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(result)
			}
			for _, item := range result.Generated {
				state := "unchanged"
				if item.Written {
					state = "written"
				}
				fmt.Fprintf(app.IO.Out, "%s\t%s\t%s\n", item.Name, state, item.Path)
			}
			fmt.Fprintf(app.IO.Out, "Scan requested: %s\n", yesNo(result.Scanned))
			printProblems(app, result.Warnings)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "derive-genres",
		Short: "Preview the HARD BOUNCE genre allowlist derived from Apple Music",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, false)
			if err != nil {
				return err
			}
			derivation, err := manager.DeriveGenres(cmd.Context())
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(derivation)
			}
			fmt.Fprintf(app.IO.Out, "Source: %s (%d tracks, %d matched)\n",
				derivation.SourcePlaylist, derivation.SourceTrackCount, derivation.MatchedCount)
			for _, genre := range derivation.Genres {
				fmt.Fprintf(app.IO.Out, "genre\t%s\n", genre)
			}
			fmt.Fprintf(app.IO.Out, "Unmatched: %d\tGenre-less: %d\tOutside library: %d\n",
				len(derivation.UnmatchedPaths), len(derivation.GenrelessPaths), len(derivation.OutsideLibrary))
			if !derivation.AllowlistChanged {
				fmt.Fprintln(app.IO.Out, "The stored allowlist already matches this derivation.")
			}
			return nil
		},
	})

	genres := []string{}
	save := &cobra.Command{
		Use:   "save-genres",
		Short: "Persist an approved HARD BOUNCE genre allowlist and rewrite the playlists",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, false)
			if err != nil {
				return err
			}
			result, err := manager.SaveGenres(cmd.Context(), genres)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(result)
			}
			fmt.Fprintf(app.IO.Out, "Saved %d genres.\n", len(navidrome.NormalizeGenres(genres)))
			printProblems(app, result.Warnings)
			return nil
		},
	}
	save.Flags().StringArrayVar(&genres, "genre", nil, "An approved genre (repeatable)")
	cmd.AddCommand(save)
	return cmd
}

func newNavidromeFavoritesCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{Use: "favorites", Short: "Import Apple Music favorites, and read what is starred on the server"}
	importCmd := &cobra.Command{Use: "import", Short: "Plan or apply the one-way favorite migration"}

	planFile := ""
	plan := &cobra.Command{
		Use:   "plan",
		Short: "Build a checksummed favorite migration plan; Apple Music is only read",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, false)
			if err != nil {
				return err
			}
			built, err := manager.FavoritePlan(cmd.Context())
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if strings.TrimSpace(planFile) != "" {
				if err := navidrome.WriteFavoritePlan(planFile, built); err != nil {
					return withExitCode(exitcode.RuntimeFailure, err)
				}
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(built)
			}
			printFavoritePlan(app, built, planFile)
			return nil
		},
	}
	plan.Flags().StringVar(&planFile, "plan-file", "", "Write the plan to this path for a later apply")
	importCmd.AddCommand(plan)

	applyPlanFile := ""
	apply := &cobra.Command{
		Use:   "apply",
		Short: "Apply a previously generated favorite migration plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(applyPlanFile) == "" {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--plan-file is required"))
			}
			manager, err := navidromeManager(app, false)
			if err != nil {
				return err
			}
			stored, err := navidrome.ReadFavoritePlan(applyPlanFile)
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, err)
			}
			result, err := manager.ApplyFavorites(cmd.Context(), stored)
			if app.Opts.JSON {
				_ = json.NewEncoder(app.IO.Out).Encode(result)
			} else {
				if result.BackupPath != "" {
					fmt.Fprintf(app.IO.Out, "Backup: %s\n", result.BackupPath)
				}
				fmt.Fprintf(app.IO.Out, "Newly starred: %d\n", len(result.NewlyStarred))
				if len(result.Compensated) > 0 {
					fmt.Fprintf(app.IO.Out, "Rolled back: %d\n", len(result.Compensated))
				}
				if result.RecoveryCommand != "" {
					fmt.Fprintf(app.IO.Out, "Recovery: %s\n", result.RecoveryCommand)
				}
				if result.Message != "" {
					fmt.Fprintln(app.IO.Out, result.Message)
				}
			}
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			return nil
		},
	}
	apply.Flags().StringVar(&applyPlanFile, "plan-file", "", "Path to the plan produced by `udl navidrome favorites import plan`")
	importCmd.AddCommand(apply)

	cmd.AddCommand(importCmd)

	list := &cobra.Command{
		Use:   "list",
		Short: "List the tracks starred on the Navidrome server",
		Long: strings.TrimSpace(`
Reads stars straight from the server, so a like made on the phone shows up here
without Apple Music being involved at all. This is the read side of the return
path: refresh the "` + navidrome.SmartPlaylistFavoritesName + `" playlist to
turn these into a snapshot, then sync that snapshot to Rekordbox.

Nothing is written. Apple Music favorites are a separate set and are not shown.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, false)
			if err != nil {
				return err
			}
			songs, err := manager.StarredFavorites(cmd.Context())
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(songs)
			}
			printStarredFavorites(app, songs)
			return nil
		},
	}
	cmd.AddCommand(list)
	return cmd
}

func printStarredFavorites(app *AppContext, songs []navidrome.Song) {
	fmt.Fprintf(app.IO.Out, "Starred on Navidrome: %d\n", len(songs))
	for _, song := range songs {
		artist := song.Artist
		if strings.TrimSpace(artist) == "" {
			artist = "Unknown artist"
		}
		fmt.Fprintf(app.IO.Out, "  %s — %s\n", artist, song.Title)
		if strings.TrimSpace(song.Path) != "" {
			fmt.Fprintf(app.IO.Out, "    %s\n", song.Path)
		}
	}
}

func newNavidromeDatesCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{Use: "dates", Short: "Manage how Date Added is derived"}
	cmd.AddCommand(&cobra.Command{
		Use:   "reconcile",
		Short: "Make Date Added mean the file's real creation time",
		Long: strings.TrimSpace(`
Navidrome records each file's APFS creation time but sorts by the time the
scanner first saw it, so an imported back catalogue comes out in scan order.
This rewrites Date Added from the recorded creation time, which fixes both the
managed smart playlists and a client's own "Date Added" sort.

A database backup is taken first. The operation is idempotent and safe to run
while the server is running.
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, true)
			if err != nil {
				return err
			}
			result, err := manager.ReconcileDateAdded(cmd.Context())
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(result)
			}
			if result.BackupPath != "" {
				fmt.Fprintf(app.IO.Out, "Backup: %s\n", result.BackupPath)
			}
			fmt.Fprintln(app.IO.Out, result.Message)
			return nil
		},
	})
	return cmd
}

func newNavidromeBackupCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{Use: "backup", Short: "Manage Navidrome database backups"}
	cmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "Create a Navidrome database backup now",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, true)
			if err != nil {
				return err
			}
			result, err := manager.CreateBackup(cmd.Context())
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(result)
			}
			fmt.Fprintln(app.IO.Out, result.Message)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List retained Navidrome database backups, newest first",
		RunE: func(cmd *cobra.Command, args []string) error {
			manager, err := navidromeManager(app, true)
			if err != nil {
				return err
			}
			items, err := navidrome.Backups(manager.Resolved)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(items)
			}
			if len(items) == 0 {
				fmt.Fprintln(app.IO.Out, "No backups yet.")
				return nil
			}
			for _, item := range items {
				fmt.Fprintf(app.IO.Out, "%s\t%d bytes\t%s\n",
					item.CreatedAt.Format("2006-01-02 15:04:05 MST"), item.SizeBytes, item.Path)
			}
			return nil
		},
	})
	return cmd
}

// newNavidromeCredentialCommand is intentionally absent: the Navidrome
// password is saved through the existing credentials surface so there is one
// place that writes secrets. This keeps the compile-time reference honest.
var _ = auth.SaveNavidromePassword

func printSetupPlan(app *AppContext, plan navidrome.SetupPlan, planFile string) {
	fmt.Fprintf(app.IO.Out, "Navidrome: %s (%s)\n", orDash(plan.ServerVersion), orDash(plan.BinaryPath))
	fmt.Fprintf(app.IO.Out, "Music: %s\n", plan.MusicDir)
	for _, dir := range plan.Directories {
		if !dir.Exists {
			fmt.Fprintf(app.IO.Out, "create directory\t%s\n", dir.Path)
		}
	}
	for _, file := range plan.Files {
		fmt.Fprintf(app.IO.Out, "%s\t%s\n", file.Action, file.Path)
	}
	fmt.Fprintf(app.IO.Out, "service\t%s\n", plan.Service.Action)
	printProblems(app, plan.Warnings)
	if len(plan.Blockers) > 0 {
		fmt.Fprintln(app.IO.ErrOut, "Blocked:")
		for _, blocker := range plan.Blockers {
			fmt.Fprintf(app.IO.ErrOut, "  - %s\n", blocker)
		}
	}
	if strings.TrimSpace(planFile) != "" {
		fmt.Fprintf(app.IO.Out, "Plan written to %s\n", planFile)
	}
}

func printFavoritePlan(app *AppContext, plan navidrome.FavoritePlan, planFile string) {
	counts := plan.Counts
	fmt.Fprintf(app.IO.Out, "Apple favorites: %d\n", counts.SourceTotal)
	fmt.Fprintf(app.IO.Out, "Will star: %d\tAlready starred: %d\n", counts.Matched, counts.AlreadyStarred)
	fmt.Fprintf(app.IO.Out, "Outside library: %d\tMissing: %d\tAmbiguous: %d\tMetadata-only: %d\n",
		counts.OutsideLibrary, counts.Missing, counts.Ambiguous, counts.MetadataOnly)
	printProblems(app, plan.Warnings)
	if len(plan.Blockers) > 0 {
		fmt.Fprintln(app.IO.ErrOut, "Blocked:")
		for _, blocker := range plan.Blockers {
			fmt.Fprintf(app.IO.ErrOut, "  - %s\n", blocker)
		}
	}
	if strings.TrimSpace(planFile) != "" {
		fmt.Fprintf(app.IO.Out, "Plan written to %s\n", planFile)
	}
}

func printProblems(app *AppContext, problems []string) {
	for _, problem := range problems {
		fmt.Fprintf(app.IO.ErrOut, "WARN: %s\n", problem)
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

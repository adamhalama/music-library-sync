package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/spf13/cobra"
)

func newPlaylistCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlist",
		Short: "Manage reusable playlist snapshots",
	}
	cmd.AddCommand(newPlaylistListCommand(app))
	cmd.AddCommand(newPlaylistShowCommand(app))
	cmd.AddCommand(newPlaylistRefreshCommand(app))
	cmd.AddCommand(newPlaylistConfigCommand(app))
	return cmd
}

func newPlaylistListCommand(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List configured standalone playlists",
		RunE: func(cmd *cobra.Command, args []string) error {
			main, playlistCfg, err := loadPlaylistConfigs(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			type row struct {
				Definition playlists.Definition `json:"definition"`
				Snapshot   *playlists.Snapshot  `json:"snapshot,omitempty"`
				Error      string               `json:"snapshot_error,omitempty"`
			}
			rows := make([]row, 0, len(playlistCfg.Playlists))
			for _, definition := range playlistCfg.Playlists {
				item := row{Definition: definition}
				snapshot, loadErr := playlists.LoadSnapshot(main.Defaults.StateDir, definition.ID)
				if loadErr == nil {
					item.Snapshot = &snapshot
				} else if !errors.Is(loadErr, os.ErrNotExist) {
					item.Error = loadErr.Error()
				}
				rows = append(rows, item)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(app.IO.Out, "No standalone playlists configured.")
				return nil
			}
			for _, item := range rows {
				status := "not refreshed"
				if item.Snapshot != nil {
					status = fmt.Sprintf("%d tracks, refreshed %s", len(item.Snapshot.Tracks), item.Snapshot.RefreshedAt.Format("2006-01-02 15:04 MST"))
				} else if item.Error != "" {
					status = "snapshot error: " + item.Error
				}
				fmt.Fprintf(app.IO.Out, "%s\t%s\t%s\n", item.Definition.ID, item.Definition.Name, status)
			}
			return nil
		},
	}
}

func newPlaylistShowCommand(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "show <playlist-id>",
		Short: "Show a saved playlist snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			main, playlistCfg, err := loadPlaylistConfigs(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			definition, ok := playlistCfg.Definition(args[0])
			if !ok {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("playlist %q is not configured", args[0]))
			}
			snapshot, err := playlists.LoadSnapshot(main.Defaults.StateDir, definition.ID)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(snapshot)
			}
			fmt.Fprintf(app.IO.Out, "%s (%s)\n", snapshot.Name, snapshot.PlaylistID)
			fmt.Fprintf(app.IO.Out, "Source: %s / %s\n", snapshot.Provider, snapshot.ProviderPlaylist)
			fmt.Fprintf(app.IO.Out, "Refreshed: %s\n", snapshot.RefreshedAt.Format("2006-01-02 15:04:05 MST"))
			fmt.Fprintf(app.IO.Out, "Tracks: %d\n", len(snapshot.Tracks))
			for _, track := range snapshot.Tracks {
				missing := ""
				if track.MissingLocal {
					missing = " [missing]"
				}
				fmt.Fprintf(app.IO.Out, "%d\t%s — %s\t%s%s\n", track.Index, track.Artist, track.Title, track.Path, missing)
			}
			return nil
		},
	}
}

func newPlaylistRefreshCommand(app *AppContext) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh <playlist-id>",
		Short: "Explicitly refresh a playlist snapshot from its provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			main, playlistCfg, err := loadPlaylistConfigs(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			definition, ok := playlistCfg.Definition(args[0])
			if !ok {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("playlist %q is not configured", args[0]))
			}
			result, err := (playlists.Service{}).Refresh(cmd.Context(), main, definition)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(result)
			}
			fmt.Fprintf(app.IO.Out, "Refreshed %s: %d tracks\n", result.Snapshot.Name, len(result.Snapshot.Tracks))
			fmt.Fprintf(app.IO.Out, "Changes: +%d -%d unchanged=%d\n", result.Changes.Added, result.Changes.Removed, result.Changes.Kept)
			fmt.Fprintf(app.IO.Out, "Snapshot: %s\n", result.Path)
			return nil
		},
	}
}

func newPlaylistConfigCommand(app *AppContext) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect standalone playlist config"}
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Show the standalone playlist config write path",
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := playlists.ResolveWritePath(app.Opts.PlaylistsConfigPath, "")
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, err)
			}
			fmt.Fprintln(app.IO.Out, path)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show merged standalone playlist config",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cfg, err := loadPlaylistConfigs(app)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			if app.Opts.JSON {
				return json.NewEncoder(app.IO.Out).Encode(cfg)
			}
			payload, err := playlists.Marshal(cfg)
			if err != nil {
				return withExitCode(exitcode.InvalidConfig, err)
			}
			fmt.Fprint(app.IO.Out, string(payload))
			return nil
		},
	})
	return cmd
}

func loadPlaylistConfigs(app *AppContext) (mainConfig config.Config, playlistConfig playlists.Config, err error) {
	mainConfig, err = loadConfig(app)
	if err != nil {
		return mainConfig, playlistConfig, err
	}
	playlistConfig, err = playlists.Load(playlists.LoadOptions{ExplicitPath: strings.TrimSpace(app.Opts.PlaylistsConfigPath)})
	return mainConfig, playlistConfig, err
}

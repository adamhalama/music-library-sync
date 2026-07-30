package playlistsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
	"github.com/jaa/update-downloads/internal/rekordbox/music"
	"github.com/jaa/update-downloads/internal/rekordbox/syncconfig"
	"golang.org/x/text/unicode/norm"
)

const (
	DefaultMusicPlaylist     = "Favourites"
	DefaultRekordboxPlaylist = "fav_imports"
	DefaultRekordboxDBDir    = "~/Library/Pioneer/rekordbox"
	DefaultPythonBin         = "python3"
	DefaultBackupDir         = "~/Music/rb-library-export"
	DefaultMode              = "mirror"
	PlanVersion              = "1"
	PlanVersionFolder        = "2"
)

type Options struct {
	JobID               string
	MusicPlaylist       string
	MusicPlaylistID     string
	RekordboxPlaylist   string
	RekordboxPlaylistID string
	RekordboxDBDir      string
	PythonBin           string
	PythonPath          string
	BackupDir           string
	Mode                string
	CreatePlaylist      bool
	CreatePlaylistSet   bool
	OutPath             string
	MappingID           string
}

type ResolvedOptions struct {
	JobID               string `json:"job_id,omitempty"`
	MusicPlaylist       string `json:"music_playlist"`
	MusicPlaylistID     string `json:"music_playlist_id,omitempty"`
	RekordboxPlaylist   string `json:"rekordbox_playlist"`
	RekordboxPlaylistID string `json:"rekordbox_playlist_id,omitempty"`
	RekordboxDBDir      string `json:"rekordbox_db_dir"`
	PythonBin           string `json:"python_bin"`
	PythonPath          string `json:"-"`
	BackupDir           string `json:"backup_dir"`
	Mode                string `json:"mode"`
	CreatePlaylist      bool   `json:"create_playlist"`
	OutPath             string `json:"out_path,omitempty"`
	MappingID           string `json:"mapping_id,omitempty"`
}

type Plan struct {
	Version           string                `json:"version"`
	GeneratedAt       string                `json:"generated_at"`
	JobID             string                `json:"job_id,omitempty"`
	Mode              string                `json:"mode"`
	RekordboxDBDir    string                `json:"rekordbox_db_dir"`
	BackupDir         string                `json:"backup_dir"`
	MusicFolder       PlanMusicFolder       `json:"music_folder,omitempty"`
	RekordboxFolder   PlanRekordboxFolder   `json:"rekordbox_folder,omitempty"`
	MusicPlaylist     PlanMusicPlaylist     `json:"music_playlist"`
	RekordboxPlaylist PlanRekordboxPlaylist `json:"rekordbox_playlist"`
	Summary           PlanSummary           `json:"summary"`
	Rows              []PlanRow             `json:"rows"`
	Operations        []PlanOperation       `json:"operations,omitempty"`
	RemovalContentIDs []string              `json:"removal_content_ids"`
	FinalContentIDs   []string              `json:"final_content_ids"`
	Preconditions     PlanPreconditions     `json:"preconditions"`
	Warnings          []string              `json:"warnings,omitempty"`
	ChecksumSHA256    string                `json:"checksum_sha256"`
}

type PlanMusicFolder struct {
	Name         string `json:"name,omitempty"`
	PersistentID string `json:"persistent_id,omitempty"`
	ChildCount   int    `json:"child_count,omitempty"`
}

type PlanRekordboxFolder struct {
	ID            string `json:"id,omitempty"`
	Name          string `json:"name,omitempty"`
	CurrentCount  int    `json:"current_count"`
	CreatePlanned bool   `json:"create_planned"`
}

type PlanMusicPlaylist struct {
	Name         string `json:"name"`
	PersistentID string `json:"persistent_id,omitempty"`
	Smart        bool   `json:"smart"`
	TrackCount   int    `json:"track_count"`
}

type PlanOperation struct {
	ID                string                `json:"id"`
	MusicPlaylist     PlanMusicPlaylist     `json:"music_playlist"`
	RekordboxPlaylist PlanRekordboxPlaylist `json:"rekordbox_playlist"`
	Summary           PlanSummary           `json:"summary"`
	Rows              []PlanRow             `json:"rows"`
	RemovalContentIDs []string              `json:"removal_content_ids"`
	FinalContentIDs   []string              `json:"final_content_ids"`
	Preconditions     PlanPreconditions     `json:"preconditions"`
	Warnings          []string              `json:"warnings,omitempty"`
}

type PlanRekordboxPlaylist struct {
	ID            string `json:"id,omitempty"`
	Name          string `json:"name"`
	Attribute     int    `json:"attribute"`
	CurrentCount  int    `json:"current_count"`
	CreatePlanned bool   `json:"create_planned"`
}

type PlanSummary struct {
	MusicTotal         int `json:"music_total"`
	MatchedByPath      int `json:"matched_by_path"`
	MissingInRekordbox int `json:"missing_in_rekordbox"`
	AmbiguousInRB      int `json:"ambiguous_in_rekordbox"`
	// DuplicateInPlaylist stays omitempty so plans written before this field
	// existed still verify: VerifyPlanChecksum re-marshals the parsed struct,
	// and an always-emitted new field would break every stored plan file.
	DuplicateInPlaylist int `json:"duplicate_in_playlist,omitempty"`
	CurrentTargetCount  int `json:"current_target_count"`
	FinalTargetCount    int `json:"final_target_count"`
	WillAdd             int `json:"will_add"`
	WillRemove          int `json:"will_remove"`
	WillMove            int `json:"will_move"`
	WillKeep            int `json:"will_keep"`
}

type PlanRow struct {
	MusicIndex         int    `json:"music_index"`
	MusicPersistentID  string `json:"music_persistent_id"`
	MusicDatabaseID    string `json:"music_database_id"`
	Artist             string `json:"artist"`
	Title              string `json:"title"`
	Album              string `json:"album"`
	Duration           string `json:"duration"`
	Path               string `json:"path"`
	NormalizedPath     string `json:"normalized_path"`
	RekordboxContentID string `json:"rekordbox_content_id,omitempty"`
	RekordboxTitle     string `json:"rekordbox_title,omitempty"`
	MatchStatus        string `json:"match_status"`
	Action             string `json:"action"`
}

type PlanPreconditions struct {
	TargetPlaylistID          string                       `json:"target_playlist_id,omitempty"`
	TargetPlaylistName        string                       `json:"target_playlist_name"`
	TargetParentID            string                       `json:"target_parent_id,omitempty"`
	TargetPlaylistMissing     bool                         `json:"target_playlist_missing"`
	ExpectedCurrentContentIDs []string                     `json:"expected_current_content_ids"`
	MatchedContent            []MatchedContentPrecondition `json:"matched_content"`
}

type MatchedContentPrecondition struct {
	ContentID  string `json:"content_id"`
	Title      string `json:"title"`
	FolderPath string `json:"folder_path"`
}

type BuildRequest struct {
	Options       ResolvedOptions
	MusicPlaylist music.Playlist
	MusicTracks   []music.Track
	Inspect       bridge.InspectResponse
}

type FolderMusicPlaylistTracks struct {
	Playlist music.Playlist
	Tracks   []music.Track
}

type FolderBuildRequest struct {
	Options       ResolvedOptions
	Mapping       syncconfig.FolderMapping
	MusicFolder   music.Playlist
	MusicChildren []FolderMusicPlaylistTracks
	Inspect       bridge.InspectResponse
}

type operationBuildRequest struct {
	MusicPlaylist  music.Playlist
	MusicTracks    []music.Track
	TargetPlaylist bridge.Playlist
	TargetMissing  bool
	// ContentByPath is the shared Rekordbox content index keyed by normalized
	// path, built once per plan by indexContentsByPath.
	ContentByPath map[string][]bridge.Content
	// FallbackTargetName names the target when the resolved playlist has no
	// name yet, which happens when the plan will create it.
	FallbackTargetName string
	// WarningPrefix is prepended to every warning, so folder plans can name the
	// playlist a warning came from while single-playlist plans stay unprefixed.
	WarningPrefix string
}

func ResolveOptions(cfg config.Config, opts Options) (ResolvedOptions, error) {
	resolved := ResolvedOptions{
		MusicPlaylist:     DefaultMusicPlaylist,
		RekordboxPlaylist: DefaultRekordboxPlaylist,
		RekordboxDBDir:    DefaultRekordboxDBDir,
		PythonBin:         DefaultPythonBin,
		BackupDir:         DefaultBackupDir,
		Mode:              DefaultMode,
		CreatePlaylist:    true,
	}

	if rb := cfg.Rekordbox; rb != nil {
		if strings.TrimSpace(rb.DBDir) != "" {
			resolved.RekordboxDBDir = rb.DBDir
		}
		if strings.TrimSpace(rb.PythonBin) != "" {
			resolved.PythonBin = rb.PythonBin
		}
		if strings.TrimSpace(rb.PythonPath) != "" {
			resolved.PythonPath = rb.PythonPath
		}
		if strings.TrimSpace(rb.BackupDir) != "" {
			resolved.BackupDir = rb.BackupDir
		}
		if strings.TrimSpace(opts.JobID) != "" {
			job, ok := findJob(rb.PlaylistSync.Jobs, opts.JobID)
			if !ok {
				return ResolvedOptions{}, fmt.Errorf("rekordbox playlist sync job %q not found", opts.JobID)
			}
			resolved.JobID = job.ID
			applyJob(&resolved, job)
		}
	}

	if value := strings.TrimSpace(opts.MusicPlaylist); value != "" {
		resolved.MusicPlaylist = value
	}
	if value := strings.TrimSpace(opts.MusicPlaylistID); value != "" {
		resolved.MusicPlaylistID = value
	}
	if value := strings.TrimSpace(opts.RekordboxPlaylist); value != "" {
		resolved.RekordboxPlaylist = value
	}
	if value := strings.TrimSpace(opts.RekordboxPlaylistID); value != "" {
		resolved.RekordboxPlaylistID = value
	}
	if value := strings.TrimSpace(opts.RekordboxDBDir); value != "" {
		resolved.RekordboxDBDir = value
	}
	if value := strings.TrimSpace(opts.PythonBin); value != "" {
		resolved.PythonBin = value
	}
	if value := strings.TrimSpace(opts.PythonPath); value != "" {
		resolved.PythonPath = value
	}
	if value := strings.TrimSpace(opts.BackupDir); value != "" {
		resolved.BackupDir = value
	}
	if value := strings.TrimSpace(opts.Mode); value != "" {
		resolved.Mode = value
	}
	if opts.CreatePlaylistSet {
		resolved.CreatePlaylist = opts.CreatePlaylist
	}
	if value := strings.TrimSpace(opts.OutPath); value != "" {
		resolved.OutPath = value
	}
	if value := strings.TrimSpace(opts.MappingID); value != "" {
		resolved.MappingID = value
	}

	if resolved.Mode != DefaultMode {
		return ResolvedOptions{}, fmt.Errorf("unsupported playlist-sync mode %q", resolved.Mode)
	}

	var err error
	resolved.RekordboxDBDir, err = config.ExpandPath(resolved.RekordboxDBDir)
	if err != nil {
		return ResolvedOptions{}, fmt.Errorf("resolve rekordbox db dir: %w", err)
	}
	resolved.BackupDir, err = config.ExpandPath(resolved.BackupDir)
	if err != nil {
		return ResolvedOptions{}, fmt.Errorf("resolve rekordbox backup dir: %w", err)
	}
	if resolved.OutPath != "" {
		resolved.OutPath, err = config.ExpandPath(resolved.OutPath)
		if err != nil {
			return ResolvedOptions{}, fmt.Errorf("resolve plan output path: %w", err)
		}
	}
	return resolved, nil
}

func DefaultOutPath(cfg config.Config, opts ResolvedOptions, now time.Time) (string, error) {
	if strings.TrimSpace(opts.OutPath) != "" {
		return opts.OutPath, nil
	}
	stateDir, err := config.ExpandPath(cfg.Defaults.StateDir)
	if err != nil {
		return "", err
	}
	base := safeFilename(firstNonEmpty(opts.JobID, opts.RekordboxPlaylist, DefaultRekordboxPlaylist))
	name := fmt.Sprintf("%s-%s.plan.json", base, now.Format("20060102-150405"))
	return filepath.Join(stateDir, "rekordbox", "playlist-sync", name), nil
}

func BuildPlan(req BuildRequest, now time.Time) (Plan, error) {
	if req.Options.Mode != DefaultMode {
		return Plan{}, fmt.Errorf("unsupported playlist-sync mode %q", req.Options.Mode)
	}

	target, targetMissing, err := selectRekordboxPlaylist(req.Inspect.Playlists, req.Options.RekordboxPlaylist, req.Options.RekordboxPlaylistID, req.Options.CreatePlaylist)
	if err != nil {
		return Plan{}, err
	}

	op, err := buildOperation(operationBuildRequest{
		MusicPlaylist:      req.MusicPlaylist,
		MusicTracks:        req.MusicTracks,
		TargetPlaylist:     target,
		TargetMissing:      targetMissing,
		ContentByPath:      indexContentsByPath(req.Inspect.Contents),
		FallbackTargetName: req.Options.RekordboxPlaylist,
	})
	if err != nil {
		return Plan{}, err
	}

	plan := Plan{
		Version:           PlanVersion,
		GeneratedAt:       now.UTC().Format(time.RFC3339),
		JobID:             req.Options.JobID,
		Mode:              req.Options.Mode,
		RekordboxDBDir:    req.Options.RekordboxDBDir,
		BackupDir:         req.Options.BackupDir,
		MusicPlaylist:     op.MusicPlaylist,
		RekordboxPlaylist: op.RekordboxPlaylist,
		Summary:           op.Summary,
		Rows:              op.Rows,
		RemovalContentIDs: op.RemovalContentIDs,
		FinalContentIDs:   op.FinalContentIDs,
		Preconditions:     op.Preconditions,
		Warnings:          op.Warnings,
	}
	if err := SignPlan(&plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func BuildFolderPlan(req FolderBuildRequest, now time.Time) (Plan, error) {
	if req.Options.Mode != DefaultMode {
		return Plan{}, fmt.Errorf("unsupported playlist-sync mode %q", req.Options.Mode)
	}
	createFolder := boolPtrValue(req.Mapping.CreateFolders, true)
	createPlaylist := boolPtrValue(req.Mapping.CreatePlaylists, true)
	targetFolder, targetMissing, err := selectRekordboxFolder(req.Inspect.Playlists, req.Mapping.RekordboxFolder, req.Mapping.RekordboxFolderID, createFolder)
	if err != nil {
		return Plan{}, err
	}
	folderID := targetFolder.ID
	ops := []PlanOperation{}
	warnings := []string{}
	aggregate := PlanSummary{}
	contentByPath := indexContentsByPath(req.Inspect.Contents)
	for _, child := range req.MusicChildren {
		targetName := child.Playlist.Name
		if mapped := strings.TrimSpace(req.Mapping.PlaylistNameMap[child.Playlist.Name]); mapped != "" {
			targetName = mapped
		}
		targetPlaylist, childMissing, err := selectRekordboxPlaylistInParent(req.Inspect.Playlists, targetName, "", folderID, createPlaylist)
		if err != nil {
			return Plan{}, err
		}
		if targetMissing {
			targetPlaylist = bridge.Playlist{Name: targetName, ParentID: folderID}
			childMissing = true
		}
		op, err := buildOperation(operationBuildRequest{
			MusicPlaylist:      child.Playlist,
			MusicTracks:        child.Tracks,
			TargetPlaylist:     targetPlaylist,
			TargetMissing:      childMissing,
			ContentByPath:      contentByPath,
			FallbackTargetName: child.Playlist.Name,
			WarningPrefix:      child.Playlist.Name + ": ",
		})
		if err != nil {
			return Plan{}, err
		}
		op.ID = safeFilename(child.Playlist.Name)
		op.Preconditions.TargetParentID = folderID
		ops = append(ops, op)
		aggregate = addSummaries(aggregate, op.Summary)
		warnings = append(warnings, op.Warnings...)
	}
	plan := Plan{
		Version:        PlanVersionFolder,
		GeneratedAt:    now.UTC().Format(time.RFC3339),
		JobID:          req.Options.MappingID,
		Mode:           req.Options.Mode,
		RekordboxDBDir: req.Options.RekordboxDBDir,
		BackupDir:      req.Options.BackupDir,
		MusicFolder: PlanMusicFolder{
			Name:         req.MusicFolder.Name,
			PersistentID: req.MusicFolder.PersistentID,
			ChildCount:   len(req.MusicChildren),
		},
		RekordboxFolder: PlanRekordboxFolder{
			ID:            targetFolder.ID,
			Name:          firstNonEmpty(targetFolder.Name, req.Mapping.RekordboxFolder),
			CurrentCount:  countChildren(req.Inspect.Playlists, targetFolder.ID),
			CreatePlanned: targetMissing,
		},
		Summary:    aggregate,
		Operations: ops,
		Warnings:   uniqueStrings(warnings),
		Preconditions: PlanPreconditions{
			TargetPlaylistID:      targetFolder.ID,
			TargetPlaylistName:    firstNonEmpty(targetFolder.Name, req.Mapping.RekordboxFolder),
			TargetPlaylistMissing: targetMissing,
		},
	}
	if err := SignPlan(&plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func buildOperation(req operationBuildRequest) (PlanOperation, error) {
	currentIDs := []string{}
	if !req.TargetMissing {
		currentIDs = append(currentIDs, req.TargetPlaylist.ContentIDs...)
	}
	currentPos := map[string]int{}
	for idx, id := range currentIDs {
		if _, exists := currentPos[id]; !exists {
			currentPos[id] = idx
		}
	}
	rows := make([]PlanRow, 0, len(req.MusicTracks))
	finalIDs := []string{}
	matchedPreconditions := []MatchedContentPrecondition{}
	claimedContent := map[string]struct{}{}
	matchedByPath := 0
	missing := 0
	ambiguous := 0
	duplicate := 0
	willAdd := 0
	willMove := 0
	willKeep := 0
	for _, track := range req.MusicTracks {
		normalizedPath := NormalizePath(track.Path)
		row := PlanRow{
			MusicIndex:        track.Index,
			MusicPersistentID: track.PersistentID,
			MusicDatabaseID:   track.DatabaseID,
			Artist:            track.Artist,
			Title:             track.Title,
			Album:             track.Album,
			Duration:          track.Duration,
			Path:              track.Path,
			NormalizedPath:    normalizedPath,
			MatchStatus:       "missing",
			Action:            "skip",
		}
		candidates := req.ContentByPath[normalizedPath]
		switch len(candidates) {
		case 0:
			missing++
		case 1:
			content := candidates[0]
			row.RekordboxContentID = content.ID
			row.RekordboxTitle = content.Title
			if _, claimed := claimedContent[content.ID]; claimed {
				// The same Music track appears twice in the source playlist.
				// Mirroring it would put one content ID in the target playlist
				// twice, so the row is reported and the plan refuses to apply.
				duplicate++
				row.MatchStatus = "duplicate_path"
				row.Action = "skip"
			} else {
				claimedContent[content.ID] = struct{}{}
				matchedByPath++
				row.MatchStatus = "matched_path"
				finalIDs = append(finalIDs, content.ID)
				matchedPreconditions = append(matchedPreconditions, MatchedContentPrecondition{
					ContentID:  content.ID,
					Title:      content.Title,
					FolderPath: content.FolderPath,
				})
				if pos, exists := currentPos[content.ID]; !exists {
					row.Action = "add"
					willAdd++
				} else if pos == len(finalIDs)-1 {
					row.Action = "keep"
					willKeep++
				} else {
					row.Action = "move"
					willMove++
				}
			}
		default:
			ambiguous++
			row.MatchStatus = "ambiguous_path"
			row.Action = "skip"
		}
		rows = append(rows, row)
	}
	finalSet := map[string]struct{}{}
	for _, id := range finalIDs {
		finalSet[id] = struct{}{}
	}
	removals := []string{}
	for _, id := range currentIDs {
		if _, keep := finalSet[id]; !keep {
			removals = append(removals, id)
		}
	}
	op := PlanOperation{
		MusicPlaylist: PlanMusicPlaylist{
			Name:         req.MusicPlaylist.Name,
			PersistentID: req.MusicPlaylist.PersistentID,
			Smart:        req.MusicPlaylist.Smart,
			TrackCount:   req.MusicPlaylist.TrackCount,
		},
		RekordboxPlaylist: PlanRekordboxPlaylist{
			ID:            req.TargetPlaylist.ID,
			Name:          firstNonEmpty(req.TargetPlaylist.Name, req.FallbackTargetName),
			Attribute:     req.TargetPlaylist.Attribute,
			CurrentCount:  len(currentIDs),
			CreatePlanned: req.TargetMissing,
		},
		Summary: PlanSummary{
			MusicTotal:          len(req.MusicTracks),
			MatchedByPath:       matchedByPath,
			MissingInRekordbox:  missing,
			AmbiguousInRB:       ambiguous,
			DuplicateInPlaylist: duplicate,
			CurrentTargetCount:  len(currentIDs),
			FinalTargetCount:    len(finalIDs),
			WillAdd:             willAdd,
			WillRemove:          len(removals),
			WillMove:            willMove,
			WillKeep:            willKeep,
		},
		Rows:              rows,
		RemovalContentIDs: removals,
		FinalContentIDs:   finalIDs,
		Preconditions: PlanPreconditions{
			TargetPlaylistID:          req.TargetPlaylist.ID,
			TargetPlaylistName:        firstNonEmpty(req.TargetPlaylist.Name, req.FallbackTargetName),
			TargetPlaylistMissing:     req.TargetMissing,
			ExpectedCurrentContentIDs: currentIDs,
			MatchedContent:            matchedPreconditions,
		},
	}
	if missing > 0 {
		op.Warnings = append(op.Warnings, req.WarningPrefix+fmt.Sprintf("%d Music track(s) are missing from Rekordbox and will not be imported in v1", missing))
	}
	if ambiguous > 0 {
		op.Warnings = append(op.Warnings, req.WarningPrefix+fmt.Sprintf("%d Music track(s) matched multiple Rekordbox rows by path", ambiguous))
	}
	if duplicate > 0 {
		op.Warnings = append(op.Warnings, req.WarningPrefix+fmt.Sprintf("%d Music track(s) appear more than once; v1 refuses to mirror duplicates", duplicate))
	}
	return op, nil
}

func indexContentsByPath(contents []bridge.Content) map[string][]bridge.Content {
	index := make(map[string][]bridge.Content, len(contents))
	for _, content := range contents {
		normalized := NormalizePath(content.FolderPath)
		if normalized != "" {
			index[normalized] = append(index[normalized], content)
		}
	}
	return index
}

func SelectMusicPlaylist(playlists []music.Playlist, name, persistentID string) (music.Playlist, error) {
	if strings.TrimSpace(persistentID) != "" {
		for _, playlist := range playlists {
			if playlist.PersistentID == persistentID {
				return playlist, nil
			}
		}
		return music.Playlist{}, fmt.Errorf("Music playlist with persistent ID %q not found", persistentID)
	}

	matches := []music.Playlist{}
	for _, playlist := range playlists {
		if playlist.Name == name {
			matches = append(matches, playlist)
		}
	}
	if len(matches) == 0 {
		return music.Playlist{}, fmt.Errorf("Music playlist %q not found", name)
	}
	if len(matches) > 1 {
		return music.Playlist{}, fmt.Errorf("multiple Music playlists named %q; use --music-playlist-id", name)
	}
	return matches[0], nil
}

func SelectMusicFolderChildren(playlists []music.Playlist, mapping syncconfig.FolderMapping) (music.Playlist, []music.Playlist, error) {
	folder, err := selectMusicFolder(playlists, mapping.MusicFolder, mapping.MusicFolderID)
	if err != nil {
		return music.Playlist{}, nil, err
	}
	include := stringSet(mapping.IncludePlaylists)
	exclude := stringSet(mapping.ExcludePlaylists)
	children := []music.Playlist{}
	for _, playlist := range playlists {
		if playlist.Folder {
			continue
		}
		parentMatches := false
		if folder.PersistentID != "" && playlist.ParentID == folder.PersistentID {
			parentMatches = true
		}
		if !parentMatches && folder.Name != "" && playlist.ParentName == folder.Name {
			parentMatches = true
		}
		if !parentMatches {
			continue
		}
		if len(include) > 0 {
			if _, ok := include[playlist.Name]; !ok {
				continue
			}
		}
		if _, skip := exclude[playlist.Name]; skip {
			continue
		}
		children = append(children, playlist)
	}
	sort.SliceStable(children, func(i, j int) bool {
		return children[i].Name < children[j].Name
	})
	if len(children) == 0 {
		return music.Playlist{}, nil, fmt.Errorf("Music folder %q has no direct child playlists to sync", folder.Name)
	}
	return folder, children, nil
}

func selectMusicFolder(playlists []music.Playlist, name, persistentID string) (music.Playlist, error) {
	if strings.TrimSpace(persistentID) != "" {
		for _, playlist := range playlists {
			if playlist.PersistentID == persistentID {
				if !playlist.Folder {
					return music.Playlist{}, fmt.Errorf("Music playlist %q is not a folder", playlist.Name)
				}
				return playlist, nil
			}
		}
		return music.Playlist{}, fmt.Errorf("Music folder with persistent ID %q not found", persistentID)
	}
	matches := []music.Playlist{}
	for _, playlist := range playlists {
		if playlist.Folder && playlist.Name == name {
			matches = append(matches, playlist)
		}
	}
	if len(matches) == 0 {
		return music.Playlist{}, fmt.Errorf("Music folder %q not found", name)
	}
	if len(matches) > 1 {
		return music.Playlist{}, fmt.Errorf("multiple Music folders named %q; use music_folder_id", name)
	}
	return matches[0], nil
}

func stringSet(values []string) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result[strings.TrimSpace(value)] = struct{}{}
		}
	}
	return result
}

func ValidatePlanForApply(plan Plan) error {
	if err := VerifyPlanChecksum(plan); err != nil {
		return err
	}
	if plan.Version != PlanVersion && plan.Version != PlanVersionFolder {
		return fmt.Errorf("unsupported plan version %q", plan.Version)
	}
	if plan.Mode != DefaultMode {
		return fmt.Errorf("unsupported plan mode %q", plan.Mode)
	}
	if plan.Version == PlanVersionFolder {
		if len(plan.Operations) == 0 {
			return fmt.Errorf("folder plan has no playlist operations")
		}
		for _, op := range plan.Operations {
			if op.Summary.MissingInRekordbox > 0 {
				return fmt.Errorf("playlist %q has %d missing Rekordbox tracks; v1 refuses partial mirror apply", op.MusicPlaylist.Name, op.Summary.MissingInRekordbox)
			}
			if op.Summary.AmbiguousInRB > 0 {
				return fmt.Errorf("playlist %q has %d ambiguous Rekordbox path matches", op.MusicPlaylist.Name, op.Summary.AmbiguousInRB)
			}
			if op.Summary.DuplicateInPlaylist > 0 {
				return fmt.Errorf("playlist %q has %d duplicate track(s); v1 refuses to mirror duplicates", op.MusicPlaylist.Name, op.Summary.DuplicateInPlaylist)
			}
			if duplicate, ok := firstDuplicate(op.FinalContentIDs); ok {
				return fmt.Errorf("playlist %q lists Rekordbox content ID %q more than once", op.MusicPlaylist.Name, duplicate)
			}
			if len(op.FinalContentIDs) != op.Summary.FinalTargetCount {
				return fmt.Errorf("playlist %q final content count does not match summary", op.MusicPlaylist.Name)
			}
		}
		return nil
	}
	if plan.Summary.MissingInRekordbox > 0 {
		return fmt.Errorf("plan has %d missing Rekordbox tracks; v1 refuses partial mirror apply", plan.Summary.MissingInRekordbox)
	}
	if plan.Summary.AmbiguousInRB > 0 {
		return fmt.Errorf("plan has %d ambiguous Rekordbox path matches", plan.Summary.AmbiguousInRB)
	}
	if plan.Summary.DuplicateInPlaylist > 0 {
		return fmt.Errorf("plan has %d duplicate track(s); v1 refuses to mirror duplicates", plan.Summary.DuplicateInPlaylist)
	}
	if duplicate, ok := firstDuplicate(plan.FinalContentIDs); ok {
		return fmt.Errorf("plan lists Rekordbox content ID %q more than once", duplicate)
	}
	if len(plan.FinalContentIDs) != plan.Summary.FinalTargetCount {
		return fmt.Errorf("plan final content count does not match summary")
	}
	return nil
}

// firstDuplicate guards the apply path against a plan whose final content IDs
// repeat, independently of the summary counters a hand-edited plan could zero.
func firstDuplicate(ids []string) (string, bool) {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, exists := seen[id]; exists {
			return id, true
		}
		seen[id] = struct{}{}
	}
	return "", false
}

func ValidatePreconditions(plan Plan, inspect bridge.InspectResponse) error {
	if plan.Version == PlanVersionFolder {
		if plan.Preconditions.TargetPlaylistMissing {
			if _, missing, err := selectRekordboxFolder(inspect.Playlists, plan.RekordboxFolder.Name, plan.RekordboxFolder.ID, plan.RekordboxFolder.CreatePlanned); err != nil {
				return err
			} else if !missing {
				return fmt.Errorf("target folder %q now exists; regenerate the plan", plan.RekordboxFolder.Name)
			}
		} else {
			folder, missing, err := selectRekordboxFolder(inspect.Playlists, plan.RekordboxFolder.Name, plan.RekordboxFolder.ID, false)
			if err != nil {
				return err
			}
			if missing {
				return fmt.Errorf("target folder %q is missing", plan.RekordboxFolder.Name)
			}
			if folder.ID != plan.RekordboxFolder.ID {
				return fmt.Errorf("target folder ID changed from %q to %q", plan.RekordboxFolder.ID, folder.ID)
			}
		}
		for _, op := range plan.Operations {
			if err := validateOperationPreconditions(op, inspect); err != nil {
				return err
			}
		}
		return nil
	}
	target, missing, err := selectRekordboxPlaylist(inspect.Playlists, plan.RekordboxPlaylist.Name, plan.RekordboxPlaylist.ID, plan.RekordboxPlaylist.CreatePlanned)
	if err != nil {
		return err
	}
	if plan.Preconditions.TargetPlaylistMissing {
		if !missing {
			return fmt.Errorf("target playlist %q now exists; regenerate the plan", plan.Preconditions.TargetPlaylistName)
		}
	} else {
		if missing {
			return fmt.Errorf("target playlist %q is missing", plan.Preconditions.TargetPlaylistName)
		}
		if target.ID != plan.Preconditions.TargetPlaylistID {
			return fmt.Errorf("target playlist ID changed from %q to %q", plan.Preconditions.TargetPlaylistID, target.ID)
		}
		if !SameStrings(target.ContentIDs, plan.Preconditions.ExpectedCurrentContentIDs) {
			return fmt.Errorf("target playlist membership changed since plan generation")
		}
	}

	return validateMatchedContent(plan.Preconditions.MatchedContent, inspect)
}

// validateMatchedContent re-checks that every content row the plan matched still
// exists at the same path, so a library change between plan and apply is caught
// before anything is written.
func validateMatchedContent(matched []MatchedContentPrecondition, inspect bridge.InspectResponse) error {
	contentsByID := make(map[string]bridge.Content, len(inspect.Contents))
	for _, content := range inspect.Contents {
		contentsByID[content.ID] = content
	}
	for _, expected := range matched {
		actual, ok := contentsByID[expected.ContentID]
		if !ok {
			return fmt.Errorf("planned Rekordbox content ID %q no longer exists", expected.ContentID)
		}
		if NormalizePath(actual.FolderPath) != NormalizePath(expected.FolderPath) {
			return fmt.Errorf("planned Rekordbox content ID %q path changed", expected.ContentID)
		}
	}
	return nil
}

func validateOperationPreconditions(op PlanOperation, inspect bridge.InspectResponse) error {
	target, missing, err := selectRekordboxPlaylistInParent(inspect.Playlists, op.RekordboxPlaylist.Name, op.RekordboxPlaylist.ID, op.Preconditions.TargetParentID, op.RekordboxPlaylist.CreatePlanned)
	if err != nil {
		return err
	}
	if op.Preconditions.TargetPlaylistMissing {
		if !missing {
			return fmt.Errorf("target playlist %q now exists; regenerate the plan", op.Preconditions.TargetPlaylistName)
		}
	} else {
		if missing {
			return fmt.Errorf("target playlist %q is missing", op.Preconditions.TargetPlaylistName)
		}
		if target.ID != op.Preconditions.TargetPlaylistID {
			return fmt.Errorf("target playlist ID changed from %q to %q", op.Preconditions.TargetPlaylistID, target.ID)
		}
		if !SameStrings(target.ContentIDs, op.Preconditions.ExpectedCurrentContentIDs) {
			return fmt.Errorf("target playlist %q membership changed since plan generation", op.Preconditions.TargetPlaylistName)
		}
	}
	return validateMatchedContent(op.Preconditions.MatchedContent, inspect)
}

func SignPlan(plan *Plan) error {
	plan.ChecksumSHA256 = ""
	sum, err := planChecksum(*plan)
	if err != nil {
		return err
	}
	plan.ChecksumSHA256 = sum
	return nil
}

func VerifyPlanChecksum(plan Plan) error {
	expected := plan.ChecksumSHA256
	if strings.TrimSpace(expected) == "" {
		return fmt.Errorf("plan checksum is missing")
	}
	plan.ChecksumSHA256 = ""
	actual, err := planChecksum(plan)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("plan checksum mismatch")
	}
	return nil
}

func WritePlan(path string, plan Plan) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("plan output path must be set")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create plan directory: %w", err)
	}
	payload, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode plan: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write plan %s: %w", path, err)
	}
	return nil
}

func ReadPlan(path string) (Plan, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, fmt.Errorf("read plan %s: %w", path, err)
	}
	var plan Plan
	if err := json.Unmarshal(payload, &plan); err != nil {
		return Plan{}, fmt.Errorf("parse plan %s: %w", path, err)
	}
	return plan, nil
}

func CheckRekordboxClosed(ctx context.Context, dbDir string) error {
	processes, err := rekordboxProcesses(ctx)
	if err != nil {
		return err
	}
	if len(processes) > 0 {
		return fmt.Errorf("Rekordbox is running; close Rekordbox before using this command (%s)", strings.Join(processes, ", "))
	}
	for _, name := range []string{"master.db-wal", "master.db-shm"} {
		path := filepath.Join(dbDir, name)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("Rekordbox database sidecar exists while RB should be closed: %s", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect Rekordbox database sidecar %s: %w", path, err)
		}
	}
	return nil
}

func CreateBackup(ctx context.Context, dbDir, backupRoot string, now time.Time) (string, error) {
	if strings.TrimSpace(dbDir) == "" {
		return "", fmt.Errorf("rekordbox db dir must be set")
	}
	if strings.TrimSpace(backupRoot) == "" {
		return "", fmt.Errorf("backup dir must be set")
	}
	if err := os.MkdirAll(backupRoot, 0o755); err != nil {
		return "", fmt.Errorf("create backup root: %w", err)
	}
	dest := filepath.Join(backupRoot, "rekordbox-dir-before-playlist-sync-"+now.Format("20060102-150405"))
	if _, err := os.Stat(dest); err == nil {
		return "", fmt.Errorf("backup destination already exists: %s", dest)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect backup destination: %w", err)
	}
	if runtime.GOOS == "darwin" {
		if _, err := exec.LookPath("ditto"); err == nil {
			cmd := exec.CommandContext(ctx, "ditto", dbDir, dest)
			if out, err := cmd.CombinedOutput(); err != nil {
				return "", fmt.Errorf("create backup with ditto: %w: %s", err, strings.TrimSpace(string(out)))
			}
			return dest, nil
		}
	}
	if err := copyDir(dbDir, dest); err != nil {
		return "", err
	}
	return dest, nil
}

func NormalizePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "file:") {
		// url.Parse already percent-decodes Path; unescaping it again would
		// corrupt any path containing a literal percent sign.
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" {
			trimmed = parsed.Path
		}
	}
	cleaned := filepath.Clean(trimmed)
	if utf8.ValidString(cleaned) {
		cleaned = norm.NFC.String(cleaned)
	}
	return cleaned
}

func planChecksum(plan Plan) (string, error) {
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode plan checksum payload: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func selectRekordboxPlaylist(playlists []bridge.Playlist, name, id string, createIfMissing bool) (bridge.Playlist, bool, error) {
	if strings.TrimSpace(id) != "" {
		for _, playlist := range playlists {
			if playlist.ID == id {
				if playlist.Attribute != 0 {
					return bridge.Playlist{}, false, fmt.Errorf("Rekordbox playlist %q is not a normal playlist", playlist.Name)
				}
				return playlist, false, nil
			}
		}
		return bridge.Playlist{}, true, fmt.Errorf("Rekordbox playlist ID %q not found", id)
	}

	matches := []bridge.Playlist{}
	for _, playlist := range playlists {
		if playlist.Name == name {
			matches = append(matches, playlist)
		}
	}
	if len(matches) == 0 {
		if createIfMissing {
			return bridge.Playlist{Name: name}, true, nil
		}
		return bridge.Playlist{}, true, fmt.Errorf("Rekordbox playlist %q not found", name)
	}
	if len(matches) > 1 {
		return bridge.Playlist{}, false, fmt.Errorf("multiple Rekordbox playlists named %q; use --rekordbox-playlist-id", name)
	}
	if matches[0].Attribute != 0 {
		return bridge.Playlist{}, false, fmt.Errorf("Rekordbox playlist %q is not a normal playlist", name)
	}
	return matches[0], false, nil
}

func selectRekordboxFolder(playlists []bridge.Playlist, name, id string, createIfMissing bool) (bridge.Playlist, bool, error) {
	if strings.TrimSpace(id) != "" {
		for _, playlist := range playlists {
			if playlist.ID == id {
				if playlist.Attribute != 1 {
					return bridge.Playlist{}, false, fmt.Errorf("Rekordbox target %q is not a folder", playlist.Name)
				}
				return playlist, false, nil
			}
		}
		return bridge.Playlist{}, true, fmt.Errorf("Rekordbox folder ID %q not found", id)
	}
	matches := []bridge.Playlist{}
	for _, playlist := range playlists {
		if playlist.Name == name && playlist.ParentID == "root" {
			matches = append(matches, playlist)
		}
	}
	if len(matches) == 0 {
		if createIfMissing {
			return bridge.Playlist{Name: name, Attribute: 1, ParentID: "root"}, true, nil
		}
		return bridge.Playlist{}, true, fmt.Errorf("Rekordbox folder %q not found", name)
	}
	if len(matches) > 1 {
		return bridge.Playlist{}, false, fmt.Errorf("multiple root Rekordbox folders named %q; use --rekordbox-folder-id", name)
	}
	if matches[0].Attribute != 1 {
		return bridge.Playlist{}, false, fmt.Errorf("Rekordbox target %q is not a folder", name)
	}
	return matches[0], false, nil
}

func selectRekordboxPlaylistInParent(playlists []bridge.Playlist, name, id, parentID string, createIfMissing bool) (bridge.Playlist, bool, error) {
	if strings.TrimSpace(id) != "" {
		return selectRekordboxPlaylist(playlists, name, id, createIfMissing)
	}
	matches := []bridge.Playlist{}
	for _, playlist := range playlists {
		if playlist.Name == name && playlist.ParentID == parentID {
			matches = append(matches, playlist)
		}
	}
	if len(matches) == 0 {
		if createIfMissing {
			return bridge.Playlist{Name: name, ParentID: parentID}, true, nil
		}
		return bridge.Playlist{}, true, fmt.Errorf("Rekordbox playlist %q not found under target folder", name)
	}
	if len(matches) > 1 {
		return bridge.Playlist{}, false, fmt.Errorf("multiple Rekordbox playlists named %q under target folder", name)
	}
	if matches[0].Attribute != 0 {
		return bridge.Playlist{}, false, fmt.Errorf("Rekordbox playlist %q is not a normal playlist", name)
	}
	return matches[0], false, nil
}

func findJob(jobs []config.RekordboxPlaylistSyncJob, id string) (config.RekordboxPlaylistSyncJob, bool) {
	for _, job := range jobs {
		if job.ID == id {
			return job, true
		}
	}
	return config.RekordboxPlaylistSyncJob{}, false
}

func applyJob(resolved *ResolvedOptions, job config.RekordboxPlaylistSyncJob) {
	if strings.TrimSpace(job.MusicPlaylist) != "" {
		resolved.MusicPlaylist = job.MusicPlaylist
	}
	if strings.TrimSpace(job.MusicPlaylistID) != "" {
		resolved.MusicPlaylistID = job.MusicPlaylistID
	}
	if strings.TrimSpace(job.RekordboxPlaylist) != "" {
		resolved.RekordboxPlaylist = job.RekordboxPlaylist
	}
	if strings.TrimSpace(job.RekordboxPlaylistID) != "" {
		resolved.RekordboxPlaylistID = job.RekordboxPlaylistID
	}
	if strings.TrimSpace(job.Mode) != "" {
		resolved.Mode = job.Mode
	}
	if job.CreatePlaylist != nil {
		resolved.CreatePlaylist = *job.CreatePlaylist
	}
}

func addSummaries(a, b PlanSummary) PlanSummary {
	a.MusicTotal += b.MusicTotal
	a.MatchedByPath += b.MatchedByPath
	a.MissingInRekordbox += b.MissingInRekordbox
	a.AmbiguousInRB += b.AmbiguousInRB
	a.CurrentTargetCount += b.CurrentTargetCount
	a.FinalTargetCount += b.FinalTargetCount
	a.WillAdd += b.WillAdd
	a.WillRemove += b.WillRemove
	a.WillMove += b.WillMove
	a.WillKeep += b.WillKeep
	return a
}

func countChildren(playlists []bridge.Playlist, parentID string) int {
	count := 0
	for _, playlist := range playlists {
		if playlist.ParentID == parentID {
			count++
		}
	}
	return count
}

func boolPtrValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func uniqueStrings(values []string) []string {
	result := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func rekordboxProcesses(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "ps", "ax", "-o", "comm=")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("inspect running processes: %w", err)
	}
	found := []string{}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(string(out), "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "rekordbox") {
			name := strings.TrimSpace(filepath.Base(line))
			if name == "" {
				name = strings.TrimSpace(line)
			}
			if _, exists := seen[name]; !exists {
				found = append(found, name)
				seen[name] = struct{}{}
			}
		}
	}
	sort.Strings(found)
	return found, nil
}

func copyDir(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(target, info.Mode())
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.Mode().IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode())
		default:
			return nil
		}
	})
}

// SameStrings reports whether two ordered ID lists are identical, which is how
// both plan preconditions and post-apply verification compare playlist order.
func SameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func safeFilename(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "playlist-sync"
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

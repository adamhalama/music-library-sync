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
	"golang.org/x/text/unicode/norm"
)

const (
	DefaultMusicPlaylist     = "Favourites"
	DefaultRekordboxPlaylist = "fav_imports"
	DefaultRekordboxDBDir    = "~/Library/Pioneer/rekordbox"
	DefaultPythonBin         = "python3"
	DefaultBackupDir         = "/Users/jaa/Music/rb-library-export"
	DefaultMode              = "mirror"
	PlanVersion              = "1"
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
}

type Plan struct {
	Version           string                `json:"version"`
	GeneratedAt       string                `json:"generated_at"`
	JobID             string                `json:"job_id,omitempty"`
	Mode              string                `json:"mode"`
	RekordboxDBDir    string                `json:"rekordbox_db_dir"`
	BackupDir         string                `json:"backup_dir"`
	MusicPlaylist     PlanMusicPlaylist     `json:"music_playlist"`
	RekordboxPlaylist PlanRekordboxPlaylist `json:"rekordbox_playlist"`
	Summary           PlanSummary           `json:"summary"`
	Rows              []PlanRow             `json:"rows"`
	RemovalContentIDs []string              `json:"removal_content_ids"`
	FinalContentIDs   []string              `json:"final_content_ids"`
	Preconditions     PlanPreconditions     `json:"preconditions"`
	Warnings          []string              `json:"warnings,omitempty"`
	ChecksumSHA256    string                `json:"checksum_sha256"`
}

type PlanMusicPlaylist struct {
	Name         string `json:"name"`
	PersistentID string `json:"persistent_id,omitempty"`
	Smart        bool   `json:"smart"`
	TrackCount   int    `json:"track_count"`
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
	CurrentTargetCount int `json:"current_target_count"`
	FinalTargetCount   int `json:"final_target_count"`
	WillAdd            int `json:"will_add"`
	WillRemove         int `json:"will_remove"`
	WillMove           int `json:"will_move"`
	WillKeep           int `json:"will_keep"`
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

	contentByPath := map[string][]bridge.Content{}
	for _, content := range req.Inspect.Contents {
		normalized := NormalizePath(content.FolderPath)
		if normalized != "" {
			contentByPath[normalized] = append(contentByPath[normalized], content)
		}
	}

	currentIDs := []string{}
	if !targetMissing {
		currentIDs = append(currentIDs, target.ContentIDs...)
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
	matchedByPath := 0
	missing := 0
	ambiguous := 0
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
		candidates := contentByPath[normalizedPath]
		switch len(candidates) {
		case 0:
			missing++
		case 1:
			content := candidates[0]
			matchedByPath++
			row.RekordboxContentID = content.ID
			row.RekordboxTitle = content.Title
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

	plan := Plan{
		Version:        PlanVersion,
		GeneratedAt:    now.UTC().Format(time.RFC3339),
		JobID:          req.Options.JobID,
		Mode:           req.Options.Mode,
		RekordboxDBDir: req.Options.RekordboxDBDir,
		BackupDir:      req.Options.BackupDir,
		MusicPlaylist: PlanMusicPlaylist{
			Name:         req.MusicPlaylist.Name,
			PersistentID: req.MusicPlaylist.PersistentID,
			Smart:        req.MusicPlaylist.Smart,
			TrackCount:   req.MusicPlaylist.TrackCount,
		},
		RekordboxPlaylist: PlanRekordboxPlaylist{
			ID:            target.ID,
			Name:          firstNonEmpty(target.Name, req.Options.RekordboxPlaylist),
			Attribute:     target.Attribute,
			CurrentCount:  len(currentIDs),
			CreatePlanned: targetMissing,
		},
		Summary: PlanSummary{
			MusicTotal:         len(req.MusicTracks),
			MatchedByPath:      matchedByPath,
			MissingInRekordbox: missing,
			AmbiguousInRB:      ambiguous,
			CurrentTargetCount: len(currentIDs),
			FinalTargetCount:   len(finalIDs),
			WillAdd:            willAdd,
			WillRemove:         len(removals),
			WillMove:           willMove,
			WillKeep:           willKeep,
		},
		Rows:              rows,
		RemovalContentIDs: removals,
		FinalContentIDs:   finalIDs,
		Preconditions: PlanPreconditions{
			TargetPlaylistID:          target.ID,
			TargetPlaylistName:        firstNonEmpty(target.Name, req.Options.RekordboxPlaylist),
			TargetPlaylistMissing:     targetMissing,
			ExpectedCurrentContentIDs: currentIDs,
			MatchedContent:            matchedPreconditions,
		},
	}
	if missing > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d Music track(s) are missing from Rekordbox and will not be imported in v1", missing))
	}
	if ambiguous > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d Music track(s) matched multiple Rekordbox rows by path", ambiguous))
	}
	if err := SignPlan(&plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
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

func ValidatePlanForApply(plan Plan) error {
	if err := VerifyPlanChecksum(plan); err != nil {
		return err
	}
	if plan.Version != PlanVersion {
		return fmt.Errorf("unsupported plan version %q", plan.Version)
	}
	if plan.Mode != DefaultMode {
		return fmt.Errorf("unsupported plan mode %q", plan.Mode)
	}
	if plan.Summary.MissingInRekordbox > 0 {
		return fmt.Errorf("plan has %d missing Rekordbox tracks; v1 refuses partial mirror apply", plan.Summary.MissingInRekordbox)
	}
	if plan.Summary.AmbiguousInRB > 0 {
		return fmt.Errorf("plan has %d ambiguous Rekordbox path matches", plan.Summary.AmbiguousInRB)
	}
	if len(plan.FinalContentIDs) != plan.Summary.FinalTargetCount {
		return fmt.Errorf("plan final content count does not match summary")
	}
	return nil
}

func ValidatePreconditions(plan Plan, inspect bridge.InspectResponse) error {
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
		if !sameStrings(target.ContentIDs, plan.Preconditions.ExpectedCurrentContentIDs) {
			return fmt.Errorf("target playlist membership changed since plan generation")
		}
	}

	contentsByID := map[string]bridge.Content{}
	for _, content := range inspect.Contents {
		contentsByID[content.ID] = content
	}
	for _, expected := range plan.Preconditions.MatchedContent {
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
		if parsed, err := url.Parse(trimmed); err == nil {
			if unescaped, err := url.PathUnescape(parsed.Path); err == nil {
				trimmed = unescaped
			}
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

func sameStrings(a, b []string) bool {
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

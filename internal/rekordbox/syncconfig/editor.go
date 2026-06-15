package syncconfig

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
	"gopkg.in/yaml.v3"
)

type WritePathOptions struct {
	ExplicitPath string
	WorkingDir   string
	Env          map[string]string
}

type WritePathResolution struct {
	Path   string
	Kind   string
	Exists bool
}

type marshalConfig struct {
	Version  int             `yaml:"version"`
	Defaults marshalDefaults `yaml:"defaults"`
	Sync     marshalSync     `yaml:"sync"`
}

type marshalDefaults struct {
	DBDir           string `yaml:"db_dir,omitempty"`
	PythonBin       string `yaml:"python_bin,omitempty"`
	PythonPath      string `yaml:"python_path,omitempty"`
	BackupDir       string `yaml:"backup_dir,omitempty"`
	Mode            string `yaml:"mode,omitempty"`
	CreateFolders   bool   `yaml:"create_folders"`
	CreatePlaylists bool   `yaml:"create_playlists"`
}

type marshalSync struct {
	Folders []marshalFolderMapping `yaml:"folders,omitempty"`
	Jobs    []marshalPlaylistJob   `yaml:"jobs,omitempty"`
}

type marshalFolderMapping struct {
	ID                string            `yaml:"id"`
	MusicFolder       string            `yaml:"music_folder,omitempty"`
	MusicFolderID     string            `yaml:"music_folder_id,omitempty"`
	RekordboxFolder   string            `yaml:"rekordbox_folder,omitempty"`
	RekordboxFolderID string            `yaml:"rekordbox_folder_id,omitempty"`
	PlaylistNameMap   map[string]string `yaml:"playlist_name_map,omitempty"`
	IncludePlaylists  []string          `yaml:"include_playlists,omitempty"`
	ExcludePlaylists  []string          `yaml:"exclude_playlists,omitempty"`
	OnMissingTracks   string            `yaml:"on_missing_tracks,omitempty"`
	CreateFolders     *bool             `yaml:"create_folders,omitempty"`
	CreatePlaylists   *bool             `yaml:"create_playlists,omitempty"`
}

type marshalPlaylistJob struct {
	ID                  string `yaml:"id"`
	MusicPlaylist       string `yaml:"music_playlist,omitempty"`
	MusicPlaylistID     string `yaml:"music_playlist_id,omitempty"`
	RekordboxPlaylist   string `yaml:"rekordbox_playlist,omitempty"`
	RekordboxPlaylistID string `yaml:"rekordbox_playlist_id,omitempty"`
	Mode                string `yaml:"mode,omitempty"`
	CreatePlaylist      *bool  `yaml:"create_playlist,omitempty"`
}

func ResolveWritePath(opts WritePathOptions) (WritePathResolution, error) {
	env := opts.Env
	if env == nil {
		env = osEnvMap()
	}
	if explicit := firstNonEmpty(opts.ExplicitPath, env[EnvConfigPath]); explicit != "" {
		path, err := config.ExpandPath(explicit)
		if err != nil {
			return WritePathResolution{}, err
		}
		return writePathResolution(path, "explicit")
	}

	cwd := strings.TrimSpace(opts.WorkingDir)
	if cwd == "" {
		wd, err := os.Getwd()
		if err != nil {
			return WritePathResolution{}, fmt.Errorf("resolve working directory: %w", err)
		}
		cwd = wd
	}
	projectPath := ProjectConfigPath(cwd)
	if existsRegularFile(projectPath) {
		return writePathResolution(projectPath, "project")
	}
	userPath, err := UserConfigPath()
	if err != nil {
		return WritePathResolution{}, err
	}
	if existsRegularFile(userPath) {
		return writePathResolution(userPath, "user")
	}
	return writePathResolution(userPath, "new-user")
}

func Save(path string, cfg Config) error {
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return err
	}
	payload, err := Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create Rekordbox config directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".rekordbox-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Rekordbox config: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary Rekordbox config: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temporary Rekordbox config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary Rekordbox config: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace Rekordbox config: %w", err)
	}
	cleanup = false
	return nil
}

func Marshal(cfg Config) ([]byte, error) {
	normalize(&cfg)
	out := marshalConfig{
		Version: cfg.Version,
		Defaults: marshalDefaults{
			DBDir:           cfg.Defaults.DBDir,
			PythonBin:       cfg.Defaults.PythonBin,
			PythonPath:      cfg.Defaults.PythonPath,
			BackupDir:       cfg.Defaults.BackupDir,
			Mode:            cfg.Defaults.Mode,
			CreateFolders:   cfg.Defaults.CreateFolders,
			CreatePlaylists: cfg.Defaults.CreatePlaylists,
		},
	}
	for _, mapping := range cfg.Sync.Folders {
		out.Sync.Folders = append(out.Sync.Folders, marshalFolderMapping{
			ID:                strings.TrimSpace(mapping.ID),
			MusicFolder:       strings.TrimSpace(mapping.MusicFolder),
			MusicFolderID:     strings.TrimSpace(mapping.MusicFolderID),
			RekordboxFolder:   strings.TrimSpace(mapping.RekordboxFolder),
			RekordboxFolderID: strings.TrimSpace(mapping.RekordboxFolderID),
			PlaylistNameMap:   sortedStringMap(mapping.PlaylistNameMap),
			IncludePlaylists:  trimStringSlice(mapping.IncludePlaylists),
			ExcludePlaylists:  trimStringSlice(mapping.ExcludePlaylists),
			OnMissingTracks:   firstNonEmpty(mapping.OnMissingTracks, DefaultMissingTracks),
			CreateFolders:     copyBoolPtr(mapping.CreateFolders),
			CreatePlaylists:   copyBoolPtr(mapping.CreatePlaylists),
		})
	}
	for _, job := range cfg.Sync.Jobs {
		out.Sync.Jobs = append(out.Sync.Jobs, marshalPlaylistJob{
			ID:                  strings.TrimSpace(job.ID),
			MusicPlaylist:       strings.TrimSpace(job.MusicPlaylist),
			MusicPlaylistID:     strings.TrimSpace(job.MusicPlaylistID),
			RekordboxPlaylist:   strings.TrimSpace(job.RekordboxPlaylist),
			RekordboxPlaylistID: strings.TrimSpace(job.RekordboxPlaylistID),
			Mode:                strings.TrimSpace(job.Mode),
			CreatePlaylist:      copyBoolPtr(job.CreatePlaylist),
		})
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(out); err != nil {
		return nil, fmt.Errorf("encode Rekordbox config: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("encode Rekordbox config: %w", err)
	}
	return buf.Bytes(), nil
}

func Clone(cfg Config) Config {
	return Config{
		Version:  cfg.Version,
		Defaults: cfg.Defaults,
		Sync: Sync{
			Folders: cloneFolderMappings(cfg.Sync.Folders),
			Jobs:    clonePlaylistJobs(cfg.Sync.Jobs),
		},
		Warnings: append([]string(nil), cfg.Warnings...),
	}
}

func cloneFolderMappings(in []FolderMapping) []FolderMapping {
	out := make([]FolderMapping, 0, len(in))
	for _, mapping := range in {
		copyMapping := mapping
		copyMapping.PlaylistNameMap = copyStringMap(mapping.PlaylistNameMap)
		copyMapping.IncludePlaylists = append([]string(nil), mapping.IncludePlaylists...)
		copyMapping.ExcludePlaylists = append([]string(nil), mapping.ExcludePlaylists...)
		copyMapping.CreateFolders = copyBoolPtr(mapping.CreateFolders)
		copyMapping.CreatePlaylists = copyBoolPtr(mapping.CreatePlaylists)
		out = append(out, copyMapping)
	}
	return out
}

func clonePlaylistJobs(in []PlaylistJob) []PlaylistJob {
	out := make([]PlaylistJob, 0, len(in))
	for _, job := range in {
		copyJob := job
		copyJob.CreatePlaylist = copyBoolPtr(job.CreatePlaylist)
		out = append(out, copyJob)
	}
	return out
}

func sortedStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(in[key]) != "" {
			keys = append(keys, strings.TrimSpace(key))
		}
	}
	sort.Strings(keys)
	out := map[string]string{}
	for _, key := range keys {
		out[key] = strings.TrimSpace(in[key])
	}
	return out
}

func writePathResolution(path, kind string) (WritePathResolution, error) {
	expanded, err := config.ExpandPath(path)
	if err != nil {
		return WritePathResolution{}, err
	}
	info, err := os.Stat(expanded)
	if err == nil && info.IsDir() {
		return WritePathResolution{}, fmt.Errorf("Rekordbox config path is a directory: %s", expanded)
	}
	return WritePathResolution{Path: expanded, Kind: kind, Exists: err == nil}, nil
}

func existsRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

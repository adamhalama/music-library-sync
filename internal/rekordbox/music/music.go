package music

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type PlaylistSelector struct {
	Name         string
	PersistentID string
}

type Playlist struct {
	Name         string `json:"name"`
	PersistentID string `json:"persistent_id"`
	ParentName   string `json:"parent_name,omitempty"`
	ParentID     string `json:"parent_id,omitempty"`
	Folder       bool   `json:"folder"`
	Smart        bool   `json:"smart"`
	TrackCount   int    `json:"track_count"`
}

type Track struct {
	Index        int    `json:"index"`
	PersistentID string `json:"persistent_id"`
	DatabaseID   string `json:"database_id"`
	Artist       string `json:"artist"`
	Title        string `json:"title"`
	Album        string `json:"album"`
	Duration     string `json:"duration"`
	Path         string `json:"path"`
}

type Reader struct {
	RunAppleScript func(ctx context.Context, script string) (string, error)
}

func (r Reader) ListPlaylists(ctx context.Context) ([]Playlist, error) {
	out, err := r.run(ctx, listPlaylistsScript)
	if err != nil {
		return nil, err
	}
	return ParsePlaylistList(out)
}

func (r Reader) ReadPlaylist(ctx context.Context, selector PlaylistSelector) (Playlist, []Track, error) {
	if strings.TrimSpace(selector.PersistentID) == "" && strings.TrimSpace(selector.Name) == "" {
		return Playlist{}, nil, fmt.Errorf("music playlist name or persistent ID must be set")
	}

	script := buildReadPlaylistScript(selector)
	out, err := r.run(ctx, script)
	if err != nil {
		return Playlist{}, nil, err
	}
	return ParsePlaylistTracks(out)
}

func (r Reader) run(ctx context.Context, script string) (string, error) {
	if r.RunAppleScript != nil {
		out, err := r.RunAppleScript(ctx, script)
		if err != nil {
			return "", actionableMusicAutomationError(err)
		}
		return out, nil
	}
	cmd := exec.CommandContext(ctx, "osascript")
	cmd.Stdin = strings.NewReader(script)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			return "", actionableMusicAutomationError(fmt.Errorf("run osascript: %w", err))
		}
		return "", actionableMusicAutomationError(fmt.Errorf("run osascript: %w: %s", err, detail))
	}
	out := strings.TrimRight(stdout.String(), "\r\n")
	if strings.HasPrefix(out, "ERROR ") {
		return "", fmt.Errorf("Music AppleScript failed: %s", out)
	}
	return out, nil
}

func actionableMusicAutomationError(err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"-1743", "not authorized", "not permitted", "automation permission"} {
		if strings.Contains(text, marker) {
			return fmt.Errorf(
				"Music automation permission denied; allow UDL in System Settings > Privacy & Security > Automation: %w",
				err,
			)
		}
	}
	return err
}

func ParsePlaylistList(raw string) ([]Playlist, error) {
	playlists := []Playlist{}
	for _, line := range splitLines(raw) {
		cols := strings.Split(line, "\t")
		if len(cols) != 4 && len(cols) != 7 {
			return nil, fmt.Errorf("parse Music playlist row %q: expected 4 or 7 columns, got %d", line, len(cols))
		}
		folder := false
		parentID := ""
		parentName := ""
		smartCol := 2
		countCol := 3
		if len(cols) == 7 {
			parentID = cols[2]
			parentName = cols[3]
			var err error
			folder, err = strconv.ParseBool(cols[4])
			if err != nil {
				return nil, fmt.Errorf("parse Music playlist folder flag %q: %w", cols[4], err)
			}
			smartCol = 5
			countCol = 6
		}
		smart, err := strconv.ParseBool(cols[smartCol])
		if err != nil {
			return nil, fmt.Errorf("parse Music playlist smart flag %q: %w", cols[smartCol], err)
		}
		count, err := strconv.Atoi(cols[countCol])
		if err != nil {
			return nil, fmt.Errorf("parse Music playlist track count %q: %w", cols[countCol], err)
		}
		playlists = append(playlists, Playlist{
			PersistentID: cols[0],
			Name:         cols[1],
			ParentID:     parentID,
			ParentName:   parentName,
			Folder:       folder,
			Smart:        smart,
			TrackCount:   count,
		})
	}
	return playlists, nil
}

func ParsePlaylistTracks(raw string) (Playlist, []Track, error) {
	var playlist Playlist
	tracks := []Track{}
	sawPlaylist := false
	for _, line := range splitLines(raw) {
		cols := strings.Split(line, "\t")
		if len(cols) == 0 {
			continue
		}
		switch cols[0] {
		case "PLAYLIST":
			if len(cols) != 5 {
				return Playlist{}, nil, fmt.Errorf("parse Music playlist header: expected 5 columns, got %d", len(cols))
			}
			smart, err := strconv.ParseBool(cols[3])
			if err != nil {
				return Playlist{}, nil, fmt.Errorf("parse Music playlist smart flag %q: %w", cols[3], err)
			}
			count, err := strconv.Atoi(cols[4])
			if err != nil {
				return Playlist{}, nil, fmt.Errorf("parse Music playlist track count %q: %w", cols[4], err)
			}
			playlist = Playlist{
				PersistentID: cols[1],
				Name:         cols[2],
				Smart:        smart,
				TrackCount:   count,
			}
			sawPlaylist = true
		case "TRACK":
			if len(cols) != 9 {
				return Playlist{}, nil, fmt.Errorf("parse Music track row: expected 9 columns, got %d", len(cols))
			}
			index, err := strconv.Atoi(cols[1])
			if err != nil {
				return Playlist{}, nil, fmt.Errorf("parse Music track index %q: %w", cols[1], err)
			}
			tracks = append(tracks, Track{
				Index:        index,
				PersistentID: cols[2],
				DatabaseID:   cols[3],
				Artist:       cols[4],
				Title:        cols[5],
				Album:        cols[6],
				Duration:     cols[7],
				Path:         cols[8],
			})
		default:
			return Playlist{}, nil, fmt.Errorf("parse Music output: unexpected row kind %q", cols[0])
		}
	}
	if !sawPlaylist {
		return Playlist{}, nil, fmt.Errorf("parse Music output: missing playlist header")
	}
	return playlist, tracks, nil
}

func splitLines(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func buildReadPlaylistScript(selector PlaylistSelector) string {
	return fmt.Sprintf(readPlaylistScriptTemplate, appleScriptString(selector.PersistentID), appleScriptString(selector.Name))
}

func appleScriptString(value string) string {
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return `"` + escaped + `"`
}

const listPlaylistsScript = `
set oldDelims to AppleScript's text item delimiters
set AppleScript's text item delimiters to linefeed
try
  tell application "Music"
    set rows to {}
    repeat with p in user playlists
      set pid to ""
      set pname to ""
      set parentID to ""
      set parentName to ""
      set folderFlag to "false"
      set psmart to "false"
      set pcount to "0"
      try
        set pid to persistent ID of p as text
      end try
      try
        set pname to name of p as text
      end try
      try
        if (special kind of p as text) is "folder" then
          set folderFlag to "true"
        end if
      end try
      try
        set parentPlaylist to parent of p
        if class of parentPlaylist is user playlist then
          set parentID to persistent ID of parentPlaylist as text
          set parentName to name of parentPlaylist as text
        end if
      end try
      try
        set psmart to smart of p as text
      end try
      try
        set pcount to (count of tracks of p) as text
      end try
      set end of rows to pid & tab & pname & tab & parentID & tab & parentName & tab & folderFlag & tab & psmart & tab & pcount
    end repeat
  end tell
  set outText to rows as text
on error errMsg number errNum
  set outText to "ERROR " & errNum & ": " & errMsg
end try
set AppleScript's text item delimiters to oldDelims
return outText
`

const readPlaylistScriptTemplate = `
set targetPersistentID to %s
set targetName to %s
set oldDelims to AppleScript's text item delimiters
set AppleScript's text item delimiters to linefeed
try
  tell application "Music"
    set targetPlaylist to missing value
    repeat with p in user playlists
      set pid to ""
      try
        set pid to persistent ID of p as text
      end try
      if targetPersistentID is not "" then
        if pid is targetPersistentID then
          set targetPlaylist to p
          exit repeat
        end if
      else
        if (name of p as text) is targetName then
          set targetPlaylist to p
          exit repeat
        end if
      end if
    end repeat
    if targetPlaylist is missing value then
      error "Music playlist not found"
    end if

    set pid to ""
    try
      set pid to persistent ID of targetPlaylist as text
    end try
    set rows to {"PLAYLIST" & tab & pid & tab & (name of targetPlaylist as text) & tab & (smart of targetPlaylist as text) & tab & ((count of tracks of targetPlaylist) as text)}
    set i to 0
    repeat with t in tracks of targetPlaylist
      set i to i + 1
      set locText to ""
      try
        set locText to POSIX path of ((location of t) as alias)
      end try
      set end of rows to "TRACK" & tab & (i as text) & tab & (persistent ID of t as text) & tab & (database ID of t as text) & tab & (artist of t as text) & tab & (name of t as text) & tab & (album of t as text) & tab & ((duration of t) as text) & tab & locText
    end repeat
  end tell
  set outText to rows as text
on error errMsg number errNum
  set outText to "ERROR " & errNum & ": " & errMsg
end try
set AppleScript's text item delimiters to oldDelims
return outText
`

// ListFavoriteTracks enumerates favorited local file tracks in the Apple Music
// library. It is strictly read-only: nothing in this package ever writes to
// Music.
func (r Reader) ListFavoriteTracks(ctx context.Context) ([]Track, error) {
	out, err := r.run(ctx, listFavoriteTracksScript)
	if err != nil {
		return nil, err
	}
	return ParseFavoriteTracks(out)
}

// ParseFavoriteTracks parses the favorite enumeration rows.
func ParseFavoriteTracks(raw string) ([]Track, error) {
	tracks := []Track{}
	for _, line := range splitLines(raw) {
		cols := strings.Split(line, "\t")
		if len(cols) == 0 || cols[0] == "" {
			continue
		}
		if cols[0] != "TRACK" {
			return nil, fmt.Errorf("parse Music favorite output: unexpected row kind %q", cols[0])
		}
		if len(cols) != 8 {
			return nil, fmt.Errorf("parse Music favorite row: expected 8 columns, got %d", len(cols))
		}
		tracks = append(tracks, Track{
			Index:        len(tracks) + 1,
			PersistentID: cols[1],
			DatabaseID:   cols[2],
			Artist:       cols[3],
			Title:        cols[4],
			Album:        cols[5],
			Duration:     cols[6],
			Path:         cols[7],
		})
	}
	return tracks, nil
}

// listFavoriteTracksScript reads favorites without mutating anything. Music.app
// renamed the property from `loved` to `favorited`, so the script tries the
// modern name first and falls back, rather than failing on one of the two.
const listFavoriteTracksScript = `
set oldDelims to AppleScript's text item delimiters
set AppleScript's text item delimiters to linefeed
try
  tell application "Music"
    -- "matched" is a Music.app term (smart-playlist matched), so a variable of
    -- that name fails with -10003. favTracks is safe.
    set favTracks to {}
    try
      set favTracks to (every file track of library playlist 1 whose favorited is true)
    on error
      -- "loved" was removed in newer Music versions and "favorited" is absent
      -- in older ones; whichever exists is used.
      set favTracks to (every file track of library playlist 1 whose loved is true)
    end try
    set rows to {}
    repeat with t in favTracks
      set locText to ""
      try
        set locText to POSIX path of ((location of t) as alias)
      end try
      set end of rows to "TRACK" & tab & (persistent ID of t as text) & tab & (database ID of t as text) & tab & (artist of t as text) & tab & (name of t as text) & tab & (album of t as text) & tab & ((duration of t) as text) & tab & locText
    end repeat
  end tell
  set outText to rows as text
on error errMsg number errNum
  set outText to "ERROR " & errNum & ": " & errMsg
end try
set AppleScript's text item delimiters to oldDelims
return outText
`

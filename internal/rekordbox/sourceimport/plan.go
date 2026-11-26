// Package sourceimport plans importing a udl source's local files into the
// Rekordbox collection so that Date Added follows the source's own order
// (oldest first). The app layer feeds it the remote order,
// the sync state, the collection rows and file tags, and hands the plan to the
// pyrekordbox bridge.
package sourceimport

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/fileops"
	"golang.org/x/text/unicode/norm"
)

const PlanVersion = "1"

const (
	// StatusExisting rows are already in the collection; only Date Added may change.
	StatusExisting = "existing"
	// StatusImport rows are local files not yet in the collection.
	StatusImport = "import"
	// StatusNotDownloaded rows have no local file recorded in sync state.
	StatusNotDownloaded = "not_downloaded"
	// StatusFileMissing rows have a recorded path that is not on disk.
	StatusFileMissing = "file_missing"
	// StatusDuplicatePath rows point at a file an earlier row already uses.
	StatusDuplicatePath = "duplicate_path"
)

// TimestampLayout is how created_at values travel between Go, the plan and the
// Python helper: UTC with milliseconds, which is the precision Rekordbox keeps.
const TimestampLayout = "2006-01-02T15:04:05.000Z"

// StockDateLayout is Rekordbox's Date Added column (djmdContent.StockDate).
const StockDateLayout = "2006-01-02"

// SourceTrack is one track of the source, in source order.
type SourceTrack struct {
	Position int
	ID       string
	Title    string
	Artist   string
	// Path is the absolute local path recorded by sync, or empty.
	Path string
}

// Content is a collection row under the source's target directory.
type Content struct {
	ID         string
	FolderPath string
	CreatedAt  time.Time
	StockDate  string
}

// Input is everything BuildPlan needs besides filesystem path resolution.
type Input struct {
	SourceID       string
	SourceType     string
	AppendOnly     bool
	LocalNotRemote int
	PlaylistURL    string
	TargetDir      string
	DBDir          string
	BackupDir      string
	Tracks         []SourceTrack
	Contents       []Content
	// ImportTime anchors Date Added for new tracks after the last existing one.
	ImportTime time.Time
	// Step separates consecutive tracks; at least one second so the seconds
	// survive Rekordbox's storage.
	Step time.Duration
	// Location decides the local calendar day written to StockDate.
	Location *time.Location
	// GenreOverride replaces the file's genre tag on imported rows.
	GenreOverride string
	FileExists    func(path string) bool
	TagReader     TagReader
}

type Row struct {
	Position int `json:"position"`
	// ID keeps the legacy JSON key so existing Spotify plan consumers still work.
	ID           string `json:"spotify_id,omitempty"`
	Title        string `json:"title"`
	Artist       string `json:"artist,omitempty"`
	Path         string `json:"path,omitempty"`
	Status       string `json:"status"`
	ContentID    string `json:"content_id,omitempty"`
	OldCreatedAt string `json:"old_created_at,omitempty"`
	NewCreatedAt string `json:"new_created_at,omitempty"`
	OldStockDate string `json:"old_stock_date,omitempty"`
	NewStockDate string `json:"new_stock_date,omitempty"`
	Tags         *Tags  `json:"tags,omitempty"`
}

// Changes reports whether applying the row writes anything.
func (r Row) Changes() bool {
	switch r.Status {
	case StatusImport:
		return true
	case StatusExisting:
		return r.OldCreatedAt != r.NewCreatedAt || r.OldStockDate != r.NewStockDate
	default:
		return false
	}
}

type Summary struct {
	SourceTracks       int `json:"source_tracks"`
	CollectionExisting int `json:"collection_existing,omitempty"`
	LocalNotRemote     int `json:"local_not_remote,omitempty"`
	Existing           int `json:"existing"`
	Import             int `json:"import"`
	DateChanges        int `json:"date_changes"`
	NotDownloaded      int `json:"not_downloaded"`
	FileMissing        int `json:"file_missing"`
	DuplicatePath      int `json:"duplicate_path"`
}

type Plan struct {
	Version       string   `json:"version"`
	SourceType    string   `json:"source_type,omitempty"`
	AppendOnly    bool     `json:"append_only,omitempty"`
	GeneratedAt   string   `json:"generated_at"`
	SourceID      string   `json:"source_id"`
	PlaylistURL   string   `json:"playlist_url"`
	TargetDir     string   `json:"target_dir"`
	DBDir         string   `json:"rekordbox_db_dir"`
	BackupDir     string   `json:"backup_dir"`
	Order         string   `json:"order"`
	StepMillis    int64    `json:"step_millis"`
	GenreOverride string   `json:"genre_override,omitempty"`
	Rows          []Row    `json:"rows"`
	Summary       Summary  `json:"summary"`
	Warnings      []string `json:"warnings,omitempty"`
}

// HasChanges reports whether applying the plan writes anything.
func (p Plan) HasChanges() bool {
	for _, row := range p.Rows {
		if row.Changes() {
			return true
		}
	}
	return false
}

// BuildPlan orders the source's local files oldest first (source order) and
// assigns each a strictly increasing Date Added. Existing rows keep their
// timestamp when it already sorts after the previous track; otherwise they move
// to one step after it. New tracks inside the existing run follow the previous
// track; new tracks after the last existing one start at ImportTime. Re-running
// on an already ordered collection therefore changes nothing.
func BuildPlan(ctx context.Context, in Input) (Plan, error) {
	if in.Step < time.Second {
		return Plan{}, fmt.Errorf("date step must be at least one second, got %s", in.Step)
	}
	if in.ImportTime.IsZero() {
		return Plan{}, fmt.Errorf("import time must be set")
	}
	loc := in.Location
	if loc == nil {
		loc = time.Local
	}
	fileExists := in.FileExists
	if fileExists == nil {
		return Plan{}, fmt.Errorf("file existence check must be set")
	}

	plan := Plan{
		Version:       PlanVersion,
		SourceType:    in.SourceType,
		AppendOnly:    in.AppendOnly,
		GeneratedAt:   in.ImportTime.UTC().Format(time.RFC3339),
		SourceID:      in.SourceID,
		PlaylistURL:   in.PlaylistURL,
		TargetDir:     in.TargetDir,
		DBDir:         in.DBDir,
		BackupDir:     in.BackupDir,
		Order:         "oldest_first",
		StepMillis:    in.Step.Milliseconds(),
		GenreOverride: strings.TrimSpace(in.GenreOverride),
	}

	byPath := map[string]Content{}
	for _, content := range in.Contents {
		key := pathKey(content.FolderPath)
		if existing, ok := byPath[key]; ok {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("collection has two rows for %s (IDs %s and %s); using %s", content.FolderPath, existing.ID, content.ID, existing.ID))
			continue
		}
		byPath[key] = content
	}

	tracks := append([]SourceTrack(nil), in.Tracks...)
	sort.SliceStable(tracks, func(i, j int) bool { return tracks[i].Position < tracks[j].Position })

	usedPaths := map[string]int{}
	for _, track := range tracks {
		key := pathKey(track.Path)
		row := Row{
			Position: track.Position,
			ID:       track.ID,
			Title:    track.Title,
			Artist:   track.Artist,
			Path:     track.Path,
		}
		switch {
		case strings.TrimSpace(track.Path) == "":
			row.Status = StatusNotDownloaded
		case usedPaths[key] > 0:
			row.Status = StatusDuplicatePath
		case !fileExists(track.Path) && !(in.AppendOnly && byPath[key].ID != ""):
			row.Status = StatusFileMissing
		default:
			usedPaths[key] = track.Position
			if content, ok := byPath[key]; ok {
				row.Status = StatusExisting
				row.ContentID = content.ID
				row.Path = content.FolderPath
				row.OldCreatedAt = FormatTimestamp(content.CreatedAt)
				row.OldStockDate = content.StockDate
			} else {
				row.Status = StatusImport
				if in.TagReader == nil {
					tags := TagsFromFilename(track.Path)
					row.Tags = &tags
				} else {
					tags, err := in.TagReader.ReadTags(ctx, track.Path)
					if err != nil {
						return Plan{}, err
					}
					row.Tags = &tags
				}
				if plan.GenreOverride != "" {
					row.Tags.Genre = plan.GenreOverride
				}
			}
		}
		plan.Rows = append(plan.Rows, row)
	}

	if in.AppendOnly {
		assignAppendOnlyTimeline(plan.Rows, in.Contents, in.ImportTime, in.Step, loc)
	} else {
		assignTimeline(plan.Rows, in.Contents, in.ImportTime, in.Step, loc)
	}
	plan.Summary = summarize(plan.Rows)
	plan.Summary.CollectionExisting = len(in.Contents)
	plan.Summary.LocalNotRemote = in.LocalNotRemote
	for _, row := range plan.Rows {
		if row.Tags != nil && row.Tags.Source == TagSourceFilename {
			plan.Warnings = append(plan.Warnings, "ffprobe not found; new tracks get title/artist from their file names only")
			break
		}
	}
	return plan, nil
}

// Append-only imports follow every source row, including rows no longer remote.
// Existing rows retain their exact stored values even when their order differs.
func assignAppendOnlyTimeline(rows []Row, contents []Content, now time.Time, step time.Duration, loc *time.Location) {
	last := now
	for _, content := range contents {
		if content.CreatedAt.After(last) {
			last = content.CreatedAt
		}
	}
	for i := range rows {
		row := &rows[i]
		switch row.Status {
		case StatusExisting:
			row.NewCreatedAt, row.NewStockDate = row.OldCreatedAt, row.OldStockDate
		case StatusImport:
			last = normalizeTimestamp(last.Add(step))
			row.NewCreatedAt = FormatTimestamp(last)
			row.NewStockDate = last.In(loc).Format(StockDateLayout)
		}
	}
}

func assignTimeline(rows []Row, contents []Content, importTime time.Time, step time.Duration, loc *time.Location) {
	createdByID := map[string]time.Time{}
	for _, content := range contents {
		createdByID[content.ID] = content.CreatedAt
	}
	lastExisting := -1
	for i, row := range rows {
		if row.Status == StatusExisting {
			lastExisting = i
		}
	}
	var prev time.Time
	for i := range rows {
		row := &rows[i]
		var base time.Time
		switch row.Status {
		case StatusExisting:
			base = createdByID[row.ContentID]
		case StatusImport:
			if i > lastExisting {
				base = importTime
			}
		default:
			continue
		}
		next := normalizeTimestamp(base)
		switch {
		case !prev.IsZero():
			// Keep the row's own time only when it is at least one step after
			// the previous track; otherwise follow the previous track.
			if next.Before(prev.Add(step)) {
				next = normalizeTimestamp(prev.Add(step))
			}
		case next.IsZero():
			// The first timeline row has no time of its own: a new track ahead
			// of every existing one. Count back from the first existing row so
			// the leading new tracks still sort before it.
			anchor, leading := firstExistingAfter(rows, i, createdByID)
			if anchor.IsZero() {
				anchor = importTime
				leading = 0
			}
			next = normalizeTimestamp(anchor.Add(-step * time.Duration(leading)))
		}
		row.NewCreatedAt = FormatTimestamp(next)
		row.NewStockDate = next.In(loc).Format(StockDateLayout)
		prev = next
	}
}

// firstExistingAfter returns the created_at of the first existing row at or
// after index i and how many new rows come before it.
func firstExistingAfter(rows []Row, i int, createdByID map[string]time.Time) (time.Time, int) {
	leading := 0
	for j := i; j < len(rows); j++ {
		switch rows[j].Status {
		case StatusExisting:
			return createdByID[rows[j].ContentID], leading
		case StatusImport:
			leading++
		}
	}
	return time.Time{}, leading
}

// normalizeTimestamp truncates to milliseconds and keeps a non-zero fraction:
// pyrekordbox drops the seconds of a value whose fraction is zero.
func normalizeTimestamp(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	t = t.UTC().Truncate(time.Millisecond)
	if t.Nanosecond() == 0 {
		t = t.Add(900 * time.Millisecond)
	}
	return t
}

func FormatTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(TimestampLayout)
}

func ParseTimestamp(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", raw, err)
	}
	return t.UTC(), nil
}

func summarize(rows []Row) Summary {
	summary := Summary{SourceTracks: len(rows)}
	for _, row := range rows {
		switch row.Status {
		case StatusExisting:
			summary.Existing++
			if row.Changes() {
				summary.DateChanges++
			}
		case StatusImport:
			summary.Import++
		case StatusNotDownloaded:
			summary.NotDownloaded++
		case StatusFileMissing:
			summary.FileMissing++
		case StatusDuplicatePath:
			summary.DuplicatePath++
		}
	}
	return summary
}

// TimelineRows returns the rows that take part in the Date Added order.
func (p Plan) TimelineRows() []Row {
	out := make([]Row, 0, len(p.Rows))
	for _, row := range p.Rows {
		if row.Status == StatusImport || !p.AppendOnly && row.Status == StatusExisting {
			out = append(out, row)
		}
	}
	return out
}

// VerifyTimeline checks that the timeline rows' new timestamps strictly
// increase in source order.
func VerifyTimeline(rows []Row) error {
	var prev time.Time
	for _, row := range rows {
		t, err := ParseTimestamp(row.NewCreatedAt)
		if err != nil {
			return err
		}
		if t.IsZero() {
			return fmt.Errorf("track %d %q has no Date Added", row.Position, row.Title)
		}
		if !prev.IsZero() && !t.After(prev) {
			return fmt.Errorf("Date Added does not increase at track %d %q", row.Position, row.Title)
		}
		prev = t
	}
	return nil
}

// pathKey compares paths across symlink aliases and macOS Unicode forms.
func pathKey(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if filepath.IsAbs(path) {
		if resolved, err := fileops.ResolveSymlinks(path); err == nil {
			path = resolved
		}
	}
	return norm.NFC.String(path)
}

// SamePath reports whether two paths name the same file under pathKey rules.
func SamePath(a, b string) bool {
	return pathKey(a) == pathKey(b)
}

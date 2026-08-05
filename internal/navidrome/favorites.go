package navidrome

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// FavoritePlanVersion is bumped whenever the plan shape changes incompatibly.
const FavoritePlanVersion = "1"

// Favorite migration row statuses.
const (
	// FavoriteStatusMatched is an exact real-path match that apply will star.
	FavoriteStatusMatched = "matched"
	// FavoriteStatusAlreadyStarred is already starred on the server.
	FavoriteStatusAlreadyStarred = "already_starred"
	// FavoriteStatusOutsideLibrary is favorited in Apple Music but lives
	// outside the configured music root.
	FavoriteStatusOutsideLibrary = "outside_library"
	// FavoriteStatusMissing has no Navidrome track at that path.
	FavoriteStatusMissing = "missing"
	// FavoriteStatusAmbiguous matches more than one Navidrome track.
	FavoriteStatusAmbiguous = "ambiguous"
	// FavoriteStatusMetadataOnly matched by artist/title only. Diagnostic:
	// apply never acts on these.
	FavoriteStatusMetadataOnly = "metadata_only"
)

// FavoriteRow is one Apple Music favorite and what UDL will do with it.
type FavoriteRow struct {
	Index       int    `json:"index"`
	Status      string `json:"status"`
	Title       string `json:"title"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	ApplePath   string `json:"apple_path,omitempty"`
	AppleID     string `json:"apple_persistent_id,omitempty"`
	NavidromeID string `json:"navidrome_id,omitempty"`
	MatchedPath string `json:"matched_path,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// FavoriteCounts summarizes plan rows.
type FavoriteCounts struct {
	SourceTotal     int `json:"source_total"`
	Matched         int `json:"matched"`
	AlreadyStarred  int `json:"already_starred"`
	OutsideLibrary  int `json:"outside_library"`
	Missing         int `json:"missing"`
	Ambiguous       int `json:"ambiguous"`
	MetadataOnly    int `json:"metadata_only"`
	ServerStarred   int `json:"server_starred"`
	ExpectedStarred int `json:"expected_starred"`
}

// FavoritePlan is the immutable, checksummed migration plan.
type FavoritePlan struct {
	Version     string `json:"version"`
	GeneratedAt string `json:"generated_at"`
	// SourceFingerprint pins the Apple Music favorite set the plan was built
	// from, so an intervening change in Music is detected at apply time.
	SourceFingerprint string `json:"source_fingerprint"`
	// ServerFingerprint pins the Navidrome catalog and star state.
	ServerFingerprint string         `json:"server_fingerprint"`
	ServerURL         string         `json:"server_url"`
	Username          string         `json:"username"`
	MusicDir          string         `json:"music_dir"`
	Counts            FavoriteCounts `json:"counts"`
	Rows              []FavoriteRow  `json:"rows"`
	Blockers          []string       `json:"blockers"`
	Warnings          []string       `json:"warnings"`
	ChecksumSHA256    string         `json:"checksum_sha256"`
}

// Applicable reports whether apply may proceed.
func (p FavoritePlan) Applicable() bool { return len(p.Blockers) == 0 }

// StarIDs are the Navidrome IDs apply will star, in plan order.
func (p FavoritePlan) StarIDs() []string {
	out := []string{}
	for _, row := range p.Rows {
		if row.Status == FavoriteStatusMatched && row.NavidromeID != "" {
			out = append(out, row.NavidromeID)
		}
	}
	return out
}

// AppleFavorite is one favorited Apple Music track. Apple Music is read-only
// for this feature; nothing here ever writes back.
type AppleFavorite struct {
	PersistentID string
	Title        string
	Artist       string
	Album        string
	Path         string
}

// BuildFavoritePlan classifies every Apple Music favorite against the
// Navidrome catalog. It performs no mutation.
func BuildFavoritePlan(ctx context.Context, cfg Config, resolved Resolved, favorites []AppleFavorite, catalog []Song, now time.Time) (FavoritePlan, error) {
	if err := ctx.Err(); err != nil {
		return FavoritePlan{}, err
	}

	byPath := map[string][]Song{}
	byMetadata := map[string][]Song{}
	starred := map[string]bool{}
	for _, song := range catalog {
		if song.Path != "" {
			byPath[song.Path] = append(byPath[song.Path], song)
		}
		byMetadata[metadataKey(song.Artist, song.Title)] = append(byMetadata[metadataKey(song.Artist, song.Title)], song)
		if song.Starred {
			starred[song.ID] = true
		}
	}

	plan := FavoritePlan{
		Version:     FavoritePlanVersion,
		GeneratedAt: now.UTC().Format(time.RFC3339),
		ServerURL:   cfg.Server.URL,
		Username:    cfg.Server.Username,
		MusicDir:    resolved.MusicDir,
		Rows:        []FavoriteRow{},
		Blockers:    []string{},
		Warnings:    []string{},
	}
	plan.Counts.SourceTotal = len(favorites)
	plan.Counts.ServerStarred = len(starred)

	for index, favorite := range favorites {
		row := FavoriteRow{
			Index:     index + 1,
			Title:     strings.TrimSpace(favorite.Title),
			Artist:    strings.TrimSpace(favorite.Artist),
			Album:     strings.TrimSpace(favorite.Album),
			AppleID:   favorite.PersistentID,
			ApplePath: NormalizePath(favorite.Path),
		}
		switch {
		case row.ApplePath == "":
			row.Status = FavoriteStatusOutsideLibrary
			row.Detail = "the favorite has no local file (cloud-only tracks are out of scope)"
		case resolved.MusicDir != "" && !withinDir(resolved.MusicDir, row.ApplePath):
			row.Status = FavoriteStatusOutsideLibrary
			row.Detail = "the file lives outside the configured music directory"
		default:
			matches := byPath[row.ApplePath]
			switch len(matches) {
			case 1:
				row.NavidromeID = matches[0].ID
				row.MatchedPath = matches[0].Path
				if starred[matches[0].ID] {
					row.Status = FavoriteStatusAlreadyStarred
				} else {
					row.Status = FavoriteStatusMatched
				}
			case 0:
				row.Status = FavoriteStatusMissing
				row.Detail = "no Navidrome track has this path; rescan the library"
				// A metadata match is reported for diagnosis only. Acting on it
				// would star a different file than the user favorited.
				if candidates := byMetadata[metadataKey(row.Artist, row.Title)]; len(candidates) == 1 {
					row.Status = FavoriteStatusMetadataOnly
					row.MatchedPath = candidates[0].Path
					row.Detail = "matched by artist and title only; UDL will not star a path it cannot confirm"
				}
			default:
				row.Status = FavoriteStatusAmbiguous
				row.Detail = fmt.Sprintf("%d Navidrome tracks share this path", len(matches))
			}
		}
		plan.Rows = append(plan.Rows, row)
	}

	for _, row := range plan.Rows {
		switch row.Status {
		case FavoriteStatusMatched:
			plan.Counts.Matched++
		case FavoriteStatusAlreadyStarred:
			plan.Counts.AlreadyStarred++
		case FavoriteStatusOutsideLibrary:
			plan.Counts.OutsideLibrary++
		case FavoriteStatusMissing:
			plan.Counts.Missing++
		case FavoriteStatusAmbiguous:
			plan.Counts.Ambiguous++
		case FavoriteStatusMetadataOnly:
			plan.Counts.MetadataOnly++
		}
	}
	plan.Counts.ExpectedStarred = plan.Counts.ServerStarred + plan.Counts.Matched

	if strings.TrimSpace(cfg.Server.Username) == "" {
		plan.Blockers = append(plan.Blockers, "no Navidrome username is configured")
	}
	if len(catalog) == 0 {
		plan.Blockers = append(plan.Blockers,
			"the Navidrome library is empty; scan it before importing favorites")
	}
	if plan.Counts.Missing > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"%d favorites have no matching Navidrome track and will be skipped", plan.Counts.Missing))
	}
	if plan.Counts.Ambiguous > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"%d favorites match more than one Navidrome track and will be skipped", plan.Counts.Ambiguous))
	}
	if plan.Counts.MetadataOnly > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"%d favorites matched only by metadata; these are diagnostic and are never applied", plan.Counts.MetadataOnly))
	}

	plan.SourceFingerprint = favoriteFingerprint(favorites)
	plan.ServerFingerprint = catalogFingerprint(catalog)
	if err := SignFavoritePlan(&plan); err != nil {
		return FavoritePlan{}, err
	}
	return plan, nil
}

// FavoriteApplyResult records what apply actually changed.
type FavoriteApplyResult struct {
	BackupPath      string   `json:"backup_path,omitempty"`
	NewlyStarred    []string `json:"newly_starred"`
	AlreadyStarred  int      `json:"already_starred"`
	FinalStarred    int      `json:"final_starred"`
	ParityVerified  bool     `json:"parity_verified"`
	Compensated     []string `json:"compensated"`
	RecoveryCommand string   `json:"recovery_command,omitempty"`
	Message         string   `json:"message"`
}

// FavoriteAPI is the server surface apply needs.
type FavoriteAPI interface {
	Songs(ctx context.Context) ([]Song, error)
	Starred(ctx context.Context) ([]Song, error)
	Star(ctx context.Context, songID string) error
	Unstar(ctx context.Context, songID string) error
}

// BackupFunc creates a verified database backup before any star changes.
type BackupFunc func(ctx context.Context) (Backup, error)

// ApplyFavoritePlan revalidates the plan against both libraries, backs up the
// Navidrome database, stars exact matches, and verifies parity. On failure it
// un-stars only the rows this attempt added.
func ApplyFavoritePlan(ctx context.Context, cfg Config, resolved Resolved, plan FavoritePlan, favorites []AppleFavorite, api FavoriteAPI, backup BackupFunc) (FavoriteApplyResult, error) {
	result := FavoriteApplyResult{NewlyStarred: []string{}, Compensated: []string{}}
	if err := VerifyFavoritePlanChecksum(plan); err != nil {
		return result, err
	}
	if plan.Version != FavoritePlanVersion {
		return result, fmt.Errorf("favorite plan version %q is not supported; regenerate the plan", plan.Version)
	}
	if !plan.Applicable() {
		return result, fmt.Errorf("favorite plan has unresolved blockers: %s", strings.Join(plan.Blockers, "; "))
	}
	if !strings.EqualFold(strings.TrimSpace(plan.Username), strings.TrimSpace(cfg.Server.Username)) {
		return result, fmt.Errorf("the plan was built for account %q but the configured account is %q; regenerate the plan",
			plan.Username, cfg.Server.Username)
	}
	if plan.MusicDir != resolved.MusicDir {
		return result, fmt.Errorf("the plan was built for music directory %q but the configured directory is %q; regenerate the plan",
			plan.MusicDir, resolved.MusicDir)
	}
	if favoriteFingerprint(favorites) != plan.SourceFingerprint {
		return result, fmt.Errorf("Apple Music favorites changed since the plan was generated; regenerate the plan")
	}

	catalog, err := api.Songs(ctx)
	if err != nil {
		return result, err
	}
	if catalogFingerprint(catalog) != plan.ServerFingerprint {
		return result, fmt.Errorf("the Navidrome library changed since the plan was generated; regenerate the plan")
	}

	if backup == nil {
		return result, fmt.Errorf("a database backup is required before changing favorites")
	}
	created, err := backup(ctx)
	if err != nil {
		return result, fmt.Errorf("create a Navidrome database backup: %w", err)
	}
	result.BackupPath = created.Path
	if info, statErr := os.Stat(created.Path); statErr != nil || info.Size() == 0 {
		return result, fmt.Errorf("the Navidrome database backup at %s is missing or empty; refusing to change favorites", created.Path)
	}

	result.AlreadyStarred = plan.Counts.AlreadyStarred
	for _, id := range plan.StarIDs() {
		if err := ctx.Err(); err != nil {
			return compensate(ctx, api, result, err)
		}
		if err := api.Star(ctx, id); err != nil {
			return compensate(ctx, api, result, err)
		}
		result.NewlyStarred = append(result.NewlyStarred, id)
	}

	starred, err := api.Starred(ctx)
	if err != nil {
		return result, fmt.Errorf("verify favorite parity: %w", err)
	}
	result.FinalStarred = len(starred)

	starredIDs := map[string]struct{}{}
	for _, song := range starred {
		starredIDs[song.ID] = struct{}{}
	}
	missing := []string{}
	for _, row := range plan.Rows {
		if row.Status != FavoriteStatusMatched && row.Status != FavoriteStatusAlreadyStarred {
			continue
		}
		if _, ok := starredIDs[row.NavidromeID]; !ok {
			missing = append(missing, row.MatchedPath)
		}
	}
	if len(missing) > 0 {
		return result, fmt.Errorf("favorite parity check failed: %d expected tracks are not starred (for example %s); the backup is at %s",
			len(missing), missing[0], result.BackupPath)
	}
	result.ParityVerified = true
	result.Message = fmt.Sprintf("starred %d tracks; %d were already starred", len(result.NewlyStarred), result.AlreadyStarred)
	return result, nil
}

// compensate un-stars only what this attempt added. Anything starred before
// the run — including favorites made in Amperfy — is left alone.
func compensate(ctx context.Context, api FavoriteAPI, result FavoriteApplyResult, cause error) (FavoriteApplyResult, error) {
	// The failure may itself be a cancellation; compensation still has to run,
	// so it gets its own bounded context.
	cleanupCtx := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		cleanupCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
	}
	failed := []string{}
	for _, id := range result.NewlyStarred {
		if err := api.Unstar(cleanupCtx, id); err != nil {
			failed = append(failed, id)
			continue
		}
		result.Compensated = append(result.Compensated, id)
	}
	if len(failed) > 0 {
		result.RecoveryCommand = fmt.Sprintf(
			"stop the service, restore %s over the Navidrome database, then start it again", result.BackupPath)
		return result, fmt.Errorf(
			"favorite import failed (%w) and %d of %d new stars could not be undone; restore the backup at %s",
			cause, len(failed), len(result.NewlyStarred), result.BackupPath)
	}
	result.NewlyStarred = []string{}
	return result, fmt.Errorf("favorite import failed and was rolled back: %w", cause)
}

// SignFavoritePlan computes and stores the plan checksum.
func SignFavoritePlan(plan *FavoritePlan) error {
	plan.ChecksumSHA256 = ""
	sum, err := favoritePlanChecksum(*plan)
	if err != nil {
		return err
	}
	plan.ChecksumSHA256 = sum
	return nil
}

// VerifyFavoritePlanChecksum rejects a modified or unsigned plan.
func VerifyFavoritePlanChecksum(plan FavoritePlan) error {
	expected := strings.TrimSpace(plan.ChecksumSHA256)
	if expected == "" {
		return fmt.Errorf("favorite plan checksum is missing")
	}
	plan.ChecksumSHA256 = ""
	actual, err := favoritePlanChecksum(plan)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("favorite plan checksum mismatch; regenerate the plan")
	}
	return nil
}

func favoritePlanChecksum(plan FavoritePlan) (string, error) {
	plan.ChecksumSHA256 = ""
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode favorite plan: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// WriteFavoritePlan stores a plan for a later apply.
func WriteFavoritePlan(path string, plan FavoritePlan) error {
	payload, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode favorite plan: %w", err)
	}
	payload = append(payload, '\n')
	return AtomicWrite(path, payload, 0o600)
}

// ReadFavoritePlan loads and verifies a stored plan.
func ReadFavoritePlan(path string) (FavoritePlan, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return FavoritePlan{}, fmt.Errorf("read favorite plan %s: %w", path, err)
	}
	var plan FavoritePlan
	if err := json.Unmarshal(payload, &plan); err != nil {
		return FavoritePlan{}, fmt.Errorf("parse favorite plan %s: %w", path, err)
	}
	if err := VerifyFavoritePlanChecksum(plan); err != nil {
		return FavoritePlan{}, err
	}
	return plan, nil
}

func metadataKey(artist, title string) string {
	return strings.ToLower(strings.TrimSpace(artist) + "\x00" + strings.TrimSpace(title))
}

func favoriteFingerprint(favorites []AppleFavorite) string {
	keys := make([]string, 0, len(favorites))
	for _, favorite := range favorites {
		keys = append(keys, favorite.PersistentID+"\x00"+NormalizePath(favorite.Path))
	}
	sortStrings(keys)
	return hashString(strings.Join(keys, "\n"))
}

func catalogFingerprint(catalog []Song) string {
	keys := make([]string, 0, len(catalog))
	for _, song := range catalog {
		starred := "0"
		if song.Starred {
			starred = "1"
		}
		keys = append(keys, song.ID+"\x00"+song.Path+"\x00"+starred)
	}
	sortStrings(keys)
	return hashString(strings.Join(keys, "\n"))
}

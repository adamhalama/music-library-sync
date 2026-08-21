package playlistmirror

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/playlists"
)

const PlanVersion = 1

type Action string

const (
	ActionAdd     Action = "add"
	ActionRemove  Action = "remove"
	ActionMove    Action = "move"
	ActionKeep    Action = "keep"
	ActionBlocked Action = "blocked"
)

type Plan struct {
	Version           int                     `json:"version"`
	GeneratedAt       time.Time               `json:"generated_at"`
	JobID             string                  `json:"job_id"`
	ConfigFingerprint string                  `json:"config_fingerprint"`
	Direction         playlists.SyncDirection `json:"direction"`
	Source            PlaylistDescriptor      `json:"source"`
	Destination       PlaylistDescriptor      `json:"destination"`
	DestinationCreate bool                    `json:"destination_create"`
	Summary           Summary                 `json:"summary"`
	Rows              []PlanRow               `json:"rows"`
	Blockers          []string                `json:"blockers"`
	Preconditions     Preconditions           `json:"preconditions"`
	ChecksumSHA256    string                  `json:"checksum_sha256"`
}

type PlaylistDescriptor struct {
	Provider   Provider `json:"provider"`
	ID         string   `json:"id,omitempty"`
	Name       string   `json:"name"`
	Owner      string   `json:"owner,omitempty"`
	Smart      bool     `json:"smart,omitempty"`
	TrackCount int      `json:"track_count"`
}

type Summary struct {
	SourceTotal      int `json:"source_total"`
	DestinationTotal int `json:"destination_total"`
	FinalTotal       int `json:"final_total"`
	WillAdd          int `json:"will_add"`
	WillRemove       int `json:"will_remove"`
	WillMove         int `json:"will_move"`
	WillKeep         int `json:"will_keep"`
	Blocked          int `json:"blocked"`
}

type PlanRow struct {
	SourceIndex           int    `json:"source_index,omitempty"`
	DestinationIndex      int    `json:"destination_index,omitempty"`
	Artist                string `json:"artist,omitempty"`
	Title                 string `json:"title"`
	Album                 string `json:"album,omitempty"`
	Duration              string `json:"duration,omitempty"`
	RawPath               string `json:"raw_path"`
	NormalizedPath        string `json:"normalized_path"`
	SourceProviderID      string `json:"source_provider_id,omitempty"`
	DestinationProviderID string `json:"destination_provider_id,omitempty"`
	Action                Action `json:"action"`
	Blocker               string `json:"blocker,omitempty"`
}

type Preconditions struct {
	SourceProviderIDs      []string      `json:"source_provider_ids"`
	SourceNormalizedPaths  []string      `json:"source_normalized_paths"`
	DestinationProviderIDs []string      `json:"destination_provider_ids"`
	DestinationPaths       []string      `json:"destination_normalized_paths"`
	FinalProviderIDs       []string      `json:"final_destination_provider_ids"`
	Matched                []MatchedPair `json:"matched"`
}

type MatchedPair struct {
	NormalizedPath        string `json:"normalized_path"`
	SourceProviderID      string `json:"source_provider_id"`
	DestinationProviderID string `json:"destination_provider_id"`
}

type BuildRequest struct {
	Job               playlists.SyncJob
	Direction         playlists.SyncDirection
	Rekordbox         Playlist
	Navidrome         Playlist
	RekordboxCatalog  RekordboxCatalog
	NavidromeCatalog  NavidromeCatalog
	NavidromeUsername string
}

func BuildPlan(request BuildRequest, now time.Time) (Plan, error) {
	if !request.Direction.Valid() {
		return Plan{}, fmt.Errorf("unsupported playlist sync direction %q", request.Direction)
	}
	fingerprint, err := playlists.SyncJobFingerprint(request.Job)
	if err != nil {
		return Plan{}, err
	}
	source, destination := request.Rekordbox, request.Navidrome
	if request.Direction == playlists.DirectionNavidromeToRekordbox {
		source, destination = request.Navidrome, request.Rekordbox
	}
	plan := Plan{
		Version: PlanVersion, GeneratedAt: now.UTC(), JobID: request.Job.ID,
		ConfigFingerprint: fingerprint, Direction: request.Direction,
		Source: descriptor(source), Destination: descriptor(destination), DestinationCreate: destination.Missing,
		Rows: []PlanRow{}, Blockers: []string{},
		Preconditions: Preconditions{
			SourceProviderIDs: []string{}, SourceNormalizedPaths: []string{},
			DestinationProviderIDs: []string{}, DestinationPaths: []string{}, FinalProviderIDs: []string{}, Matched: []MatchedPair{},
		},
	}
	plan.Summary.SourceTotal = len(source.Tracks)
	plan.Summary.DestinationTotal = len(destination.Tracks)
	plan.Summary.FinalTotal = len(source.Tracks)
	if source.Missing {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("source %s playlist %q was not found", source.Provider, source.Name))
	} else if len(source.Tracks) == 0 {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("source %s playlist %q is empty; refusing to clear the destination", source.Provider, source.Name))
	}
	if destination.Provider == ProviderNavidrome && !destination.Missing {
		if destination.Smart {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("Navidrome playlist %q is UDL-managed and cannot be overwritten", destination.Name))
		}
		if owner, want := strings.TrimSpace(destination.Owner), strings.TrimSpace(request.NavidromeUsername); owner != "" && want != "" && !strings.EqualFold(owner, want) {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("Navidrome playlist %q is owned by %q, not the configured account %q", destination.Name, owner, want))
		}
	}

	destinationByID := map[string]int{}
	destinationPaths := map[string]int{}
	for index, track := range destination.Tracks {
		plan.Preconditions.DestinationProviderIDs = append(plan.Preconditions.DestinationProviderIDs, track.ProviderID)
		plan.Preconditions.DestinationPaths = append(plan.Preconditions.DestinationPaths, track.NormalizedPath)
		if _, exists := destinationByID[track.ProviderID]; !exists {
			destinationByID[track.ProviderID] = index
		} else {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("destination playlist repeats provider ID %q", track.ProviderID))
		}
		if track.NormalizedPath != "" {
			if previous := destinationPaths[track.NormalizedPath]; previous != 0 {
				plan.Blockers = append(plan.Blockers, fmt.Sprintf("destination rows %d and %d share normalized path %q", previous, track.Index, track.NormalizedPath))
			} else {
				destinationPaths[track.NormalizedPath] = track.Index
			}
		}
	}
	desiredIDs := make([]string, 0, len(source.Tracks))
	desiredSet := map[string]struct{}{}
	sourcePaths := map[string]int{}
	for _, track := range source.Tracks {
		plan.Preconditions.SourceProviderIDs = append(plan.Preconditions.SourceProviderIDs, track.ProviderID)
		plan.Preconditions.SourceNormalizedPaths = append(plan.Preconditions.SourceNormalizedPaths, track.NormalizedPath)
		row := PlanRow{
			SourceIndex: track.Index, Artist: track.Artist, Title: track.Title, Album: track.Album, Duration: track.Duration,
			RawPath: track.RawPath, NormalizedPath: track.NormalizedPath, SourceProviderID: track.ProviderID,
		}
		if track.NormalizedPath == "" {
			row.Action, row.Blocker = ActionBlocked, "source track does not expose a real path"
		} else if previous := sourcePaths[track.NormalizedPath]; previous != 0 {
			row.Action, row.Blocker = ActionBlocked, fmt.Sprintf("duplicate canonical source path also appears at row %d", previous)
		} else {
			sourcePaths[track.NormalizedPath] = track.Index
			matchID, count := matchDestination(request, track.NormalizedPath)
			switch {
			case count == 0:
				row.Action, row.Blocker = ActionBlocked, "no exact normalized real-path match exists in the destination library"
			case count > 1:
				row.Action, row.Blocker = ActionBlocked, fmt.Sprintf("%d destination library tracks share this normalized path", count)
			default:
				row.DestinationProviderID = matchID
				desiredIDs = append(desiredIDs, matchID)
				desiredSet[matchID] = struct{}{}
				plan.Preconditions.Matched = append(plan.Preconditions.Matched, MatchedPair{
					NormalizedPath: track.NormalizedPath, SourceProviderID: track.ProviderID, DestinationProviderID: matchID,
				})
				if currentIndex, exists := destinationByID[matchID]; !exists {
					row.Action = ActionAdd
				} else if currentIndex == len(desiredIDs)-1 {
					row.Action = ActionKeep
					row.DestinationIndex = currentIndex + 1
				} else {
					row.Action = ActionMove
					row.DestinationIndex = currentIndex + 1
				}
			}
		}
		if row.Action == ActionBlocked {
			plan.Blockers = append(plan.Blockers, fmt.Sprintf("source row %d (%s): %s", track.Index, track.Title, row.Blocker))
		}
		plan.Rows = append(plan.Rows, row)
	}
	plan.Preconditions.FinalProviderIDs = append(plan.Preconditions.FinalProviderIDs, desiredIDs...)
	for _, track := range destination.Tracks {
		if _, keep := desiredSet[track.ProviderID]; keep {
			continue
		}
		plan.Rows = append(plan.Rows, PlanRow{
			DestinationIndex: track.Index, Artist: track.Artist, Title: track.Title, Album: track.Album, Duration: track.Duration,
			RawPath: track.RawPath, NormalizedPath: track.NormalizedPath,
			DestinationProviderID: track.ProviderID, Action: ActionRemove,
		})
	}
	for _, row := range plan.Rows {
		switch row.Action {
		case ActionAdd:
			plan.Summary.WillAdd++
		case ActionRemove:
			plan.Summary.WillRemove++
		case ActionMove:
			plan.Summary.WillMove++
		case ActionKeep:
			plan.Summary.WillKeep++
		case ActionBlocked:
			plan.Summary.Blocked++
		}
	}
	plan.ChecksumSHA256 = ""
	checksum, err := planChecksum(plan)
	if err != nil {
		return Plan{}, err
	}
	plan.ChecksumSHA256 = checksum
	return plan, nil
}

func matchDestination(request BuildRequest, path string) (string, int) {
	if request.Direction == playlists.DirectionRekordboxToNavidrome {
		matches := request.NavidromeCatalog[path]
		if len(matches) == 0 {
			return "", 0
		}
		return matches[0].ID, len(matches)
	}
	matches := request.RekordboxCatalog[path]
	if len(matches) == 0 {
		return "", 0
	}
	return matches[0].ID, len(matches)
}

func descriptor(value Playlist) PlaylistDescriptor {
	return PlaylistDescriptor{
		Provider: value.Provider, ID: value.ID, Name: value.Name, Owner: value.Owner,
		Smart: value.Smart, TrackCount: len(value.Tracks),
	}
}

func VerifyPlan(plan Plan) error {
	if plan.Version != PlanVersion {
		return fmt.Errorf("playlist mirror plan version must be %d; regenerate the plan", PlanVersion)
	}
	if !plan.Direction.Valid() {
		return errors.New("playlist mirror plan direction is invalid")
	}
	if !playlists.ValidID(plan.JobID) {
		return errors.New("playlist mirror plan job_id is invalid")
	}
	expected := plan.ChecksumSHA256
	copyPlan := plan
	copyPlan.ChecksumSHA256 = ""
	actual, err := planChecksum(copyPlan)
	if err != nil {
		return err
	}
	if expected == "" || expected != actual {
		return errors.New("playlist mirror plan checksum does not match content; regenerate the plan")
	}
	return nil
}

func VerifyPlanApplicable(plan Plan) error {
	if err := VerifyPlan(plan); err != nil {
		return err
	}
	if len(plan.Blockers) > 0 {
		return fmt.Errorf("playlist mirror plan has %d blocker(s); regenerate after resolving them", len(plan.Blockers))
	}
	return nil
}

func WritePlan(path string, plan Plan) error {
	if err := VerifyPlan(plan); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode playlist mirror plan: %w", err)
	}
	return atomicWrite(path, append(payload, '\n'), 0o600)
}

func ReadPlan(path string) (Plan, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, fmt.Errorf("read playlist mirror plan %s: %w", path, err)
	}
	var plan Plan
	if err := json.Unmarshal(payload, &plan); err != nil {
		return Plan{}, fmt.Errorf("parse playlist mirror plan %s: %w", path, err)
	}
	if err := VerifyPlan(plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func PlanDirectory(stateDir string) (string, error) {
	if strings.TrimSpace(stateDir) == "" {
		stateDir = config.DefaultStateDir()
	}
	expanded, err := config.ExpandPath(stateDir)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		return "", errors.New("state_dir must resolve to an absolute path")
	}
	return filepath.Join(expanded, "playlist-sync", "plans"), nil
}

func planChecksum(plan Plan) (string, error) {
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode playlist mirror plan checksum: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func atomicWrite(path string, payload []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".playlist-mirror-plan-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return nil
}

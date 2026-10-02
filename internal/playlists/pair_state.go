package playlists

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
)

const PairStateVersion = 1

type SyncDirection string

const (
	DirectionRekordboxToNavidrome SyncDirection = "rekordbox-to-navidrome"
	DirectionNavidromeToRekordbox SyncDirection = "navidrome-to-rekordbox"
)

func (direction SyncDirection) Valid() bool {
	switch direction {
	case DirectionRekordboxToNavidrome, DirectionNavidromeToRekordbox:
		return true
	default:
		return false
	}
}

type ProviderBinding struct {
	PlaylistID   string `json:"playlist_id"`
	PlaylistName string `json:"playlist_name"`
}

// PairState is operational state, not configuration. It records selectors
// resolved during the last verified apply without silently pinning those IDs
// into playlists.yaml.
type PairState struct {
	Version           int             `json:"version"`
	JobID             string          `json:"job_id"`
	ConfigFingerprint string          `json:"config_fingerprint"`
	Rekordbox         ProviderBinding `json:"rekordbox"`
	Navidrome         ProviderBinding `json:"navidrome"`
	LastDirection     SyncDirection   `json:"last_direction"`
	LastVerifiedAt    time.Time       `json:"last_verified_at"`
	FinalPathChecksum string          `json:"final_path_checksum"`
	ChecksumSHA256    string          `json:"checksum_sha256"`
}

func SyncJobFingerprint(job SyncJob) (string, error) {
	copyJob := job
	normalizeSyncJob(&copyJob)
	if err := validateSyncJob(copyJob); err != nil {
		return "", err
	}
	payload, err := json.Marshal(copyJob)
	if err != nil {
		return "", fmt.Errorf("encode sync job fingerprint: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func PairStatePath(stateDir, jobID string) (string, error) {
	jobID = strings.TrimSpace(jobID)
	if !ValidID(jobID) {
		return "", fmt.Errorf("sync job id %q is not safe for a state path", jobID)
	}
	expanded, err := config.ExpandPath(firstNonEmpty(stateDir, config.DefaultStateDir()))
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		return "", errors.New("state_dir must resolve to an absolute path")
	}
	return filepath.Join(expanded, "playlist-sync", "jobs", jobID+".json"), nil
}

func WritePairState(stateDir string, state PairState) (string, error) {
	state.Version = PairStateVersion
	state.JobID = strings.TrimSpace(state.JobID)
	state.ChecksumSHA256 = ""
	checksum, err := pairStateChecksum(state)
	if err != nil {
		return "", err
	}
	state.ChecksumSHA256 = checksum
	if err := ValidatePairState(state); err != nil {
		return "", err
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode playlist sync state: %w", err)
	}
	path, err := PairStatePath(stateDir, state.JobID)
	if err != nil {
		return "", err
	}
	if err := atomicWrite(path, append(payload, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func LoadPairState(stateDir, jobID string) (PairState, error) {
	path, err := PairStatePath(stateDir, jobID)
	if err != nil {
		return PairState{}, err
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return PairState{}, err
	}
	var state PairState
	if err := json.Unmarshal(payload, &state); err != nil {
		return PairState{}, fmt.Errorf("parse playlist sync state %s: %w", path, err)
	}
	if err := ValidatePairState(state); err != nil {
		return PairState{}, fmt.Errorf("invalid playlist sync state %s: %w", path, err)
	}
	return state, nil
}

func ValidatePairState(state PairState) error {
	if state.Version != PairStateVersion {
		return fmt.Errorf("state version must be %d", PairStateVersion)
	}
	if !ValidID(state.JobID) {
		return errors.New("state job_id is invalid")
	}
	if strings.TrimSpace(state.ConfigFingerprint) == "" {
		return errors.New("state config_fingerprint must not be empty")
	}
	if strings.TrimSpace(state.Rekordbox.PlaylistID) == "" || strings.TrimSpace(state.Rekordbox.PlaylistName) == "" {
		return errors.New("state rekordbox binding must include playlist_id and playlist_name")
	}
	if strings.TrimSpace(state.Navidrome.PlaylistID) == "" || strings.TrimSpace(state.Navidrome.PlaylistName) == "" {
		return errors.New("state navidrome binding must include playlist_id and playlist_name")
	}
	if !state.LastDirection.Valid() {
		return errors.New("state last_direction is invalid")
	}
	if state.LastVerifiedAt.IsZero() {
		return errors.New("state last_verified_at must be set")
	}
	if strings.TrimSpace(state.FinalPathChecksum) == "" {
		return errors.New("state final_path_checksum must not be empty")
	}
	expected, err := pairStateChecksum(state)
	if err != nil {
		return err
	}
	if state.ChecksumSHA256 == "" || state.ChecksumSHA256 != expected {
		return errors.New("state checksum does not match content")
	}
	return nil
}

// PairStateForJob returns a trusted saved binding only when it belongs to the
// current job configuration. A changed selector invalidates state without
// mutating the configuration file.
func PairStateForJob(state PairState, job SyncJob) (PairState, bool, error) {
	fingerprint, err := SyncJobFingerprint(job)
	if err != nil {
		return PairState{}, false, err
	}
	if err := ValidatePairState(state); err != nil {
		return PairState{}, false, err
	}
	if state.JobID != strings.TrimSpace(job.ID) || state.ConfigFingerprint != fingerprint {
		return PairState{}, false, nil
	}
	return state, true, nil
}

func pairStateChecksum(state PairState) (string, error) {
	copyState := state
	copyState.ChecksumSHA256 = ""
	payload, err := json.Marshal(copyState)
	if err != nil {
		return "", fmt.Errorf("encode playlist sync state checksum: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeSyncJob(job *SyncJob) {
	job.ID = strings.TrimSpace(job.ID)
	job.Rekordbox.Playlist = strings.TrimSpace(job.Rekordbox.Playlist)
	job.Rekordbox.PlaylistID = strings.TrimSpace(job.Rekordbox.PlaylistID)
	job.Navidrome.Playlist = strings.TrimSpace(job.Navidrome.Playlist)
	job.Navidrome.PlaylistID = strings.TrimSpace(job.Navidrome.PlaylistID)
}

func validateSyncJob(job SyncJob) error {
	return Validate(Config{Version: ConfigVersion, SyncJobs: []SyncJob{job}})
}

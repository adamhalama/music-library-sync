package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/playlistmirror"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
)

type PlaylistMirrorNavidrome interface {
	playlistmirror.NavidromeReader
	ReplacePlaylist(context.Context, string, string, []string) (navidrome.PlaylistMutationResult, error)
}

type PlaylistMirrorUseCase struct {
	Bridge          RekordboxBridge
	Navidrome       PlaylistMirrorNavidrome
	Now             func() time.Time
	CheckClosed     func(context.Context, string) error
	BackupRekordbox func(context.Context, string) (string, error)
	BackupNavidrome func(context.Context) (string, error)
}

type PlaylistMirrorPlanRequest struct {
	Config         config.Config
	PlaylistConfig playlists.Config
	JobID          string
	Direction      playlists.SyncDirection
	RekordboxDBDir string
	NavidromeUser  string
	OutPath        string
}

type PlaylistMirrorPlanResult struct {
	Plan     playlistmirror.Plan `json:"plan"`
	PlanPath string              `json:"plan_path"`
}

type PlaylistMirrorApplyRequest struct {
	Config         config.Config
	PlaylistConfig playlists.Config
	Plan           playlistmirror.Plan
	RekordboxDBDir string
	NavidromeUser  string
	DryRun         bool
}

type PlaylistMirrorApplyResult struct {
	DryRun        bool                             `json:"dry_run"`
	NoOp          bool                             `json:"no_op"`
	BackupPath    string                           `json:"backup_path,omitempty"`
	PairStatePath string                           `json:"pair_state_path,omitempty"`
	Rekordbox     bridge.ApplyResponse             `json:"rekordbox,omitempty"`
	Navidrome     navidrome.PlaylistMutationResult `json:"navidrome,omitempty"`
}

func (u PlaylistMirrorUseCase) Plan(ctx context.Context, request PlaylistMirrorPlanRequest) (PlaylistMirrorPlanResult, error) {
	job, ok := request.PlaylistConfig.SyncJob(strings.TrimSpace(request.JobID))
	if !ok {
		return PlaylistMirrorPlanResult{}, fmt.Errorf("playlist sync job %q is not configured", request.JobID)
	}
	plan, err := u.buildLivePlan(ctx, request.Config, job, request.Direction, request.RekordboxDBDir, request.NavidromeUser, u.now())
	if err != nil {
		return PlaylistMirrorPlanResult{}, err
	}
	outPath := strings.TrimSpace(request.OutPath)
	if outPath == "" {
		dir, err := playlistmirror.PlanDirectory(request.Config.Defaults.StateDir)
		if err != nil {
			return PlaylistMirrorPlanResult{}, err
		}
		outPath = filepath.Join(dir, fmt.Sprintf("%s-%s.json", job.ID, u.now().UTC().Format("20060102-150405")))
	}
	if err := playlistmirror.WritePlan(outPath, plan); err != nil {
		return PlaylistMirrorPlanResult{}, err
	}
	return PlaylistMirrorPlanResult{Plan: plan, PlanPath: outPath}, nil
}

func (u PlaylistMirrorUseCase) Apply(ctx context.Context, request PlaylistMirrorApplyRequest) (PlaylistMirrorApplyResult, error) {
	if err := playlistmirror.VerifyPlanApplicable(request.Plan); err != nil {
		return PlaylistMirrorApplyResult{}, err
	}
	job, ok := request.PlaylistConfig.SyncJob(request.Plan.JobID)
	if !ok {
		return PlaylistMirrorApplyResult{}, fmt.Errorf("playlist sync job %q is no longer configured", request.Plan.JobID)
	}
	fingerprint, err := playlists.SyncJobFingerprint(job)
	if err != nil {
		return PlaylistMirrorApplyResult{}, err
	}
	if fingerprint != request.Plan.ConfigFingerprint {
		return PlaylistMirrorApplyResult{}, errors.New("playlist sync job configuration changed since planning; regenerate the plan")
	}
	live, err := u.buildLivePlan(ctx, request.Config, job, request.Plan.Direction, request.RekordboxDBDir, request.NavidromeUser, request.Plan.GeneratedAt)
	if err != nil {
		return PlaylistMirrorApplyResult{}, err
	}
	if err := playlistmirror.VerifyPlanApplicable(live); err != nil {
		return PlaylistMirrorApplyResult{}, fmt.Errorf("live playlist state is no longer applicable: %w", err)
	}
	if !sameLivePlan(request.Plan, live) {
		return PlaylistMirrorApplyResult{}, errors.New("source or destination changed since planning; regenerate the plan")
	}
	result := PlaylistMirrorApplyResult{DryRun: request.DryRun, NoOp: isNoOp(request.Plan)}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if request.DryRun {
		return result, nil
	}
	if result.NoOp {
		state := pairStateFromApply(request.Plan, live, fingerprint, u.now())
		result.PairStatePath, err = playlists.WritePairState(request.Config.Defaults.StateDir, state)
		if err != nil {
			return result, fmt.Errorf("verified parity but pair state could not be saved: %w", err)
		}
		return result, nil
	}
	if request.Plan.Direction == playlists.DirectionRekordboxToNavidrome {
		if u.BackupNavidrome == nil {
			return result, errors.New("Navidrome backup is not configured")
		}
		result.BackupPath, err = u.BackupNavidrome(ctx)
		if err != nil {
			return result, fmt.Errorf("create Navidrome backup: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("canceled after Navidrome backup %s and before playlist write: %w", result.BackupPath, err)
		}
		result.Navidrome, err = u.Navidrome.ReplacePlaylist(ctx, request.Plan.Destination.ID, request.Plan.Destination.Name, request.Plan.Preconditions.FinalProviderIDs)
		if err != nil {
			return result, fmt.Errorf("replace Navidrome playlist after backup %s: %w", result.BackupPath, err)
		}
		live.Destination.ID = result.Navidrome.Playlist.ID
		live.Destination.Name = result.Navidrome.Playlist.Name
	} else {
		if u.BackupRekordbox == nil {
			return result, errors.New("Rekordbox backup is not configured")
		}
		result.BackupPath, err = u.BackupRekordbox(ctx, request.RekordboxDBDir)
		if err != nil {
			return result, fmt.Errorf("create Rekordbox backup: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("canceled after Rekordbox backup %s and before playlist write: %w", result.BackupPath, err)
		}
		result.Rekordbox, err = u.Bridge.Apply(ctx, bridge.ApplyRequest{
			DBDir: request.RekordboxDBDir, TargetPlaylistID: request.Plan.Destination.ID,
			TargetPlaylistName: request.Plan.Destination.Name, CreatePlaylistIfMissing: request.Plan.DestinationCreate,
			ExpectedCurrentContentIDs: request.Plan.Preconditions.DestinationProviderIDs,
			FinalContentIDs:           request.Plan.Preconditions.FinalProviderIDs,
		})
		if err != nil {
			return result, fmt.Errorf("replace Rekordbox playlist after backup %s: %w", result.BackupPath, err)
		}
		if !reflect.DeepEqual(result.Rekordbox.FinalContentIDs, request.Plan.Preconditions.FinalProviderIDs) {
			return result, fmt.Errorf("Rekordbox post-apply verification failed after backup %s: final order differs from plan", result.BackupPath)
		}
		live.Destination.ID = result.Rekordbox.PlaylistID
		live.Destination.Name = result.Rekordbox.PlaylistName
	}

	state := pairStateFromApply(request.Plan, live, fingerprint, u.now())
	result.PairStatePath, err = playlists.WritePairState(request.Config.Defaults.StateDir, state)
	if err != nil {
		return result, fmt.Errorf("destination reached verified parity but pair state could not be saved after backup %s: %w", result.BackupPath, err)
	}
	return result, nil
}

func (u PlaylistMirrorUseCase) buildLivePlan(ctx context.Context, main config.Config, job playlists.SyncJob, direction playlists.SyncDirection, dbDir, navUser string, now time.Time) (playlistmirror.Plan, error) {
	if u.Bridge == nil || u.Navidrome == nil {
		return playlistmirror.Plan{}, errors.New("playlist mirror providers are not configured")
	}
	if strings.TrimSpace(dbDir) == "" {
		return playlistmirror.Plan{}, errors.New("Rekordbox database directory is not configured")
	}
	if err := u.checkClosed(ctx, dbDir); err != nil {
		return playlistmirror.Plan{}, err
	}
	inspect, err := u.Bridge.Inspect(ctx, dbDir)
	if err != nil {
		return playlistmirror.Plan{}, err
	}
	var binding *playlists.PairState
	if saved, err := playlists.LoadPairState(main.Defaults.StateDir, job.ID); err == nil {
		if trusted, ok, err := playlists.PairStateForJob(saved, job); err != nil {
			return playlistmirror.Plan{}, err
		} else if ok {
			binding = &trusted
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return playlistmirror.Plan{}, err
	}
	var rbBinding, ndBinding *playlists.ProviderBinding
	if binding != nil {
		rbBinding, ndBinding = &binding.Rekordbox, &binding.Navidrome
	}
	rbMissing := direction == playlists.DirectionNavidromeToRekordbox
	ndMissing := direction == playlists.DirectionRekordboxToNavidrome
	rekordboxPlaylist, err := playlistmirror.ReadRekordbox(inspect, job.Rekordbox, rbBinding, rbMissing)
	if err != nil {
		return playlistmirror.Plan{}, err
	}
	navidromePlaylist, err := playlistmirror.ReadNavidrome(ctx, u.Navidrome, job.Navidrome, ndBinding, ndMissing)
	if err != nil {
		return playlistmirror.Plan{}, err
	}
	rbCatalog, err := playlistmirror.IndexRekordboxCatalog(inspect.Contents)
	if err != nil {
		return playlistmirror.Plan{}, err
	}
	ndCatalog, err := playlistmirror.ReadNavidromeCatalog(ctx, u.Navidrome)
	if err != nil {
		return playlistmirror.Plan{}, err
	}
	return playlistmirror.BuildPlan(playlistmirror.BuildRequest{
		Job: job, Direction: direction, Rekordbox: rekordboxPlaylist, Navidrome: navidromePlaylist,
		RekordboxCatalog: rbCatalog, NavidromeCatalog: ndCatalog, NavidromeUsername: navUser,
	}, now)
}

func (u PlaylistMirrorUseCase) now() time.Time {
	if u.Now != nil {
		return u.Now().UTC()
	}
	return time.Now().UTC()
}

func (u PlaylistMirrorUseCase) checkClosed(ctx context.Context, dbDir string) error {
	if u.CheckClosed != nil {
		return u.CheckClosed(ctx, dbDir)
	}
	return fmt.Errorf("Rekordbox closed-state check is not configured for %s", dbDir)
}

func sameLivePlan(planned, live playlistmirror.Plan) bool {
	return planned.Direction == live.Direction &&
		planned.Source.ID == live.Source.ID && planned.Source.Name == live.Source.Name &&
		planned.Destination.ID == live.Destination.ID && planned.Destination.Name == live.Destination.Name &&
		planned.DestinationCreate == live.DestinationCreate &&
		reflect.DeepEqual(planned.Preconditions, live.Preconditions)
}

func isNoOp(plan playlistmirror.Plan) bool {
	return !plan.DestinationCreate && plan.Summary.WillAdd == 0 && plan.Summary.WillRemove == 0 && plan.Summary.WillMove == 0
}

func pairStateFromApply(plan playlistmirror.Plan, live playlistmirror.Plan, fingerprint string, now time.Time) playlists.PairState {
	rb := plan.Source
	nd := live.Destination
	if plan.Direction == playlists.DirectionNavidromeToRekordbox {
		rb, nd = live.Destination, plan.Source
	}
	sum := sha256.Sum256([]byte(strings.Join(plan.Preconditions.SourceNormalizedPaths, "\x00")))
	return playlists.PairState{
		JobID: plan.JobID, ConfigFingerprint: fingerprint,
		Rekordbox:     playlists.ProviderBinding{PlaylistID: rb.ID, PlaylistName: rb.Name},
		Navidrome:     playlists.ProviderBinding{PlaylistID: nd.ID, PlaylistName: nd.Name},
		LastDirection: plan.Direction, LastVerifiedAt: now.UTC(), FinalPathChecksum: hex.EncodeToString(sum[:]),
	}
}

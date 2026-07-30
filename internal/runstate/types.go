package runstate

import (
	"fmt"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/output"
)

type StatusFilter string

const (
	FilterAll         StatusFilter = "all"
	FilterWillSync    StatusFilter = "will_sync"
	FilterMissingNew  StatusFilter = "missing_new"
	FilterKnownGap    StatusFilter = "known_gap"
	FilterAlreadyHave StatusFilter = "already_have"
	FilterInRun       StatusFilter = "in_run"
	FilterRemaining   StatusFilter = "remaining"
	FilterDownloaded  StatusFilter = "downloaded"
	FilterSkipped     StatusFilter = "skipped"
	FilterFailed      StatusFilter = "failed"
)

type TrackPlanClass string

const (
	TrackPlanClassNew         TrackPlanClass = "new"
	TrackPlanClassKnownGap    TrackPlanClass = "known_gap"
	TrackPlanClassAlreadyHave TrackPlanClass = "already_have"
)

type TrackRunScope string

const (
	TrackRunScopeIncluded TrackRunScope = "included"
	TrackRunScopeExcluded TrackRunScope = "excluded"
	TrackRunScopeLocked   TrackRunScope = "locked"
)

type TrackRuntimeStatus string

const (
	TrackStatusIdle        TrackRuntimeStatus = "idle"
	TrackStatusQueued      TrackRuntimeStatus = "queued"
	TrackStatusDownloading TrackRuntimeStatus = "downloading"
	TrackStatusDownloaded  TrackRuntimeStatus = "downloaded"
	TrackStatusSkipped     TrackRuntimeStatus = "skipped"
	TrackStatusFailed      TrackRuntimeStatus = "failed"
)

type SourceLifecycle string

const (
	SourceLifecycleIdle      SourceLifecycle = "idle"
	SourceLifecyclePreflight SourceLifecycle = "preflight"
	SourceLifecycleRunning   SourceLifecycle = "running"
	SourceLifecycleFinished  SourceLifecycle = "finished"
	SourceLifecycleFailed    SourceLifecycle = "failed"
)

type PlanTrackRow struct {
	SourceID          string               `json:"source_id"`
	SourceLabel       string               `json:"source_label"`
	RemoteID          string               `json:"remote_id"`
	Title             string               `json:"title"`
	Index             int                  `json:"index"`
	Toggleable        bool                 `json:"toggleable"`
	PlanStatus        engine.PlanRowStatus `json:"plan_status"`
	PlanClass         TrackPlanClass       `json:"plan_class"`
	SelectedByDefault bool                 `json:"selected_by_default"`
}

type TrackRow struct {
	SourceID        string               `json:"source_id"`
	SourceLabel     string               `json:"source_label"`
	RemoteID        string               `json:"remote_id"`
	Title           string               `json:"title"`
	Index           int                  `json:"index"`
	ExecutionSlot   int                  `json:"execution_slot"`
	Toggleable      bool                 `json:"toggleable"`
	PlanStatus      engine.PlanRowStatus `json:"plan_status"`
	PlanClass       TrackPlanClass       `json:"plan_class"`
	Selected        bool                 `json:"selected"`
	RunScope        TrackRunScope        `json:"run_scope"`
	RuntimeStatus   TrackRuntimeStatus   `json:"runtime_status"`
	StatusLabel     string               `json:"status_label"`
	FailureDetail   string               `json:"failure_detail,omitempty"`
	ProgressKnown   bool                 `json:"progress_known"`
	ProgressPercent float64              `json:"progress_percent"`
}

type ActivityEntry struct {
	Timestamp time.Time    `json:"timestamp"`
	Level     output.Level `json:"level"`
	Message   string       `json:"message"`
	SourceID  string       `json:"source_id"`
}

type SourceSnapshot struct {
	Lifecycle SourceLifecycle `json:"lifecycle"`
	Confirmed bool            `json:"confirmed"`
	Rows      []TrackRow      `json:"rows"`
	Activity  []ActivityEntry `json:"activity"`
}

type FailureState struct {
	SourceID       string `json:"source_id"`
	Message        string `json:"message"`
	ExitCode       *int   `json:"exit_code,omitempty"`
	TimedOut       bool   `json:"timed_out"`
	Interrupted    bool   `json:"interrupted"`
	StdoutTail     string `json:"stdout_tail,omitempty"`
	StderrTail     string `json:"stderr_tail,omitempty"`
	FailureLogPath string `json:"failure_log_path,omitempty"`
}

func RuntimeStatusFromPlanStatus(status engine.PlanRowStatus) TrackRuntimeStatus {
	if status == engine.PlanRowAlreadyDownloaded {
		return TrackStatusIdle
	}
	return TrackStatusQueued
}

func TrackPlanClassFromPlanStatus(status engine.PlanRowStatus) TrackPlanClass {
	switch status {
	case engine.PlanRowMissingKnownGap:
		return TrackPlanClassKnownGap
	case engine.PlanRowAlreadyDownloaded:
		return TrackPlanClassAlreadyHave
	default:
		return TrackPlanClassNew
	}
}

func TrackRunScopeForRow(toggleable, selected bool) TrackRunScope {
	if !toggleable {
		return TrackRunScopeLocked
	}
	if selected {
		return TrackRunScopeIncluded
	}
	return TrackRunScopeExcluded
}

func DisplayRowFromPlanRow(row PlanTrackRow, selected bool) TrackRow {
	runtimeStatus := RuntimeStatusFromPlanStatus(row.PlanStatus)
	return TrackRow{
		SourceID: row.SourceID, SourceLabel: row.SourceLabel, RemoteID: row.RemoteID,
		Title: row.Title, Index: row.Index, Toggleable: row.Toggleable,
		PlanStatus: row.PlanStatus, PlanClass: row.PlanClass,
		Selected:      row.Toggleable && selected,
		RunScope:      TrackRunScopeForRow(row.Toggleable, selected),
		RuntimeStatus: runtimeStatus,
		StatusLabel:   TrackStatusLabel(runtimeStatus, 0, false, ""),
	}
}

func TrackStatusLabel(status TrackRuntimeStatus, percent float64, progressKnown bool, failureDetail string) string {
	switch status {
	case TrackStatusIdle:
		return "idle"
	case TrackStatusQueued:
		return "pending"
	case TrackStatusDownloading:
		if progressKnown {
			return fmt.Sprintf("downloading %.0f%%", percent)
		}
		return "downloading"
	case TrackStatusDownloaded:
		return "downloaded"
	case TrackStatusSkipped:
		if detail := strings.TrimSpace(failureDetail); detail != "" {
			return "skipped: " + detail
		}
		return "skipped"
	case TrackStatusFailed:
		if detail := strings.TrimSpace(failureDetail); detail != "" {
			return "failed: " + detail
		}
		return "failed"
	default:
		return string(status)
	}
}

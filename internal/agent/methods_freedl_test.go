package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/freedl"
)

func writeAgentFreeDLConfig(t *testing.T, dir string, main config.Config) (string, freedl.Job) {
	t.Helper()
	job := freedl.Job{
		ID: "free-job", Enabled: true,
		SourceURL:  "https://soundcloud.com/user/likes",
		LibraryDir: filepath.Join(dir, "music"),
		BufferDir:  filepath.Join(dir, "buffer"),
		BackupDir:  filepath.Join(dir, "backups"),
		LogDir:     filepath.Join(dir, "logs"),
		StateFile:  "free-job.sync.scdl",
		PlanLimit:  10, DownloadOrder: freedl.DefaultDownloadOrder,
		TargetFormat:  freedl.DefaultTargetFormat,
		MinMatchScore: freedl.DefaultMinMatchScore,
		AmbiguityGap:  freedl.DefaultAmbiguityGap,
	}
	cfg := freedl.Config{
		Version:  1,
		Defaults: freedl.DefaultConfig(main).Defaults,
		Jobs:     []freedl.Job{job},
	}
	path := filepath.Join(dir, "udl.freedl.yaml")
	if err := freedl.SaveSingleFile(path, cfg, main); err != nil {
		t.Fatal(err)
	}
	return path, job
}

func TestFreeDLConfigAndCapturePathsRemainSessionScoped(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	main, err := config.LoadSingleFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	freePath, job := writeAgentFreeDLConfig(t, dir, main)
	server := &Server{WorkingDir: dir, ConfigPath: mainPath, FreeDLConfigPath: freePath}

	value, rpcErr := server.readFreeDLConfig()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	read := value.(freeDLConfigResult)
	if read.Path != freePath || len(read.Config.Jobs) != 1 || !strings.Contains(read.Content, "free-job") {
		t.Fatalf("unexpected Free DL config read: %+v", read)
	}
	read.Config.Jobs[0].PlanLimit = 4
	if _, rpcErr := server.writeFreeDLConfig(mustJSON(t, map[string]any{"config": read.Config})); rpcErr != nil {
		t.Fatal(rpcErr)
	}

	authorized, rpcErr := authorizeCapturePlan(freedl.CapturePlan{
		RunID: "capture-1", Job: job, BufferRoot: "/tmp/untrusted", LogDir: "/tmp/untrusted",
	}, job)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if authorized.BufferRoot != filepath.Join(job.BufferDir, "capture-1") ||
		authorized.LogDir != filepath.Join(job.LogDir, "capture-1") {
		t.Fatalf("client paths were not replaced: %+v", authorized)
	}
	if _, rpcErr := authorizeCapturePlan(freedl.CapturePlan{RunID: "../escape"}, job); rpcErr == nil {
		t.Fatal("path-traversing run ID was accepted")
	}
	runCfg, err := buildFreeDLCaptureConfig(main, authorized)
	if err != nil {
		t.Fatal(err)
	}
	source := runCfg.Sources[0]
	if source.Adapter.Kind != "scdl-freedl" || source.StateFile != "capture.sync.scdl" ||
		source.TargetDir != filepath.Join(job.BufferDir, "capture-1", "downloads") {
		t.Fatalf("synthetic capture source drifted from the TUI workflow: %+v", source)
	}
}

func TestFreeDLPlanStreamsEventsAndPreservesRemoteIDOverrides(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	main, err := config.LoadSingleFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	freePath, job := writeAgentFreeDLConfig(t, dir, main)
	plan := freedl.CapturePlan{
		RunID: "capture-1", Job: job, BufferRoot: filepath.Join(job.BufferDir, "capture-1"),
		LogDir: filepath.Join(job.LogDir, "capture-1"),
		Rows:   []freedl.PlanRow{{RemoteID: "remote-1", Title: "One", Selectable: true, Selected: true}},
	}
	ops := &FreeDLOperations{
		BuildCapturePlanProgress: func(context.Context, config.Config, freedl.Job) <-chan freedl.CapturePlanEvent {
			events := make(chan freedl.CapturePlanEvent, 3)
			events <- freedl.CapturePlanEvent{Kind: freedl.CapturePlanEventStarted, Plan: plan}
			events <- freedl.CapturePlanEvent{Kind: freedl.CapturePlanEventRow, Row: plan.Rows[0]}
			events <- freedl.CapturePlanEvent{Kind: freedl.CapturePlanEventDone, Plan: plan}
			close(events)
			return events
		},
	}
	output := &synchronizedBuffer{}
	server := &Server{
		Conn: NewConn(strings.NewReader(""), output), Runs: NewRunRegistry(),
		WorkingDir: dir, ConfigPath: mainPath, FreeDLConfigPath: freePath,
		FreeDLOps: ops, runContext: context.Background(),
	}
	value, rpcErr := server.startFreeDLPlan(mustJSON(t, freeDLPlanParams{
		JobID: job.ID, SelectionOverrides: map[string]bool{"remote-1": false},
	}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if value.(map[string]string)["run_id"] == "" {
		t.Fatal("planning did not return a run ID")
	}
	waitForNotifications(t, output, 4)
	frames := strings.Split(strings.TrimSpace(output.String()), "\n")
	sawOverriddenRow := false
	for _, line := range frames {
		var frame envelope
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatal(err)
		}
		if frame.Method != "freedl.planEvent" {
			continue
		}
		var notification freeDLPlanEventParams
		if err := json.Unmarshal(frame.Params, &notification); err != nil {
			t.Fatal(err)
		}
		if notification.Event.Row != nil && notification.Event.Row.RemoteID == "remote-1" {
			sawOverriddenRow = true
			if notification.Event.Row.Selected {
				t.Fatal("streamed re-merge discarded the selection override")
			}
		}
		if notification.Event.Plan != nil && len(notification.Event.Plan.Rows) > 0 &&
			notification.Event.Plan.Rows[0].Selected {
			t.Fatal("plan event discarded the selection override")
		}
	}
	if !sawOverriddenRow {
		t.Fatal("missing streamed plan row")
	}
}

func TestFreeDLPromotionApplyRebuildsUntrustedPlan(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	main, err := config.LoadSingleFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	freePath, job := writeAgentFreeDLConfig(t, dir, main)
	applied := make(chan freedl.PromotionPlan, 1)
	ops := &FreeDLOperations{
		BuildPromotionPlan: func(context.Context, freedl.Job, string, string) (freedl.PromotionPlan, error) {
			return freedl.PromotionPlan{
				RunID: "capture-1", Job: job,
				Rows: []freedl.PromotionRow{{LibraryPath: filepath.Join(job.LibraryDir, "trusted.m4a"), Selected: false}},
			}, nil
		},
		ApplyPromotionPlan: func(_ context.Context, plan freedl.PromotionPlan) freedl.PromotionResult {
			applied <- plan
			return freedl.PromotionResult{RunID: plan.RunID, Skipped: 1}
		},
	}
	output := &synchronizedBuffer{}
	server := &Server{
		Conn: NewConn(strings.NewReader(""), output), Runs: NewRunRegistry(),
		WorkingDir: dir, ConfigPath: mainPath, FreeDLConfigPath: freePath,
		FreeDLOps: ops, runContext: context.Background(),
	}
	value, rpcErr := server.applyFreeDLPromotion(mustJSON(t, map[string]any{
		"plan": freedl.PromotionPlan{
			RunID: "capture-1", Job: freedl.Job{ID: job.ID},
			Rows: []freedl.PromotionRow{{LibraryPath: "/tmp/client-controlled.m4a", Selected: true}},
		},
	}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if value.(map[string]string)["run_id"] == "" {
		t.Fatal("apply did not return a run ID")
	}
	select {
	case plan := <-applied:
		if len(plan.Rows) != 1 || plan.Rows[0].LibraryPath == "/tmp/client-controlled.m4a" || plan.Rows[0].Selected {
			t.Fatalf("untrusted promotion plan reached apply: %+v", plan)
		}
	case <-time.After(time.Second):
		t.Fatal("promotion apply did not run")
	}
}

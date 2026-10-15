package agent

import (
	"context"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/freedl"
	"github.com/jaa/update-downloads/internal/playlists"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestScopedFreeDLPlanStartsOnlyScopedBuilder(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	main, err := config.LoadSingleFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	freePath, job := writeAgentFreeDLConfig(t, dir, main)
	_, err = playlists.WriteSnapshot(main.Defaults.StateDir, playlists.Snapshot{
		PlaylistID: "review", Name: "Review", Provider: playlists.ProviderAppleMusic,
		RefreshedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var unscoped, scoped atomic.Int32
	ops := &FreeDLOperations{
		BuildCapturePlanProgress: func(context.Context, config.Config, freedl.Job) <-chan freedl.CapturePlanEvent {
			unscoped.Add(1)
			c := make(chan freedl.CapturePlanEvent)
			close(c)
			return c
		},
		BuildCapturePlanForPlaylist: func(context.Context, config.Config, freedl.Job, playlists.Snapshot) <-chan freedl.CapturePlanEvent {
			scoped.Add(1)
			c := make(chan freedl.CapturePlanEvent, 1)
			c <- freedl.CapturePlanEvent{Kind: freedl.CapturePlanEventDone}
			close(c)
			return c
		},
	}
	output := &synchronizedBuffer{}
	s := &Server{Conn: NewConn(strings.NewReader(""), output), Runs: NewRunRegistry(), WorkingDir: dir, ConfigPath: mainPath, FreeDLConfigPath: freePath, FreeDLOps: ops, runContext: context.Background()}
	if _, rpcErr := s.startFreeDLPlan(mustJSON(t, freeDLPlanParams{JobID: job.ID, PlaylistID: "review"})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	waitForNotifications(t, output, 2)
	if unscoped.Load() != 0 || scoped.Load() != 1 {
		t.Fatalf("expected scoped builder only, got unscoped=%d scoped=%d", unscoped.Load(), scoped.Load())
	}
}

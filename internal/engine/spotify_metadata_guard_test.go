package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/output"
)

func TestSpotifyDeemixRefusesExecutionWithoutPrimedMetadata(t *testing.T) {
	for _, failure := range []string{"lookup", "cache"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			target, runtimeDir := filepath.Join(root, "target"), filepath.Join(root, "runtime")
			for _, dir := range []string{target, runtimeDir} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			const trackID = "3n3Ppam7vgaVa1iaRUc9Lp"
			statePath := filepath.Join(root, "spotify.sync")
			originalState := []byte("# udl spotify state v2\n41gXFhitx4whS6PsoXREzy\n")
			if err := os.WriteFile(statePath, originalState, 0o600); err != nil {
				t.Fatal(err)
			}
			state, err := parseSpotifySyncState(statePath)
			if err != nil {
				t.Fatal(err)
			}
			source := config.Source{ID: "guard", Type: config.SourceTypeSpotify,
				URL: spotifyTrackURL(trackID), TargetDir: target, StateFile: statePath,
				DeemixRuntimeDir: runtimeDir, Adapter: config.AdapterSpec{Kind: "deemix"}}
			plan := &spotifyDeemixExecutionPlan{Source: source, PlannedTrackIDs: []string{trackID},
				State: state, StateWritePath: statePath, DownloadOrder: DownloadOrderOldestFirst}
			originalToken, originalPage := fetchSpotifyAccessTokenFn, fetchSpotifyTrackMetadataFn
			t.Cleanup(func() {
				fetchSpotifyAccessTokenFn, fetchSpotifyTrackMetadataFn = originalToken, originalPage
			})
			fetchSpotifyAccessTokenFn = func(context.Context, auth.SpotifyCredentials) (string, error) {
				return "", errors.New("metadata provider offline")
			}
			fetchSpotifyTrackMetadataFn = func(context.Context, string) (spotifyTrackMetadata, error) {
				return spotifyTrackMetadata{}, errors.New("metadata provider offline")
			}
			if failure == "cache" {
				plan.TrackMetadata = map[string]spotifyTrackMetadata{trackID: {
					Title: "Exact track", Artist: "Exact artist", ISRC: "GBBKS9700074"}}
				cachePath := filepath.Join(runtimeDir, "config", "spotify", "cache.json")
				if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cachePath, []byte("malformed cache"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			runner, emitter := &execResultRunner{result: ExecResult{ExitCode: 0}}, &captureEventEmitter{}
			syncer := NewSyncer(map[string]Adapter{"deemix": fakeDeemixAdapter{}}, runner, emitter)
			outcome := syncer.runSpotifyDeemix(context.Background(),
				config.Config{Defaults: config.Defaults{ContinueOnError: true}}, source,
				fakeDeemixAdapter{}, source, nil, plan, SyncOptions{})
			if len(runner.specs) != 0 || outcome.Failed != 1 || outcome.Succeeded != 0 {
				t.Fatalf("unsafe unprimed execution: calls=%d outcome=%+v", len(runner.specs), outcome)
			}
			payload, err := os.ReadFile(statePath)
			if err != nil || string(payload) != string(originalState) {
				t.Fatalf("state changed without verified metadata: %q, %v", payload, err)
			}
			var reported bool
			for _, event := range emitter.events {
				if event.Event == output.EventSourceFailed && strings.Contains(event.Message, "metadata") {
					reported = true
				}
			}
			if !reported {
				t.Fatal("missing visible metadata failure")
			}
		})
	}
}

package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/rekordbox/playlistsync"
)

func TestRekordboxResultsUseProtocolFieldNames(t *testing.T) {
	payload, err := json.Marshal(RekordboxPlaylistSyncPlanResult{
		Resolved: playlistsync.ResolvedOptions{MusicPlaylist: "Music"},
		Plan:     playlistsync.Plan{Version: playlistsync.PlanVersion},
		PlanPath: "/tmp/plan.json",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, field := range []string{`"resolved"`, `"plan"`, `"plan_path"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("missing field %s in %s", field, text)
		}
	}
	if strings.Contains(text, `"PlanPath"`) {
		t.Fatalf("exported Go field leaked into protocol: %s", text)
	}
}

package navidrome

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPartialRemoteLibraryStatusDoesNotClaimTheMirrorIsStale(t *testing.T) {
	for _, partial := range []string{``, `,"partial":false`, `,"partial":true`} {
		t.Run(partial, func(t *testing.T) {
			cfg, resolved := remoteTestConfig(t)
			writeFile(t, resolved.MusicDir+"/a.mp3")
			runner := &scriptedRunner{reply: func(name string, args []string) (RemoteOutput, error) {
				if requireGateCommand(t, name, args) != "udl-mirror status" {
					t.Fatalf("unexpected gate command: %v", args)
				}
				return RemoteOutput{Stdout: []byte(`{"ok":true,"library":{"files":0,"newest_mtime":"2000-01-01T00:00:00Z"` + partial + `},"database":{"present":true,"size_bytes":42,"error":"metadata query timed out"}}`)}, nil
			}}
			manager := &Manager{Config: cfg, Resolved: resolved, Remote: runner.run}
			status, err := manager.RemoteStatus(context.Background())
			if err != nil || status.Library.Partial != strings.Contains(partial, "true") || !status.Database.Present || status.Database.SizeBytes != 42 || status.Database.Error != "metadata query timed out" {
				t.Fatalf("partial status must retain usable metadata and its diagnostic: %+v, %v", status, err)
			}
			problems := manager.staleMirrorProblems(status)
			if len(problems) != 1 {
				t.Fatalf("status comparison problems = %v", problems)
			}
			if status.Library.Partial {
				if problems[0] != "the server's file count is incomplete (status timed out)" {
					t.Fatalf("truncated counts must not claim a mismatch: %v", problems)
				}
				// Also skip the newest-mtime comparison when truncated counts
				// happen to match the local count.
				status.Library.Files = 1
				if got := manager.staleMirrorProblems(status); strings.Join(got, "\n") != problems[0] {
					t.Fatalf("partial timestamps must not claim the mirror is behind: %v", got)
				}
			} else {
				if !strings.Contains(problems[0], "has 0 files") {
					t.Fatalf("complete/older replies must still compare counts: %v", problems)
				}
				wire, err := json.Marshal(status)
				if err != nil || strings.Contains(string(wire), `"partial"`) {
					t.Fatalf("false partial must remain optional on the wire: %s, %v", wire, err)
				}
			}
		})
	}
}

package navidrome

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A restart boots the job out and immediately bootstraps it again, and launchd
// answers that window with "Bootstrap failed: 5: Input/output error". Seen on a
// real restart, which left the service down until it was started by hand.
func TestStartRetriesBootstrapThroughTheLaunchdTeardownWindow(t *testing.T) {
	home := t.TempDir()
	agent := filepath.Join(home, "com.jaa.udl.navidrome.plist")
	if err := os.WriteFile(agent, []byte("<!-- "+OwnershipMarker+" -->"), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	resolved := Resolved{LaunchAgent: agent}

	bootstraps := 0
	svc := Service{
		UID:  "501",
		Wait: func(time.Duration) {},
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "launchctl" && len(args) > 0 && args[0] == "bootstrap" {
				bootstraps++
				if bootstraps < 3 {
					return []byte("Bootstrap failed: 5: Input/output error"), errors.New("exit status 5")
				}
			}
			return []byte(""), nil
		},
	}
	if err := svc.Start(context.Background(), resolved); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if bootstraps != 3 {
		t.Fatalf("bootstrap attempts = %d, want 3 (two transient failures then success)", bootstraps)
	}
}

func TestStartGivesUpOnABootstrapErrorItCannotWaitOut(t *testing.T) {
	home := t.TempDir()
	agent := filepath.Join(home, "com.jaa.udl.navidrome.plist")
	if err := os.WriteFile(agent, []byte("<!-- "+OwnershipMarker+" -->"), 0o644); err != nil {
		t.Fatalf("write agent: %v", err)
	}
	attempts := 0
	svc := Service{
		UID:  "501",
		Wait: func(time.Duration) {},
		Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "launchctl" && len(args) > 0 && args[0] == "bootstrap" {
				attempts++
				return []byte("Load failed: 122: Path had bad ownership"), errors.New("exit status 122")
			}
			return []byte(""), nil
		},
	}
	err := svc.Start(context.Background(), Resolved{LaunchAgent: agent})
	if err == nil || !strings.Contains(err.Error(), "bad ownership") {
		t.Fatalf("error = %v, want the real bootstrap failure surfaced", err)
	}
	if attempts != 1 {
		t.Fatalf("a non-transient failure must not be retried: %d attempts", attempts)
	}
}

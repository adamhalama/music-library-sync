package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseSoundCloudHydratedSound(t *testing.T) {
	document := `<html><head></head><body><script>window.__sc_hydration = [{"hydratable":"sound","data":{"id":2210531636,"title":"PICHI - BO FUNK [FREE DL]","genre":"Trance","artwork_url":"https://i1.sndcdn.com/artworks-abc-large.jpg","purchase_url":"https://hypeddit.com/pichi/pichibofunk","permalink_url":"https://soundcloud.com/pichipichipichipichipichi/bofunkpicho","user":{"username":"PICHI","avatar_url":"https://i1.sndcdn.com/avatars-def-large.jpg"}}}];</script></body></html>`

	sound, err := parseSoundCloudHydratedSound(document)
	if err != nil {
		t.Fatalf("parse soundcloud hydration: %v", err)
	}
	if sound.ID != 2210531636 {
		t.Fatalf("expected sound id 2210531636, got %d", sound.ID)
	}
	if sound.PurchaseURL != "https://hypeddit.com/pichi/pichibofunk" {
		t.Fatalf("unexpected purchase_url: %q", sound.PurchaseURL)
	}
	if sound.ArtworkURL != "https://i1.sndcdn.com/artworks-abc-large.jpg" {
		t.Fatalf("unexpected artwork_url: %q", sound.ArtworkURL)
	}
	if sound.User.Username != "PICHI" {
		t.Fatalf("unexpected username: %q", sound.User.Username)
	}
}

func TestExtractSoundCloudBuyURLFallback(t *testing.T) {
	document := `<a href="https://hypeddit.com/pichi/pichibofunk">Buy PICHI - BO FUNK [FREE DL]</a>`
	got := extractSoundCloudBuyURLFallback(document)
	if got != "https://hypeddit.com/pichi/pichibofunk" {
		t.Fatalf("unexpected fallback url: %q", got)
	}
}

func TestIsHypedditPurchaseURL(t *testing.T) {
	if !isHypedditPurchaseURL("https://hypeddit.com/pichi/pichibofunk") {
		t.Fatalf("expected hypeddit host to be detected")
	}
	if !isHypedditPurchaseURL("https://www.hypeddit.com/track/abc") {
		t.Fatalf("expected hypeddit subdomain to be detected")
	}
	if isHypedditPurchaseURL("https://example.com/track/abc") {
		t.Fatalf("did not expect non-hypeddit host to be detected")
	}
}

func TestSelectBrowserDownloadCandidatePrefersMetadataMatch(t *testing.T) {
	now := time.Now()
	before := map[string]mediaFileSnapshot{}
	after := map[string]mediaFileSnapshot{
		"Stara - Uh Oh.m4a": {
			Size:    10,
			ModTime: now.Add(-1 * time.Second),
		},
		"MASTER BOFUNK.wav": {
			Size:    20,
			ModTime: now,
		},
	}
	metadata := soundCloudFreeDownloadMetadata{
		Title:  "BO FUNK",
		Artist: "PICHI",
	}
	got := selectBrowserDownloadCandidate(before, after, metadata)
	if got != "MASTER BOFUNK.wav" {
		t.Fatalf("expected metadata-matching candidate, got %q", got)
	}
}

func TestNextAvailablePathAddsSuffixWhenExists(t *testing.T) {
	tmp := t.TempDir()
	original := filepath.Join(tmp, "track.wav")
	if err := os.WriteFile(original, []byte("x"), 0o644); err != nil {
		t.Fatalf("write original: %v", err)
	}
	got := nextAvailablePath(original)
	want := filepath.Join(tmp, "track (1).wav")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestBrowserOpenCommandDarwinDefault(t *testing.T) {
	origGOOS := runtimeGOOS
	runtimeGOOS = "darwin"
	t.Cleanup(func() {
		runtimeGOOS = origGOOS
	})

	origApp := os.Getenv("UDL_FREEDL_BROWSER_APP")
	if err := os.Setenv("UDL_FREEDL_BROWSER_APP", ""); err != nil {
		t.Fatalf("unset browser app env: %v", err)
	}
	t.Cleanup(func() {
		if setErr := os.Setenv("UDL_FREEDL_BROWSER_APP", origApp); setErr != nil {
			t.Fatalf("restore browser app env: %v", setErr)
		}
	})

	bin, args, err := browserOpenCommand("https://hypeddit.com/pichi/pichibofunk")
	if err != nil {
		t.Fatalf("browserOpenCommand: %v", err)
	}
	if bin != "open" {
		t.Fatalf("expected open binary, got %q", bin)
	}
	wantArgs := []string{"https://hypeddit.com/pichi/pichibofunk"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("expected args %v, got %v", wantArgs, args)
	}
}

func TestBrowserOpenCommandDarwinWithAppOverride(t *testing.T) {
	origGOOS := runtimeGOOS
	runtimeGOOS = "darwin"
	t.Cleanup(func() {
		runtimeGOOS = origGOOS
	})

	origApp := os.Getenv("UDL_FREEDL_BROWSER_APP")
	if err := os.Setenv("UDL_FREEDL_BROWSER_APP", "Helium"); err != nil {
		t.Fatalf("set browser app env: %v", err)
	}
	t.Cleanup(func() {
		if setErr := os.Setenv("UDL_FREEDL_BROWSER_APP", origApp); setErr != nil {
			t.Fatalf("restore browser app env: %v", setErr)
		}
	})

	bin, args, err := browserOpenCommand("https://hypeddit.com/pichi/pichibofunk")
	if err != nil {
		t.Fatalf("browserOpenCommand: %v", err)
	}
	if bin != "open" {
		t.Fatalf("expected open binary, got %q", bin)
	}
	wantArgs := []string{"-a", "Helium", "https://hypeddit.com/pichi/pichibofunk"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("expected args %v, got %v", wantArgs, args)
	}
}

func TestOpenURLInBrowserPropagatesLaunchFailure(t *testing.T) {
	origRunBrowserCommand := runBrowserCommandFn
	runBrowserCommandFn = func(ctx context.Context, bin string, args ...string) error {
		return fmt.Errorf("open failed")
	}
	t.Cleanup(func() {
		runBrowserCommandFn = origRunBrowserCommand
	})

	err := openURLInBrowser(context.Background(), "https://hypeddit.com/pichi/pichibofunk")
	if err == nil {
		t.Fatalf("expected launch failure")
	}
	if !strings.Contains(err.Error(), "browser launch command failed") {
		t.Fatalf("expected wrapped launch error, got %v", err)
	}
}

func TestSnapshotMediaFilesIncludesAIF(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "Rare Mamba - 150cc [RV014].aif")
	if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
		t.Fatalf("write media: %v", err)
	}

	snapshot, err := snapshotMediaFiles(tmp)
	if err != nil {
		t.Fatalf("snapshot media files: %v", err)
	}
	if _, ok := snapshot[filepath.Base(path)]; !ok {
		t.Fatalf("expected .aif file to be detected in media snapshot, got %+v", snapshot)
	}
}

func TestHasBrowserInProgressActivity(t *testing.T) {
	now := time.Now()
	before := map[string]mediaFileSnapshot{
		"track.crdownload": {
			Size:    10,
			ModTime: now,
		},
	}
	after := map[string]mediaFileSnapshot{
		"track.crdownload": {
			Size:    20,
			ModTime: now.Add(1 * time.Second),
		},
	}
	if !hasBrowserInProgressActivity(before, after) {
		t.Fatalf("expected in-progress activity when size/modtime changes")
	}
}

func TestResolveBrowserDownloadIdleTimeoutOverride(t *testing.T) {
	orig := os.Getenv("UDL_FREEDL_BROWSER_IDLE_TIMEOUT")
	if err := os.Setenv("UDL_FREEDL_BROWSER_IDLE_TIMEOUT", "30s"); err != nil {
		t.Fatalf("set idle timeout override: %v", err)
	}
	t.Cleanup(func() {
		if setErr := os.Setenv("UDL_FREEDL_BROWSER_IDLE_TIMEOUT", orig); setErr != nil {
			t.Fatalf("restore idle timeout override: %v", setErr)
		}
	})

	got := resolveBrowserDownloadIdleTimeout(5 * time.Minute)
	if got != 30*time.Second {
		t.Fatalf("expected 30s idle timeout override, got %s", got)
	}
}

func TestResolveSoundCloudArtworkURLUpgradesLargeVariant(t *testing.T) {
	got := resolveSoundCloudArtworkURL("https://i1.sndcdn.com/artworks-abc-large.jpg")
	want := "https://i1.sndcdn.com/artworks-abc-t500x500.jpg"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestInferArtworkFileExtensionFromContentType(t *testing.T) {
	got := inferArtworkFileExtension("image/png", "")
	if got != ".png" {
		t.Fatalf("expected .png, got %q", got)
	}
}

func TestResolveSoundCloudFreeDLStuckLogPath(t *testing.T) {
	got, err := resolveSoundCloudFreeDLStuckLogPath("/tmp/udl-state", "sc-free")
	if err != nil {
		t.Fatalf("resolve stuck log path: %v", err)
	}
	want := filepath.Clean("/tmp/udl-state/sc-free.freedl-stuck.jsonl")
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestAppendSoundCloudFreeDLStuckRecordWritesJSONL(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "state", "sc-free.freedl-stuck.jsonl")
	record := soundCloudFreeDLStuckRecord{
		Timestamp:   "2026-02-27T00:00:00Z",
		SourceID:    "sc-free",
		TrackID:     "111",
		Title:       "Launch Fail",
		PurchaseURL: "https://hypeddit.com/pichi/111",
		Stage:       "browser-launch",
		Error:       "launch failed",
		Strategy:    "browser-handoff",
	}

	if err := appendSoundCloudFreeDLStuckRecord(path, record); err != nil {
		t.Fatalf("append stuck record: %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stuck log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(payload)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one stuck record line, got %d", len(lines))
	}
	decoded := soundCloudFreeDLStuckRecord{}
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("decode stuck record: %v", err)
	}
	if decoded.TrackID != "111" || decoded.Stage != "browser-launch" {
		t.Fatalf("unexpected decoded record: %+v", decoded)
	}
}

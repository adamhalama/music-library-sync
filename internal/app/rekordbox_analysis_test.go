package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/rekordbox/bridge"
)

type analysisBridgeFake struct {
	inspect  bridge.AnalysisInspectResponse
	applied  bridge.AnalysisApplyRequest
	response bridge.AnalysisApplyResponse
}

func (f *analysisBridgeFake) AnalysisInspect(context.Context, bridge.AnalysisInspectRequest) (bridge.AnalysisInspectResponse, error) {
	return f.inspect, nil
}
func (f *analysisBridgeFake) AnalysisApply(_ context.Context, r bridge.AnalysisApplyRequest) (bridge.AnalysisApplyResponse, error) {
	f.applied = r
	return f.response, nil
}
func analysisReq(t *testing.T, b *analysisBridgeFake) (RekordboxAnalysisUseCase, RekordboxAnalysisRequest) {
	t.Helper()
	dir := t.TempDir()
	use := RekordboxAnalysisUseCase{Bridge: b,
		CheckClosed: func(context.Context, string) error { return nil },
		CreateBackup: func(context.Context, string, string, time.Time) (string, error) {
			return filepath.Join(dir, "backup"), nil
		},
		RunAnalyser: func(context.Context, string, string, string, string, string, string) (bridge.AnalysisResult, error) {
			return bridge.AnalysisResult{BPM: 128, Key: "Ebm", Mode: "rbxport", AnalyserVersion: "rev", DurationSeconds: 180, Files: []string{"ANLZ0000.DAT", "ANLZ0000.EXT", "ANLZ0000.2EX"}}, nil
		},
	}
	req := RekordboxAnalysisRequest{Config: config.Config{}, DBDir: dir, BackupDir: dir, ContentIDs: []string{"1"}, Mode: "rbxport", KeyNotation: "classic", Analyser: "/usr/bin/true"}
	return use, req
}
func TestAnalysisPlanSkipsValidAnalysisUnlessReanalyse(t *testing.T) {
	b := &analysisBridgeFake{inspect: bridge.AnalysisInspectResponse{KeyNotation: "classic", Contents: []bridge.AnalysisContent{{ID: "1", Path: "/tmp/a.mp3", AnalysisDataPath: "/PIONEER/USBANLZ/a/ANLZ0000.DAT", ANLZExists: true, Analysed: 105}, {ID: "2", Path: "/tmp/b.mp3", AnalysisDataPath: "", ANLZExists: false}}}}
	u, r := analysisReq(t, b)
	p, e := u.Plan(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if p.Tracks[0].Status != "skipped" || p.Tracks[0].Reason != "already analysed" {
		t.Fatalf("%+v", p.Tracks)
	}
	r.Reanalyse = true
	p, e = u.Plan(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	if p.Tracks[0].Status != "ready" {
		t.Fatalf("expected ready: %+v", p.Tracks[0])
	}
	if _, err := u.Apply(context.Background(), r, p); err != nil {
		t.Fatal(err)
	}
	if !b.applied.Reanalyse {
		t.Fatal("bridge request omitted reanalysis")
	}
}
func TestAnalysisModeAndSelectorValidation(t *testing.T) {
	u, r := analysisReq(t, &analysisBridgeFake{})
	r.Mode = "bad"
	if _, e := u.Plan(context.Background(), r); e == nil || !strings.Contains(e.Error(), "mode") {
		t.Fatal(e)
	}
	r.Mode = "rbxport"
	r.ContentIDs = nil
	if _, e := u.Plan(context.Background(), r); e == nil || !strings.Contains(e.Error(), "selector") {
		t.Fatal(e)
	}
}
func TestAnalysisFailureIsolation(t *testing.T) {
	b := &analysisBridgeFake{inspect: bridge.AnalysisInspectResponse{Contents: []bridge.AnalysisContent{{ID: "1", Path: "/tmp/a.mp3"}, {ID: "2", Path: "/tmp/b.mp3"}}}, response: bridge.AnalysisApplyResponse{Rows: []bridge.AnalysisAppliedRow{{ID: "1"}}}}
	u, r := analysisReq(t, b)
	r.ContentIDs = []string{"1", "2"}
	calls := 0
	u.RunAnalyser = func(context.Context, string, string, string, string, string, string) (bridge.AnalysisResult, error) {
		calls++
		if calls == 2 {
			return bridge.AnalysisResult{}, errors.New("decode")
		}
		return bridge.AnalysisResult{BPM: 128, Mode: "rbxport", AnalyserVersion: "r", DurationSeconds: 1, Files: []string{"ANLZ0000.DAT", "ANLZ0000.EXT", "ANLZ0000.2EX"}}, nil
	}
	p, e := u.Plan(context.Background(), r)
	if e != nil {
		t.Fatal(e)
	}
	p, e = u.Apply(context.Background(), r, p)
	if e != nil {
		t.Fatal(e)
	}
	if p.Tracks[0].Status != "analysed" || p.Tracks[1].Status != "failed" {
		t.Fatalf("%+v", p.Tracks)
	}
	if len(b.applied.Rows) != 1 {
		t.Fatalf("expected one isolated row, got %d", len(b.applied.Rows))
	}
}

func TestAnalysisGuardAndDryRunPreventWrites(t *testing.T) {
	b := &analysisBridgeFake{inspect: bridge.AnalysisInspectResponse{Contents: []bridge.AnalysisContent{{ID: "1", Path: "/tmp/a.mp3"}}}}
	u, r := analysisReq(t, b)
	backups := 0
	u.CreateBackup = func(context.Context, string, string, time.Time) (string, error) { backups++; return "backup", nil }
	u.CheckClosed = func(context.Context, string) error { return errors.New("RBxport is running") }
	p, err := u.Plan(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = u.Apply(context.Background(), r, p); err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatal(err)
	}
	r.DryRun = true
	if _, err = u.Apply(context.Background(), r, p); err != nil {
		t.Fatal(err)
	}
	if backups != 0 || len(b.applied.Rows) != 0 {
		t.Fatal("guard/dry run permitted writes")
	}
}

func TestAnalysisApplyRejectsSymlinksBeforeBackup(t *testing.T) {
	for _, name := range []string{"master.db", "share"} {
		t.Run(name, func(t *testing.T) {
			b := &analysisBridgeFake{inspect: bridge.AnalysisInspectResponse{Contents: []bridge.AnalysisContent{{ID: "1", Path: "/tmp/a.mp3"}}}}
			u, r := analysisReq(t, b)
			u.CreateBackup = func(context.Context, string, string, time.Time) (string, error) {
				t.Fatal("backup started with a symlink in the library")
				return "", nil
			}
			path := filepath.Join(r.DBDir, name)
			if err := os.Symlink(filepath.Join(t.TempDir(), name), path); err != nil {
				t.Fatal(err)
			}
			p, err := u.Plan(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := u.Apply(context.Background(), r, p); err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "is a symlink") {
				t.Fatalf("expected a library symlink error, got %v", err)
			}
		})
	}
}

func TestResolveAnalyserRequiresExecutableSibling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix executable permissions")
	}
	t.Setenv("UDL_RBL_ANALYSE", "")
	t.Setenv("PATH", t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(filepath.Dir(exe), "udl-rbl-analyse")
	if err := os.Mkdir(candidate, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(candidate) })
	if _, err := resolveAnalyser(""); err == nil {
		t.Fatal("resolved a directory as the analyser")
	}
	if err := os.Remove(candidate); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAnalyser(""); err == nil {
		t.Fatal("resolved a non-executable file as the analyser")
	}
	if err := os.Chmod(candidate, 0700); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveAnalyser(""); err != nil || got != candidate {
		t.Fatalf("expected sibling %q, got %q (%v)", candidate, got, err)
	}
}

func TestAnalysisFakeExecutableIsolatesDecodeFailure(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "udl-rbl-analyse")
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
 case "$1" in --audio) audio="$2";; --out) out="$2";; esac
 shift 2
done
case "$audio" in *bad*) echo 'decode failed' >&2; exit 1;; esac
for ext in DAT EXT 2EX; do printf PMAItest > "$out/ANLZ0000.$ext"; done
printf '%s' '{"bpm":128,"key":"Ebm","mode":"rbxport","analyser_version":"rev","duration_seconds":180,"files":["ANLZ0000.DAT","ANLZ0000.EXT","ANLZ0000.2EX"]}'
`
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	b := &analysisBridgeFake{inspect: bridge.AnalysisInspectResponse{Contents: []bridge.AnalysisContent{{ID: "1", Path: "/tmp/bad.mp3"}, {ID: "2", Path: "/tmp/good.mp3"}}}, response: bridge.AnalysisApplyResponse{Rows: []bridge.AnalysisAppliedRow{{ID: "2"}}}}
	u, r := analysisReq(t, b)
	u.RunAnalyser = nil
	r.Analyser = bin
	p, err := u.Plan(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	p, err = u.Apply(context.Background(), r, p)
	if err != nil {
		t.Fatal(err)
	}
	if p.Tracks[0].Status != "failed" || !strings.Contains(p.Tracks[0].Reason, "decode failed") || p.Tracks[1].Status != "analysed" {
		t.Fatalf("%+v", p.Tracks)
	}
	if len(b.applied.Rows) != 1 || b.applied.Rows[0].Content.ID != "2" {
		t.Fatalf("%+v", b.applied.Rows)
	}
}

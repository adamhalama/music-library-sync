package bridge

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stubPython writes an executable stand-in for the python interpreter. It records
// the request it received on stdin plus the PYTHONPATH it ran with, then replies
// with the supplied stdout and exit code.
func stubPython(t *testing.T, stdout, stderr string, exitCode int) (bin, requestPath, envPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub interpreter script requires a POSIX shell")
	}
	dir := t.TempDir()
	bin = filepath.Join(dir, "fake-python")
	requestPath = filepath.Join(dir, "request.json")
	envPath = filepath.Join(dir, "pythonpath.txt")

	script := strings.Join([]string{
		"#!/bin/sh",
		"cat > " + requestPath,
		"printf '%s' \"$PYTHONPATH\" > " + envPath,
		"printf '%s' " + shellQuote(stdout),
		"printf '%s' " + shellQuote(stderr) + " >&2",
		"exit " + strconv.Itoa(exitCode),
	}, "\n") + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatalf("write stub interpreter: %v", err)
	}
	return bin, requestPath, envPath
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func TestClientInspectSendsRequestAndDecodesResponse(t *testing.T) {
	response := `{"playlists":[{"id":"p1","name":"fav_imports","attribute":0,"parent_id":"root","content_ids":["c1"]}],` +
		`"contents":[{"id":"c1","title":"One","folder_path":"/Music/One.mp3"}]}`
	bin, requestPath, _ := stubPython(t, response, "", 0)

	inspect, err := (Client{PythonBin: bin}).Inspect(context.Background(), "/db/dir")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(inspect.Playlists) != 1 || inspect.Playlists[0].Name != "fav_imports" {
		t.Fatalf("unexpected playlists: %+v", inspect.Playlists)
	}
	if len(inspect.Contents) != 1 || inspect.Contents[0].FolderPath != "/Music/One.mp3" {
		t.Fatalf("unexpected contents: %+v", inspect.Contents)
	}

	payload, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("read recorded request: %v", err)
	}
	var request map[string]any
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode recorded request %s: %v", payload, err)
	}
	if request["op"] != "inspect" || request["db_dir"] != "/db/dir" {
		t.Fatalf("unexpected helper request: %v", request)
	}
}

func TestClientApplySendsPlanFieldsToHelper(t *testing.T) {
	bin, requestPath, _ := stubPython(t, `{"playlist_id":"p1","playlist_name":"fav_imports","final_content_ids":["c1","c2"],"final_track_count":2}`, "", 0)

	resp, err := (Client{PythonBin: bin}).Apply(context.Background(), ApplyRequest{
		DBDir:                     "/db/dir",
		TargetPlaylistID:          "p1",
		TargetPlaylistName:        "fav_imports",
		CreatePlaylistIfMissing:   false,
		ExpectedCurrentContentIDs: []string{"c0"},
		FinalContentIDs:           []string{"c1", "c2"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if resp.FinalTrackCount != 2 || len(resp.FinalContentIDs) != 2 {
		t.Fatalf("unexpected apply response: %+v", resp)
	}

	payload, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("read recorded request: %v", err)
	}
	var request struct {
		Op      string       `json:"op"`
		Request ApplyRequest `json:"request"`
	}
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("decode recorded request %s: %v", payload, err)
	}
	if request.Op != "apply" {
		t.Fatalf("expected apply op, got %q", request.Op)
	}
	if request.Request.TargetPlaylistID != "p1" || request.Request.CreatePlaylistIfMissing {
		t.Fatalf("unexpected forwarded request: %+v", request.Request)
	}
	if got := request.Request.FinalContentIDs; len(got) != 2 || got[0] != "c1" || got[1] != "c2" {
		t.Fatalf("expected planned order to be forwarded verbatim, got %v", got)
	}
}

func TestClientRunPrependsPythonPathToExistingValue(t *testing.T) {
	t.Setenv("PYTHONPATH", "/pre/existing")
	bin, _, envPath := stubPython(t, `{"playlists":[],"contents":[]}`, "", 0)

	if _, err := (Client{PythonBin: bin, PythonPath: "/extra/site-packages"}).Inspect(context.Background(), "/db/dir"); err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	recorded, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read recorded PYTHONPATH: %v", err)
	}
	want := "/extra/site-packages" + string(os.PathListSeparator) + "/pre/existing"
	if string(recorded) != want {
		t.Fatalf("expected PYTHONPATH %q, got %q", want, recorded)
	}
}

func TestClientRunUsesEnvOverrideWhenPythonPathUnset(t *testing.T) {
	t.Setenv("PYTHONPATH", "")
	t.Setenv("UDL_REKORDBOX_PYTHONPATH", "/from/env")
	bin, _, envPath := stubPython(t, `{"playlists":[],"contents":[]}`, "", 0)

	if _, err := (Client{PythonBin: bin}).Inspect(context.Background(), "/db/dir"); err != nil {
		t.Fatalf("Inspect: %v", err)
	}

	recorded, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read recorded PYTHONPATH: %v", err)
	}
	if string(recorded) != "/from/env" {
		t.Fatalf("expected PYTHONPATH from env override, got %q", recorded)
	}
}

func TestClientRunReportsHelperStderrOnFailure(t *testing.T) {
	bin, _, _ := stubPython(t, "", "ERROR: Rekordbox playlist ID '42' not found", 1)

	_, err := (Client{PythonBin: bin}).Apply(context.Background(), ApplyRequest{DBDir: "/db/dir"})
	if err == nil {
		t.Fatalf("expected helper failure to surface as an error")
	}
	if !strings.Contains(err.Error(), "Rekordbox playlist ID '42' not found") {
		t.Fatalf("expected helper detail in error, got %v", err)
	}
}

func TestClientRunReportsUndecodableHelperOutput(t *testing.T) {
	bin, _, _ := stubPython(t, "not json", "", 0)

	_, err := (Client{PythonBin: bin}).Inspect(context.Background(), "/db/dir")
	if err == nil {
		t.Fatalf("expected decode failure")
	}
	if !strings.Contains(err.Error(), "not json") {
		t.Fatalf("expected raw output in decode error, got %v", err)
	}
}

func TestAnalysisBridgeContract(t *testing.T) {
	bin, requestPath, _ := stubPython(t, `{"contents":[{"id":"1","path":"/tmp/a.mp3","uuid":"abc-def","analysis_data_path":"","anlz_exists":false,"analysed":0}],"key_notation":"classic"}`, "", 0)
	got, err := (Client{PythonBin: bin}).AnalysisInspect(context.Background(), AnalysisInspectRequest{DBDir: "/copy", ContentIDs: []string{"1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Contents) != 1 || got.KeyNotation != "classic" {
		t.Fatalf("%+v", got)
	}
	data, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Op      string                 `json:"op"`
		Request AnalysisInspectRequest `json:"request"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Op != "analysis_inspect" || payload.Request.DBDir != "/copy" {
		t.Fatalf("%+v", payload)
	}
}

func TestAnalysisSqliteFixtures(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 unavailable")
	}
	cmd := exec.Command(python, "-m", "unittest", "discover", "-s", ".", "-p", "analysis_helper_test.py", "-v")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite fixtures: %v\n%s", err, out)
	}
}

func TestAnalysisApplyInterruptsHelperOnCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interrupt test requires POSIX signals")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-python")
	ready := filepath.Join(dir, "ready")
	script := "#!/bin/sh\n" +
		"trap 'echo interrupted >&2; exit 1' INT\n" +
		"cat >/dev/null\n" +
		"touch " + shellQuote(ready) + "\n" +
		"while :; do sleep 0.05; done\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (Client{PythonBin: bin}).AnalysisApply(ctx, AnalysisApplyRequest{DBDir: dir})
		done <- err
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("helper exited before it was ready: %v", err)
		case <-ctx.Done():
			t.Fatal("helper did not signal readiness")
		case <-ticker.C:
		}
	}
	cancel()
	if err := <-done; err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("helper did not handle the interrupt: %v", err)
	}
}

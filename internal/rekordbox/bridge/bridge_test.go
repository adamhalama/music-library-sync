package bridge

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
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

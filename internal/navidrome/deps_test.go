package navidrome

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recordedCommand struct {
	name string
	args []string
}

type fakeCommands struct {
	calls    []recordedCommand
	response func(name string, args ...string) ([]byte, error)
}

func (f *fakeCommands) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, recordedCommand{name: name, args: args})
	if f.response == nil {
		return nil, nil
	}
	return f.response(name, args...)
}

func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"navidrome version 0.63.2 (a1b2c3d)": "0.63.2",
		"0.64.0":                             "0.64.0",
		"Navidrome 1.2.10\n":                 "1.2.10",
	}
	for raw, want := range cases {
		got, ok := ParseVersion(raw)
		if !ok || got != want {
			t.Fatalf("ParseVersion(%q) = %q,%v want %q", raw, got, ok, want)
		}
	}
	if _, ok := ParseVersion("unknown build"); ok {
		t.Fatalf("expected parse failure for version-less output")
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("0.63.1", MinimumServerVersion) >= 0 {
		t.Fatalf("0.63.1 must sort before the minimum")
	}
	if CompareVersions("0.63.2", MinimumServerVersion) != 0 {
		t.Fatalf("equal versions must compare equal")
	}
	if CompareVersions("0.64.0", MinimumServerVersion) <= 0 {
		t.Fatalf("0.64.0 must sort after the minimum")
	}
	if CompareVersions("1.0.0", "0.99.99") <= 0 {
		t.Fatalf("major version must dominate")
	}
}

func TestStatusReportsMissingNavidrome(t *testing.T) {
	commands := &fakeCommands{}
	checker := DependencyChecker{
		Run:         commands.run,
		LookPath:    func(string) (string, error) { return "", errors.New("not found") },
		SearchPaths: []string{filepath.Join(t.TempDir(), "navidrome")},
	}
	status := checker.Status(context.Background())
	if status.Installed || status.Ready() {
		t.Fatalf("status must report Navidrome as missing: %+v", status)
	}
	if len(commands.calls) != 0 {
		t.Fatalf("status must not run commands when the binary is missing: %+v", commands.calls)
	}
	if !containsFragment(status.Problems, "not installed") {
		t.Fatalf("problems = %v", status.Problems)
	}
}

func TestStatusRejectsOldVersion(t *testing.T) {
	binary := writeFakeBinary(t, "navidrome")
	commands := &fakeCommands{response: func(string, ...string) ([]byte, error) {
		return []byte("navidrome version 0.62.0 (abc)"), nil
	}}
	checker := DependencyChecker{
		Run:      commands.run,
		LookPath: func(name string) (string, error) { return binary, nil },
	}
	status := checker.Status(context.Background())
	if !status.Installed {
		t.Fatalf("binary should be detected")
	}
	if status.VersionSupported || status.Ready() {
		t.Fatalf("0.62.0 must be rejected")
	}
	if !containsFragment(status.Problems, MinimumServerVersion) {
		t.Fatalf("problems = %v", status.Problems)
	}
}

func TestStatusAcceptsSupportedVersion(t *testing.T) {
	binary := writeFakeBinary(t, "navidrome")
	commands := &fakeCommands{response: func(string, ...string) ([]byte, error) {
		return []byte("navidrome version 0.63.2 (abc)"), nil
	}}
	checker := DependencyChecker{Run: commands.run, LookPath: func(string) (string, error) { return binary, nil }}
	status := checker.Status(context.Background())
	if !status.Ready() {
		t.Fatalf("0.63.2 must be accepted: %+v", status)
	}
	if status.Version != "0.63.2" {
		t.Fatalf("version = %q", status.Version)
	}
}

func TestEnsureRequiresConfirmation(t *testing.T) {
	commands := &fakeCommands{}
	checker := DependencyChecker{
		Run:         commands.run,
		LookPath:    func(string) (string, error) { return "", errors.New("missing") },
		SearchPaths: []string{filepath.Join(t.TempDir(), "navidrome")},
	}
	result, err := checker.Ensure(context.Background(), false)
	if err == nil {
		t.Fatalf("unconfirmed install must fail")
	}
	if result.Ran {
		t.Fatalf("unconfirmed install must not run anything")
	}
	for _, call := range commands.calls {
		if strings.Contains(call.name, "brew") {
			t.Fatalf("unconfirmed install ran brew: %+v", call)
		}
	}
}

func TestLogTailMissingFileIsNotAnError(t *testing.T) {
	lines, err := LogTail(filepath.Join(t.TempDir(), "navidrome.log"), 10)
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("lines = %v", lines)
	}
}

func TestLogTailReturnsLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "navidrome.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	lines, err := LogTail(path, 2)
	if err != nil {
		t.Fatalf("LogTail: %v", err)
	}
	if len(lines) != 2 || lines[0] != "two" || lines[1] != "three" {
		t.Fatalf("lines = %v", lines)
	}
}

func writeFakeBinary(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	return path
}

func containsFragment(values []string, fragment string) bool {
	for _, value := range values {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

// The real library is 911 m4a and 12 wav against 651 mp3. With ffmpeg missing
// every one of those 923 tracks failed to stream, and nothing in status said
// so — the only evidence was "invalid argument" in the server log.
func TestStatusReportsMissingFFmpeg(t *testing.T) {
	binary := writeFakeBinary(t, "navidrome")
	commands := &fakeCommands{response: func(string, ...string) ([]byte, error) {
		return []byte("navidrome version 0.63.2 (abc)"), nil
	}}
	checker := DependencyChecker{
		Run: commands.run,
		LookPath: func(name string) (string, error) {
			if name == "ffmpeg" {
				return "", errors.New("not found")
			}
			return binary, nil
		},
		FFmpegSearchPaths: []string{filepath.Join(t.TempDir(), "ffmpeg")},
	}
	status := checker.Status(context.Background())
	if status.FFmpegInstalled || status.FFmpegPath != "" {
		t.Fatalf("ffmpeg must be reported as missing: %+v", status)
	}
	if !containsFragment(status.Problems, "ffmpeg is not installed") {
		t.Fatalf("problems = %v", status.Problems)
	}
}

func TestStatusResolvesFFmpegOutsidePATH(t *testing.T) {
	ffmpeg := writeFakeBinary(t, "ffmpeg")
	binary := writeFakeBinary(t, "navidrome")
	commands := &fakeCommands{response: func(string, ...string) ([]byte, error) {
		return []byte("navidrome version 0.63.2 (abc)"), nil
	}}
	checker := DependencyChecker{
		Run: commands.run,
		LookPath: func(name string) (string, error) {
			if name == "ffmpeg" {
				return "", errors.New("not found")
			}
			return binary, nil
		},
		FFmpegSearchPaths: []string{ffmpeg},
	}
	status := checker.Status(context.Background())
	if !status.FFmpegInstalled || status.FFmpegPath != ffmpeg {
		t.Fatalf("ffmpeg must resolve from the search paths: %+v", status)
	}
	if containsFragment(status.Problems, "ffmpeg") {
		t.Fatalf("a resolved ffmpeg must not be a problem: %v", status.Problems)
	}
}

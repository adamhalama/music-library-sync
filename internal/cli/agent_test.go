package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentCommandEOFKeepsStdoutPureAndRestoresWorkingDirectory(t *testing.T) {
	before, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd := newAgentCommand(&AppContext{Build: BuildInfo{Version: "test"}})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"--working-dir", t.TempDir()})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("protocol stdout was contaminated: %q", stdout.String())
	}
	after, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("working directory was not restored: got %q want %q", after, before)
	}
}

func TestResolveAgentWorkingDirRejectsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAgentWorkingDir(path); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("expected directory validation error, got %v", err)
	}
}

func TestRootHelpIncludesAgent(t *testing.T) {
	var out bytes.Buffer
	app := &AppContext{IO: IOStreams{In: strings.NewReader(""), Out: &out, ErrOut: &out}}
	cmd := newRootCommand(app)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "agent") {
		t.Fatalf("root help does not include agent:\n%s", out.String())
	}
}

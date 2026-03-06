package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestTUICommandHelp(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.SetArgs([]string{"tui", "--help"})

	if err := root.Execute(); err != nil {
		t.Fatalf("tui --help failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Launch the full-screen TUI shell") {
		t.Fatalf("expected tui help output, got: %s", stdout.String())
	}
}

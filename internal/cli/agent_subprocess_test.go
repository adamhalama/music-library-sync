package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestAgentSubprocessStdoutIsProtocolOnlyNDJSON(t *testing.T) {
	if os.Getenv("UDL_AGENT_TEST_HELPER") == "1" {
		cmd := newAgentCommand(&AppContext{Build: BuildInfo{Version: "test"}})
		cmd.SetIn(os.Stdin)
		cmd.SetOut(os.Stdout)
		cmd.SetErr(os.Stderr)
		cmd.SetArgs([]string{"--working-dir", os.Getenv("UDL_AGENT_TEST_DIR")})
		if err := cmd.Execute(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	command := exec.Command(os.Args[0], "-test.run=TestAgentSubprocessStdoutIsProtocolOnlyNDJSON")
	command.Env = append(os.Environ(),
		"UDL_AGENT_TEST_HELPER=1",
		"UDL_AGENT_TEST_DIR="+t.TempDir(),
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	send := func(frame string) map[string]any {
		t.Helper()
		if _, err := fmt.Fprintln(stdin, frame); err != nil {
			t.Fatal(err)
		}
		if !scanner.Scan() {
			t.Fatalf("missing protocol response; stderr=%s", stderr.String())
		}
		var decoded map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &decoded); err != nil {
			t.Fatalf("non-JSON stdout frame: %q: %v", scanner.Text(), err)
		}
		return decoded
	}
	init := send(`{"jsonrpc":"2.0","id":1,"method":"session.initialize","params":{"protocol_version":2}}`)
	if init["error"] != nil || init["result"] == nil {
		t.Fatalf("unexpected initialize response: %+v", init)
	}
	shutdown := send(`{"jsonrpc":"2.0","id":2,"method":"session.shutdown"}`)
	if shutdown["error"] != nil || shutdown["result"] == nil {
		t.Fatalf("unexpected shutdown response: %+v", shutdown)
	}
	_ = stdin.Close()
	wait := make(chan error, 1)
	go func() { wait <- command.Wait() }()
	select {
	case err := <-wait:
		if err != nil {
			t.Fatalf("agent subprocess failed: %v; stderr=%s", err, stderr.String())
		}
	case <-time.After(3 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("agent subprocess did not exit after session.shutdown")
	}
	if scanner.Scan() {
		t.Fatalf("unexpected extra stdout frame: %q", scanner.Text())
	}
}

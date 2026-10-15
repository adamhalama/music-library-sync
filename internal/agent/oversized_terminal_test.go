package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/exitcode"
)

func TestOversizedTerminalStillFinishesRun(t *testing.T) {
	for _, largeError := range []bool{false, true} {
		t.Run(map[bool]string{false: "result", true: "error"}[largeError], func(t *testing.T) {
			output := &synchronizedBuffer{}
			server := &Server{Conn: NewConn(strings.NewReader(""), output), Runs: NewRunRegistry()}
			runID, err := server.StartRun(func(context.Context, string) (any, error, int) {
				large := strings.Repeat("x", MaxFrameBytes)
				if largeError {
					return nil, errors.New(large), exitcode.RuntimeFailure
				}
				return large, nil, exitcode.Success
			})
			if err != nil {
				t.Fatal(err)
			}
			waitForNotifications(t, output, 1)
			var frame envelope
			if err := json.Unmarshal([]byte(strings.TrimSpace(output.String())), &frame); err != nil {
				t.Fatal(err)
			}
			var terminal runFinishedParams
			if err := json.Unmarshal(frame.Params, &terminal); err != nil {
				t.Fatal(err)
			}
			if frame.Method != "run.finished" || terminal.RunID != runID || terminal.ExitCode != exitcode.RuntimeFailure || terminal.Error == "" || terminal.Result != nil {
				t.Fatalf("unexpected reduced terminal: %+v", terminal)
			}
			if server.Runs.Len() != 0 {
				t.Fatal("run still registered")
			}
		})
	}
}

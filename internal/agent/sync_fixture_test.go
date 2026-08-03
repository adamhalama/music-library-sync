package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestScriptedFullSyncProtocolFixture(t *testing.T) {
	file, err := os.Open("testdata/sync_full_run_v2.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	type scriptLine struct {
		Direction string          `json:"direction"`
		Frame     json.RawMessage `json:"frame"`
	}
	methods := []string{}
	lines := 0
	selectRequests := 0
	sawRebuildReply := false
	sawFinalSelection := false
	sawConfirmation := false
	sawExecution := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines++
		var line scriptLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("line %d: %v", lines, err)
		}
		if line.Direction != "client_to_server" && line.Direction != "server_to_client" {
			t.Fatalf("line %d has invalid direction %q", lines, line.Direction)
		}
		var frame envelope
		if err := json.Unmarshal(line.Frame, &frame); err != nil {
			t.Fatalf("line %d frame: %v", lines, err)
		}
		if frame.JSONRPC != "2.0" {
			t.Fatalf("line %d omitted JSON-RPC version", lines)
		}
		if frame.Method != "" {
			methods = append(methods, frame.Method)
		}
		if frame.Method == "sync.start" {
			var params syncStartParams
			if err := json.Unmarshal(frame.Params, &params); err != nil {
				t.Fatalf("decode sync.start: %v", err)
			}
			if len(params.SourceIDs) != 1 || !params.Plan {
				t.Fatalf("invalid sync fixture start: %+v", params)
			}
		}
		if frame.Method == "sync.event" && strings.Contains(string(frame.Params), `"Progress"`) {
			t.Fatalf("structured progress leaked exported Go names: %s", frame.Params)
		}
		switch frame.Method {
		case "ui.selectRows":
			selectRequests++
		case "ui.confirm":
			if !sawFinalSelection {
				t.Fatal("confirmation arrived before final selection")
			}
			sawConfirmation = true
		case "sync.event", "sync.progress":
			if !sawConfirmation {
				t.Fatal("execution event arrived before confirmation")
			}
			sawExecution = true
		}
		if frame.Method == "" && len(frame.Result) > 0 {
			var result struct {
				SelectedIndices []int `json:"selected_indices"`
				Rebuild         bool  `json:"rebuild"`
			}
			if err := json.Unmarshal(frame.Result, &result); err == nil {
				if result.Rebuild {
					sawRebuildReply = true
				}
				if !result.Rebuild && len(result.SelectedIndices) == 1 &&
					result.SelectedIndices[0] == 7 {
					if !sawRebuildReply || selectRequests != 2 {
						t.Fatal("final selection did not follow the rebuild request")
					}
					sawFinalSelection = true
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lines != 15 {
		t.Fatalf("unexpected fixture length: %d", lines)
	}
	if !sawRebuildReply || !sawFinalSelection || !sawConfirmation || !sawExecution {
		t.Fatalf(
			"incomplete scripted flow: rebuild=%t selection=%t confirmation=%t execution=%t",
			sawRebuildReply, sawFinalSelection, sawConfirmation, sawExecution,
		)
	}
	joined := strings.Join(methods, ",")
	for _, required := range []string{
		"session.initialize", "sync.start", "ui.selectRows", "ui.confirm",
		"sync.event", "sync.progress", "run.finished", "session.shutdown",
	} {
		if !strings.Contains(joined, required) {
			t.Fatalf("fixture missing %s: %s", required, joined)
		}
	}
}

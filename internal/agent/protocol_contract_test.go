package agent

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestProtocolV2GoldenInventoryAndMethodGroups(t *testing.T) {
	payload, err := os.ReadFile("testdata/protocol_v2_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ProtocolVersion int      `json:"protocol_version"`
		Methods         []string `json:"methods"`
		Groups          []struct {
			Group    string         `json:"group"`
			Method   string         `json:"method"`
			Request  map[string]any `json:"request"`
			Response map[string]any `json:"response"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.ProtocolVersion != ProtocolVersion {
		t.Fatalf("golden protocol version=%d, implementation=%d", fixture.ProtocolVersion, ProtocolVersion)
	}
	if !reflect.DeepEqual(fixture.Methods, protocolMethods) {
		t.Fatalf("golden method inventory drifted:\ngolden=%v\nactual=%v", fixture.Methods, protocolMethods)
	}
	expectedGroups := map[string]bool{
		"session": true, "runs": true, "sync": true, "config": true,
		"doctor": true, "credentials": true, "startup_sources": true,
		"playlists": true, "freedl": true, "rekordbox": true,
	}
	methods := map[string]bool{}
	for _, method := range protocolMethods {
		methods[method] = true
	}
	for _, group := range fixture.Groups {
		if !expectedGroups[group.Group] {
			t.Fatalf("unexpected golden group %q", group.Group)
		}
		delete(expectedGroups, group.Group)
		if !methods[group.Method] {
			t.Fatalf("golden method %q is not advertised", group.Method)
		}
		if group.Request == nil || group.Response == nil {
			t.Fatalf("golden group %q must include request and response objects", group.Group)
		}
	}
	if len(expectedGroups) != 0 {
		t.Fatalf("missing golden method groups: %v", expectedGroups)
	}
}

func TestGoDecodesSwiftGeneratedRequests(t *testing.T) {
	path := filepath.Join("..", "..", "macos", "UDLTests", "Fixtures", "swift_requests_v2.ndjson")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	seen := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var message envelope
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			t.Fatalf("decode Swift request: %v", err)
		}
		if message.JSONRPC != "2.0" || message.Method == "" || len(message.ID) == 0 {
			t.Fatalf("invalid Swift request envelope: %+v", message)
		}
		seen[message.Method] = true
		switch message.Method {
		case "session.initialize":
			var params initializeParams
			if err := json.Unmarshal(message.Params, &params); err != nil || params.ProtocolVersion != ProtocolVersion {
				t.Fatalf("decode initialize params: %+v err=%v", params, err)
			}
		case "run.cancel":
			var params struct {
				RunID string `json:"run_id"`
			}
			if err := json.Unmarshal(message.Params, &params); err != nil || params.RunID == "" {
				t.Fatalf("decode cancel params: %+v err=%v", params, err)
			}
		case "sync.start":
			var params syncStartParams
			if err := json.Unmarshal(message.Params, &params); err != nil ||
				!params.Plan || len(params.SourceIDs) != 1 {
				t.Fatalf("decode sync params: %+v err=%v", params, err)
			}
		case "credentials.clear":
			var params credentialMutationParams
			if err := json.Unmarshal(message.Params, &params); err != nil || params.Kind == "" {
				t.Fatalf("decode credential params: %+v err=%v", params, err)
			}
		default:
			t.Fatalf("unexpected Swift fixture method %q", message.Method)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 4 {
		t.Fatalf("decoded %d Swift fixture methods, want 4", len(seen))
	}
}

//go:build darwin

package playlistmirror

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/playlists"
)

// Exercise the real Swift DTO, with the RPC encoder's date strategy, rather
// than only a Go JSON round trip: fractional Date re-encoding used to strip
// generated_at precision and invalidate every native-app apply checksum.
func TestPlanChecksumSurvivesSwiftApplyRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("Swift toolchain unavailable")
	}
	_, source, _, _ := runtime.Caller(0)
	repo := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	dir := t.TempDir()
	runner := filepath.Join(dir, "Runner.swift")
	code := `import Foundation
@main struct RoundTrip {
 static func main() throws {
  let input = FileHandle.standardInput.readDataToEndOfFile()
  let plan = try JSONDecoder.agent.decode(PlaylistSyncPlan.self, from: input)
  let encoder = JSONEncoder.agent
  let request = PlaylistSyncApplyParams(plan: plan, dryRun: false)
  FileHandle.standardOutput.write(try encoder.encode(request))
 }
}`
	if err := os.WriteFile(runner, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "roundtrip")
	args := []string{"swiftc", "-swift-version", "6", "-parse-as-library",
		filepath.Join(repo, "macos/UDL/Backend/JSONValue.swift"),
		filepath.Join(repo, "macos/UDL/Backend/JSONRPCConnection.swift"),
		filepath.Join(repo, "macos/UDL/Backend/Wire/WireModels.swift"),
		filepath.Join(repo, "macos/UDL/Features/Playlists/PlaylistModels.swift"),
		filepath.Join(repo, "macos/UDL/Features/PlaylistSync/PlaylistSyncModels.swift"), runner, "-o", binary}
	if output, err := exec.Command("xcrun", args...).CombinedOutput(); err != nil {
		t.Fatalf("compile real Swift DTOs: %v\n%s", err, output)
	}
	req := BuildRequest{Job: mirrorJob(), Direction: playlists.DirectionNavidromeToRekordbox,
		Navidrome:        Playlist{Provider: ProviderNavidrome, ID: "nd", Name: "favs", Tracks: []Track{mirrorTrack(1, "s1", "One", "/Music/one.mp3")}},
		Rekordbox:        Playlist{Provider: ProviderRekordbox, ID: "rb", Name: "favs", Tracks: []Track{mirrorTrack(1, "c1", "One", "/Music/one.mp3")}},
		RekordboxCatalog: RekordboxCatalog{"/Music/one.mp3": {{ID: "c1"}}}}
	for _, nanos := range []int64{0, 123456789, 900000000} {
		t.Run(time.Unix(10, nanos).Format(time.RFC3339Nano), func(t *testing.T) {
			original, err := BuildPlan(req, time.Unix(10, nanos))
			if err != nil {
				t.Fatal(err)
			}
			input, err := json.Marshal(original)
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			go func() { defer stdin.Close(); _, _ = stdin.Write(input) }()
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("Swift decode/encode: %v", err)
			}
			var applied struct {
				Plan   Plan `json:"plan"`
				DryRun bool `json:"dry_run"`
			}
			if err := json.Unmarshal(output, &applied); err != nil {
				t.Fatal(err)
			}
			if !applied.Plan.GeneratedAt.Equal(original.GeneratedAt) {
				t.Fatalf("timestamp changed: %s => %s", original.GeneratedAt, applied.Plan.GeneratedAt)
			}
			if err := VerifyPlan(applied.Plan); err != nil {
				t.Fatalf("Swift apply lost checksum validity: %v\n%s", err, output)
			}
			if applied.Plan.ChecksumSHA256 != original.ChecksumSHA256 {
				t.Fatal("checksum changed")
			}
		})
	}
}

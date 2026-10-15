package playlistmirror

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/playlists"
)

func mirrorJob() playlists.SyncJob {
	return playlists.SyncJob{
		ID:        "favs-august",
		Rekordbox: playlists.PlaylistSelector{Playlist: "favs_august"},
		Navidrome: playlists.PlaylistSelector{Playlist: "favs_august"},
	}
}

func mirrorTrack(index int, id, title, path string) Track {
	return Track{Index: index, ProviderID: id, Title: title, RawPath: path, NormalizedPath: path}
}

func TestBuildPlanRekordboxToNavidromeClassifiesRows(t *testing.T) {
	req := BuildRequest{
		Job: mirrorJob(), Direction: playlists.DirectionRekordboxToNavidrome,
		Rekordbox: Playlist{Provider: ProviderRekordbox, ID: "rb", Name: "favs_august", Tracks: []Track{
			mirrorTrack(1, "c1", "One", "/Music/one.mp3"),
			mirrorTrack(2, "c2", "Two", "/Music/two.mp3"),
			mirrorTrack(3, "c3", "Three", "/Music/three.mp3"),
		}},
		Navidrome: Playlist{Provider: ProviderNavidrome, ID: "nd", Name: "favs_august", Owner: "dj", Tracks: []Track{
			mirrorTrack(1, "s1", "One", "/Music/one.mp3"),
			mirrorTrack(2, "old", "Old", "/Music/old.mp3"),
		}},
		NavidromeUsername: "dj",
		NavidromeCatalog: NavidromeCatalog{
			"/Music/one.mp3":   {{ID: "s1"}},
			"/Music/two.mp3":   {{ID: "s2"}},
			"/Music/three.mp3": {{ID: "s3"}},
		},
	}
	plan, err := BuildPlan(req, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPlanApplicable(plan); err != nil {
		t.Fatal(err)
	}
	if plan.Summary.WillKeep != 1 || plan.Summary.WillAdd != 2 || plan.Summary.WillRemove != 1 || plan.Summary.FinalTotal != 3 {
		t.Fatalf("unexpected summary: %#v", plan.Summary)
	}
	if len(plan.Rows) != 4 || plan.Rows[3].Action != ActionRemove {
		t.Fatalf("unexpected rows: %#v", plan.Rows)
	}
}

func TestBuildPlanNavidromeToRekordboxPreservesSourceOrder(t *testing.T) {
	req := BuildRequest{
		Job: mirrorJob(), Direction: playlists.DirectionNavidromeToRekordbox,
		Navidrome: Playlist{Provider: ProviderNavidrome, ID: "nd", Name: "favs_august", Tracks: []Track{
			mirrorTrack(1, "s2", "Two", "/Music/two.mp3"),
			mirrorTrack(2, "s1", "One", "/Music/one.mp3"),
		}},
		Rekordbox: Playlist{Provider: ProviderRekordbox, ID: "rb", Name: "favs_august", Tracks: []Track{
			mirrorTrack(1, "c1", "One", "/Music/one.mp3"),
			mirrorTrack(2, "c2", "Two", "/Music/two.mp3"),
		}},
		RekordboxCatalog: RekordboxCatalog{
			"/Music/one.mp3": {{ID: "c1"}},
			"/Music/two.mp3": {{ID: "c2"}},
		},
	}
	plan, err := BuildPlan(req, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.WillMove != 2 || plan.Preconditions.Matched[0].DestinationProviderID != "c2" || plan.Preconditions.Matched[1].DestinationProviderID != "c1" {
		t.Fatalf("source order was not preserved: summary=%#v matched=%#v", plan.Summary, plan.Preconditions.Matched)
	}
}

func TestBuildPlanNoOpAndDestinationCreation(t *testing.T) {
	req := BuildRequest{
		Job: mirrorJob(), Direction: playlists.DirectionRekordboxToNavidrome,
		Rekordbox:        Playlist{Provider: ProviderRekordbox, ID: "rb", Name: "favs_august", Tracks: []Track{mirrorTrack(1, "c1", "One", "/Music/one.mp3")}},
		Navidrome:        Playlist{Provider: ProviderNavidrome, ID: "nd", Name: "favs_august", Owner: "dj", Tracks: []Track{mirrorTrack(1, "s1", "One", "/Music/one.mp3")}},
		NavidromeCatalog: NavidromeCatalog{"/Music/one.mp3": {{ID: "s1"}}}, NavidromeUsername: "dj",
	}
	plan, err := BuildPlan(req, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Summary.WillKeep != 1 || plan.Summary.WillAdd+plan.Summary.WillMove+plan.Summary.WillRemove != 0 {
		t.Fatalf("expected no-op plan: %#v", plan.Summary)
	}
	req.Navidrome = Playlist{Provider: ProviderNavidrome, Name: "favs_august", Missing: true, Tracks: []Track{}}
	created, err := BuildPlan(req, time.Unix(10, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !created.DestinationCreate || created.Summary.WillAdd != 1 || len(created.Blockers) != 0 {
		t.Fatalf("unexpected creation plan: %#v", created)
	}
}

func TestBuildPlanRecordsEverySafetyBlocker(t *testing.T) {
	tests := []struct {
		name string
		edit func(*BuildRequest)
		want string
	}{
		{name: "empty source", edit: func(req *BuildRequest) { req.Rekordbox.Tracks = nil }, want: "is empty"},
		{name: "missing source", edit: func(req *BuildRequest) {
			req.Rekordbox.Missing = true
			req.Rekordbox.Tracks = nil
		}, want: "was not found"},
		{name: "missing path", edit: func(req *BuildRequest) { req.Rekordbox.Tracks[0].NormalizedPath = "" }, want: "does not expose a real path"},
		{name: "unmatched", edit: func(req *BuildRequest) { req.NavidromeCatalog = NavidromeCatalog{} }, want: "no exact normalized"},
		{name: "ambiguous path", edit: func(req *BuildRequest) {
			req.NavidromeCatalog["/Music/one.mp3"] = []navidrome.Song{{ID: "s1"}, {ID: "s2"}}
		}, want: "share this normalized path"},
		{name: "duplicate source", edit: func(req *BuildRequest) {
			req.Rekordbox.Tracks = append(req.Rekordbox.Tracks, mirrorTrack(2, "c2", "Again", "/Music/one.mp3"))
		}, want: "duplicate canonical source"},
		{name: "duplicate destination", edit: func(req *BuildRequest) {
			req.Navidrome.Tracks = []Track{
				mirrorTrack(1, "s1", "One", "/Music/one.mp3"),
				mirrorTrack(2, "s2", "Again", "/Music/one.mp3"),
			}
		}, want: "share normalized path"},
		{name: "smart destination", edit: func(req *BuildRequest) { req.Navidrome.Smart = true }, want: "UDL-managed"},
		{name: "unowned destination", edit: func(req *BuildRequest) { req.Navidrome.Owner = "other" }, want: "owned by"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := BuildRequest{
				Job: mirrorJob(), Direction: playlists.DirectionRekordboxToNavidrome,
				Rekordbox:        Playlist{Provider: ProviderRekordbox, ID: "rb", Name: "favs", Tracks: []Track{mirrorTrack(1, "c1", "One", "/Music/one.mp3")}},
				Navidrome:        Playlist{Provider: ProviderNavidrome, ID: "nd", Name: "favs", Owner: "dj", Tracks: []Track{}},
				NavidromeCatalog: NavidromeCatalog{"/Music/one.mp3": {{ID: "s1"}}}, NavidromeUsername: "dj",
			}
			test.edit(&req)
			plan, err := BuildPlan(req, time.Unix(10, 0))
			if err != nil {
				t.Fatal(err)
			}
			if err := VerifyPlanApplicable(plan); err == nil {
				t.Fatal("blocked plan was applicable")
			}
			if !strings.Contains(strings.Join(plan.Blockers, " | "), test.want) {
				t.Fatalf("blockers %v do not contain %q", plan.Blockers, test.want)
			}
		})
	}
}

func TestPlanChecksumIsDeterministicAndRoundTrips(t *testing.T) {
	req := BuildRequest{
		Job: mirrorJob(), Direction: playlists.DirectionNavidromeToRekordbox,
		Navidrome:        Playlist{Provider: ProviderNavidrome, ID: "nd", Name: "favs", Tracks: []Track{mirrorTrack(1, "s1", "One", "/Music/one.mp3")}},
		Rekordbox:        Playlist{Provider: ProviderRekordbox, ID: "rb", Name: "favs", Tracks: []Track{mirrorTrack(1, "c1", "One", "/Music/one.mp3")}},
		RekordboxCatalog: RekordboxCatalog{"/Music/one.mp3": {{ID: "c1"}}},
	}
	when := time.Unix(10, 0)
	first, err := BuildPlan(req, when)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlan(req, when)
	if err != nil {
		t.Fatal(err)
	}
	if first.ChecksumSHA256 != second.ChecksumSHA256 {
		t.Fatalf("equivalent plans differ: %s != %s", first.ChecksumSHA256, second.ChecksumSHA256)
	}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := WritePlan(path, first); err != nil {
		t.Fatal(err)
	}
	reread, err := ReadPlan(path)
	if err != nil || reread.ChecksumSHA256 != first.ChecksumSHA256 {
		t.Fatalf("round trip = %#v, %v", reread, err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	payload = []byte(strings.Replace(string(payload), `"title": "One"`, `"title": "Tampered"`, 1))
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPlan(path); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered plan error = %v", err)
	}
}

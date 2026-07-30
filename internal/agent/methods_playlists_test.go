package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/playlists"
)

type agentPlaylistProvider struct {
	readStarted chan struct{}
}

func (p agentPlaylistProvider) List(context.Context) ([]playlists.ProviderPlaylist, error) {
	return []playlists.ProviderPlaylist{{Name: "Favourites", ID: "provider-id", TrackCount: 2}}, nil
}

func (p agentPlaylistProvider) Read(ctx context.Context, _ playlists.Definition) (playlists.ProviderPlaylist, []playlists.Track, error) {
	if p.readStarted != nil {
		close(p.readStarted)
	}
	<-ctx.Done()
	return playlists.ProviderPlaylist{}, nil, ctx.Err()
}

type synchronizedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(payload)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestPlaylistCacheAndConfigMethodsDoNotReadProvider(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	playlistPath := dir + "/udl.playlists.yaml"
	definition := playlists.Definition{
		ID: "favorites", Name: "Favorites", Provider: playlists.ProviderAppleMusic,
		ProviderPlaylist: "Favourites", ProviderPlaylistID: "provider-id",
	}
	if err := playlists.Save(playlistPath, playlists.Config{
		Version: playlists.ConfigVersion, Playlists: []playlists.Definition{definition},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := playlists.WriteSnapshot(dir+"/state", playlists.Snapshot{
		PlaylistID: "favorites", Name: "Favorites", Provider: playlists.ProviderAppleMusic,
		ProviderPlaylist: "Favourites", RefreshedAt: time.Unix(10, 0).UTC(),
		Tracks: []playlists.Track{{ProviderID: "one", Title: "One"}},
	}); err != nil {
		t.Fatal(err)
	}
	readStarted := make(chan struct{})
	service := playlists.Service{Provider: agentPlaylistProvider{readStarted: readStarted}}
	server := &Server{
		WorkingDir: dir, ConfigPath: mainPath, PlaylistsConfigPath: playlistPath,
		PlaylistService: &service,
	}

	listValue, rpcErr := server.listPlaylists()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	rows := listValue.(map[string]any)["playlists"].([]playlistListRow)
	if len(rows) != 1 || rows[0].Snapshot == nil || rows[0].Snapshot.Tracks[0].Title != "One" {
		t.Fatalf("unexpected cached list: %+v", rows)
	}
	showValue, rpcErr := server.showPlaylist(mustJSON(t, playlistIDParams{PlaylistID: "favorites"}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if showValue.(map[string]any)["snapshot"].(playlists.Snapshot).PlaylistID != "favorites" {
		t.Fatalf("unexpected show result: %+v", showValue)
	}
	select {
	case <-readStarted:
		t.Fatal("cache-only list/show read the provider")
	default:
	}

	readValue, rpcErr := server.readPlaylistsConfig()
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	read := readValue.(playlistConfigResult)
	if read.Path != playlistPath || !strings.Contains(read.Content, "favorites") {
		t.Fatalf("unexpected config read: %+v", read)
	}
	read.Config.Playlists[0].Name = "My Favorites"
	if _, rpcErr := server.writePlaylistsConfig(mustJSON(t, map[string]any{"config": read.Config})); rpcErr != nil {
		t.Fatal(rpcErr)
	}
	createdValue, rpcErr := server.savePlaylistDefinition(mustJSON(t, map[string]any{
		"definition": playlists.Definition{
			ID: "later", Name: "Later", Provider: playlists.ProviderAppleMusic,
			ProviderPlaylist: "Later",
		},
	}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if !createdValue.(map[string]any)["created"].(bool) {
		t.Fatalf("new definition was not created: %+v", createdValue)
	}
}

func TestPlaylistProviderAndRefreshRunsAreCancelable(t *testing.T) {
	dir := t.TempDir()
	mainPath := writeAgentTestConfig(t, dir)
	playlistPath := dir + "/udl.playlists.yaml"
	definition := playlists.Definition{
		ID: "favorites", Name: "Favorites", Provider: playlists.ProviderAppleMusic,
		ProviderPlaylist: "Favourites",
	}
	if err := playlists.Save(playlistPath, playlists.Config{
		Version: playlists.ConfigVersion, Playlists: []playlists.Definition{definition},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := playlists.WriteSnapshot(dir+"/state", playlists.Snapshot{
		PlaylistID: "favorites", Name: "Favorites", Provider: playlists.ProviderAppleMusic,
		ProviderPlaylist: "Favourites", RefreshedAt: time.Unix(10, 0).UTC(),
		Tracks: []playlists.Track{{ProviderID: "old", Title: "Old"}},
	}); err != nil {
		t.Fatal(err)
	}
	readStarted := make(chan struct{})
	service := playlists.Service{Provider: agentPlaylistProvider{readStarted: readStarted}}
	output := &synchronizedBuffer{}
	server := &Server{
		Conn: NewConn(strings.NewReader(""), output), Runs: NewRunRegistry(),
		WorkingDir: dir, ConfigPath: mainPath, PlaylistsConfigPath: playlistPath,
		PlaylistService: &service, runContext: context.Background(),
	}

	providerValue, rpcErr := server.listProviderPlaylists(nil)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if providerValue.(map[string]string)["run_id"] == "" {
		t.Fatal("provider list did not start a run")
	}
	waitForNotifications(t, output, 1)

	refreshValue, rpcErr := server.refreshPlaylist(mustJSON(t, playlistIDParams{PlaylistID: "favorites"}))
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	refreshID := refreshValue.(map[string]string)["run_id"]
	select {
	case <-readStarted:
	case <-time.After(time.Second):
		t.Fatal("refresh did not reach provider")
	}
	if !server.Runs.Cancel(refreshID) {
		t.Fatal("refresh run was not cancelable")
	}
	waitForNotifications(t, output, 2)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	var finished runFinishedParams
	var frame envelope
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &frame); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(frame.Params, &finished); err != nil {
		t.Fatal(err)
	}
	if finished.RunID != refreshID || finished.ExitCode != exitcode.Interrupted {
		t.Fatalf("unexpected canceled refresh result: %+v", finished)
	}
	snapshot, err := playlists.LoadSnapshot(dir+"/state", "favorites")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Tracks) != 1 || snapshot.Tracks[0].ProviderID != "old" {
		t.Fatalf("canceled refresh replaced the valid snapshot: %+v", snapshot)
	}
}

func waitForNotifications(t *testing.T, output *synchronizedBuffer, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(strings.TrimSpace(output.String()), "\n")+1 >= count && strings.TrimSpace(output.String()) != "" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d notifications: %s", count, output.String())
}

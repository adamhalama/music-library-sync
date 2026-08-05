package navidrome

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// resolvedPlaylistsDirName is the managed playlists folder name inside the
// music root, excluded from the audio manifest.
const resolvedPlaylistsDirName = DefaultPlaylistsPath

// EnvAcceptanceBinary opts in to the real-server acceptance test by naming an
// absolute path to a `navidrome` executable.
const EnvAcceptanceBinary = "UDL_NAVIDROME_ACCEPTANCE_BINARY"

// fileManifest records everything that must be identical before and after a
// server run. PLAN.md's central promise is that UDL never rewrites audio
// contents, creation times, or modification times, and this is what proves it.
type fileManifest struct {
	RelPath  string
	SHA256   string
	Size     int64
	Modified time.Time
	Birth    time.Time
}

// TestNavidromeIsolatedAcceptance runs a real Navidrome against copied
// fixtures in a temporary tree and verifies scanning, ordering, stars,
// backups, restart, and — above all — that not one source byte or timestamp
// changed.
//
// It is opt-in because it needs a real binary. Without one it skips rather
// than silently passing.
func TestNavidromeIsolatedAcceptance(t *testing.T) {
	binary := strings.TrimSpace(os.Getenv(EnvAcceptanceBinary))
	if binary == "" {
		t.Skipf("set %s to an absolute navidrome path to run the real-server acceptance test", EnvAcceptanceBinary)
	}
	if !filepath.IsAbs(binary) {
		t.Fatalf("%s must be an absolute path, got %q", EnvAcceptanceBinary, binary)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("stat %s: %v", binary, err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is required to generate acceptance fixtures")
	}

	root := t.TempDir()
	music := filepath.Join(root, "music")
	data := filepath.Join(root, "data")
	cache := filepath.Join(root, "cache")
	backups := filepath.Join(root, "backups")
	for _, dir := range []string{music, data, cache, backups} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	// Fixtures cover what the ordering rule has to get right: distinct
	// creation times, a deliberate tie broken by path, multiple genres, and a
	// Unicode path that must survive NFC folding.
	fixtures := []struct {
		rel     string
		title   string
		genre   string
		created time.Time
	}{
		{"a-newest.mp3", "Newest", "Hard Bounce", time.Now().Add(-1 * time.Hour)},
		{"b-tie-one.mp3", "Tie One", "Hard Bounce", time.Now().Add(-3 * time.Hour)},
		{"c-tie-two.mp3", "Tie Two", "Bounce", time.Now().Add(-3 * time.Hour)},
		{"Café/d-oldest.mp3", "Oldest", "Techno", time.Now().Add(-9 * time.Hour)},
	}
	for _, fixture := range fixtures {
		path := filepath.Join(music, fixture.rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		generateFixture(t, ffmpeg, path, fixture.title, fixture.genre)
		// Creation time is Date Added, so it is set explicitly rather than left
		// to whatever order the files happened to be written in.
		setBirthTime(t, path, fixture.created)
	}

	// The manifest deliberately excludes the managed playlists directory: it
	// lives inside the music root because Navidrome's PlaylistsPath is relative
	// to MusicFolder, and UDL writes its own .nsp files there by design. The
	// promise being verified is about the audio, not about UDL's own artifacts.
	before := manifest(t, music, resolvedPlaylistsDirName)

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.Server.Username = "acceptance"
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.Port = freeLoopbackPort(t)
	cfg.Server.URL = fmt.Sprintf("http://127.0.0.1:%d", cfg.Server.Port)
	cfg.Paths.MusicDir = music
	cfg.Paths.DataDir = data
	cfg.Paths.CacheDir = cache
	cfg.Paths.BackupDir = backups
	cfg.Paths.LogFile = filepath.Join(root, "navidrome.log")
	cfg.Paths.ConfigFile = filepath.Join(root, "navidrome.toml")
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := os.MkdirAll(resolved.PlaylistsDir, 0o755); err != nil {
		t.Fatalf("mkdir playlists: %v", err)
	}
	if err := AtomicWrite(resolved.ConfigFile, []byte(RenderTOML(cfg, resolved, "/opt/homebrew/bin/ffmpeg")), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	const password = "acceptance-password"
	// Navidrome's dev auto-create hook names the account `admin` regardless of
	// anything in the config, so the acceptance account is admin, not the
	// configured username.
	cfg.Server.Username = "admin"
	server := startAcceptanceServer(t, ctx, binary, resolved.ConfigFile, password)
	client := NewClient(cfg.Server.URL, Credentials{Username: cfg.Server.Username, Password: password})
	waitForPing(t, ctx, client)

	if err := client.StartScan(ctx, true); err != nil {
		t.Fatalf("StartScan: %v", err)
	}
	songs := waitForSongs(t, ctx, client, len(fixtures))

	// Every fixture must be indexed at its real path.
	byPath := map[string]Song{}
	for _, song := range songs {
		byPath[song.Path] = song
	}
	for _, fixture := range fixtures {
		want := NormalizePath(filepath.Join(music, fixture.rel))
		song, ok := byPath[want]
		if !ok {
			t.Fatalf("Navidrome did not index %s; indexed paths: %v", want, indexedPaths(songs))
		}
		if len(song.Genres) == 0 {
			t.Fatalf("%s was indexed with no genre", want)
		}
	}

	// Managed playlists must import and order newest-first with a
	// path-deterministic tie-break.
	cfg.Playlists.HardBounceGenres = []string{"Hard Bounce"}
	refresh, err := RefreshManagedPlaylists(ctx, cfg, resolved, client)
	if err != nil {
		t.Fatalf("RefreshManagedPlaylists: %v", err)
	}
	if len(refresh.Generated) != 3 {
		t.Fatalf("generated = %+v", refresh.Generated)
	}
	all := waitForPlaylist(t, ctx, client, SmartPlaylistAllName, len(fixtures))
	if len(all) != len(fixtures) {
		t.Fatalf("All Music has %d tracks, want %d", len(all), len(fixtures))
	}
	// `-dateadded` is descending `media_file.created_at`, which is scan
	// discovery order — NOT APFS birth time. See the ManagedSort comment and
	// the deviation in IMPLEMENTATION.md. What this asserts is that the sort is
	// honoured at all and is strictly monotonic, so a silently-ignored sort
	// field cannot pass unnoticed.
	for i := 1; i < len(all); i++ {
		if !all[i-1].Created.After(all[i].Created) {
			t.Fatalf("All Music is not ordered newest-added first: %s (%s) precedes %s (%s)",
				filepath.Base(all[i-1].Path), all[i-1].Created,
				filepath.Base(all[i].Path), all[i].Created)
		}
	}

	// The Hard Bounce allowlist must be an exact genre match, not a prefix.
	hardBounce := waitForPlaylist(t, ctx, client, SmartPlaylistHardBounceName, 2)
	hardBounceNames := map[string]bool{}
	for _, song := range hardBounce {
		hardBounceNames[filepath.Base(song.Path)] = true
	}
	if len(hardBounce) != 2 || !hardBounceNames["a-newest.mp3"] || !hardBounceNames["b-tie-one.mp3"] {
		t.Fatalf("HARD BOUNCE = %v, want exactly the two Hard Bounce tracks", hardBounceNames)
	}

	// Every managed playlist must belong to the shared account, or personalized
	// results would refer to someone else's data.
	playlists, err := client.Playlists(ctx)
	if err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	if problems := VerifyPlaylistOwnership(playlists, cfg.Server.Username); len(problems) != 0 {
		t.Fatalf("ownership problems: %v", problems)
	}

	// Stars round-trip through the API.
	target := songs[0]
	if err := client.Star(ctx, target.ID); err != nil {
		t.Fatalf("Star: %v", err)
	}
	starred, err := client.Starred(ctx)
	if err != nil {
		t.Fatalf("Starred: %v", err)
	}
	if len(starred) != 1 || starred[0].ID != target.ID {
		t.Fatalf("starred = %+v", starred)
	}

	// A backup must actually produce a file.
	backupResult, err := CreateBackup(ctx, DefaultCommandRunner, binary, resolved)
	if err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if info, statErr := os.Stat(backupResult.Backup.Path); statErr != nil || info.Size() == 0 {
		t.Fatalf("backup %s is missing or empty", backupResult.Backup.Path)
	}

	// A restart must preserve the star, so state lives in the database rather
	// than in memory.
	server.stop(t)
	server = startAcceptanceServer(t, ctx, binary, resolved.ConfigFile, password)
	waitForPing(t, ctx, client)
	starred, err = client.Starred(ctx)
	if err != nil {
		t.Fatalf("Starred after restart: %v", err)
	}
	if len(starred) != 1 {
		t.Fatalf("the star did not survive a restart: %+v", starred)
	}
	server.stop(t)

	// The headline promise: not one source byte or timestamp changed.
	after := manifest(t, music, resolvedPlaylistsDirName)
	if len(before) != len(after) {
		t.Fatalf("the source file set changed: %d before, %d after", len(before), len(after))
	}
	for rel, was := range before {
		now, ok := after[rel]
		if !ok {
			t.Fatalf("%s disappeared during the server run", rel)
		}
		if was.SHA256 != now.SHA256 {
			t.Fatalf("%s contents changed", rel)
		}
		if was.Size != now.Size {
			t.Fatalf("%s size changed from %d to %d", rel, was.Size, now.Size)
		}
		if !was.Modified.Equal(now.Modified) {
			t.Fatalf("%s modification time changed from %s to %s", rel, was.Modified, now.Modified)
		}
		if !was.Birth.Equal(now.Birth) {
			t.Fatalf("%s birth time changed from %s to %s", rel, was.Birth, now.Birth)
		}
	}

	// Nothing but UDL's own .nsp files may have appeared inside the music root.
	playlistEntries, err := os.ReadDir(resolved.PlaylistsDir)
	if err != nil {
		t.Fatalf("read playlists dir: %v", err)
	}
	for _, entry := range playlistEntries {
		if filepath.Ext(entry.Name()) != ".nsp" {
			t.Fatalf("unexpected file %s in the managed playlists directory", entry.Name())
		}
	}

	// Every managed artifact lives under the temp root, so cleanup is exact.
	for _, path := range []string{resolved.ConfigFile, resolved.DataDir, resolved.BackupDir, resolved.PlaylistsDir} {
		if !strings.HasPrefix(path, root) {
			t.Fatalf("%s escaped the temporary acceptance root %s", path, root)
		}
	}
}

type acceptanceServer struct {
	cmd  *exec.Cmd
	logs *os.File
}

func startAcceptanceServer(t *testing.T, ctx context.Context, binary, configFile, adminPassword string) *acceptanceServer {
	t.Helper()
	logs, err := os.CreateTemp(t.TempDir(), "navidrome-*.log")
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	cmd := exec.CommandContext(ctx, binary, "--configfile", configFile)
	// The variable has to be on the *child's* environment. Setting it on the
	// test process does nothing, which is exactly how the first run of this
	// test failed.
	cmd.Env = append(os.Environ(), "ND_DEVAUTOCREATEADMINPASSWORD="+adminPassword)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		t.Fatalf("start navidrome: %v", err)
	}
	server := &acceptanceServer{cmd: cmd, logs: logs}
	t.Cleanup(func() { server.stop(t) })
	return server
}

func (s *acceptanceServer) stop(t *testing.T) {
	t.Helper()
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
	_, _ = s.cmd.Process.Wait()
	s.cmd = nil
}

func waitForPing(t *testing.T, ctx context.Context, client *Client) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := client.Ping(ctx); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("navidrome never became reachable: %v", lastErr)
}

func waitForSongs(t *testing.T, ctx context.Context, client *Client, want int) []Song {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	var songs []Song
	for time.Now().Before(deadline) {
		var err error
		songs, err = client.Songs(ctx)
		if err == nil && len(songs) >= want {
			return songs
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("the scan indexed %d of %d fixtures within the timeout", len(songs), want)
	return nil
}

func waitForPlaylist(t *testing.T, ctx context.Context, client *Client, name string, want int) []Song {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		playlist, ok, err := client.PlaylistByName(ctx, name)
		if err == nil && ok {
			_, songs, readErr := client.Playlist(ctx, playlist.ID)
			if readErr == nil && len(songs) >= want {
				return songs
			}
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("playlist %q never reached %d tracks", name, want)
	return nil
}

func generateFixture(t *testing.T, ffmpeg, path, title, genre string) {
	t.Helper()
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "anullsrc=r=44100:cl=mono", "-t", "2",
		"-c:a", "libmp3lame", "-b:a", "128k",
		"-metadata", "title="+title,
		"-metadata", "artist=Acceptance",
		"-metadata", "album=Acceptance",
		"-metadata", "genre="+genre,
		path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate %s: %v: %s", path, err, out)
	}
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a loopback port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func manifest(t *testing.T, root string, skipDirs ...string) map[string]fileManifest {
	t.Helper()
	skip := map[string]struct{}{}
	for _, dir := range skipDirs {
		skip[dir] = struct{}{}
	}
	out := map[string]fileManifest{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if _, skipped := skip[info.Name()]; skipped {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		file, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer file.Close()
		hash := sha256.New()
		if _, copyErr := io.Copy(hash, file); copyErr != nil {
			return copyErr
		}
		out[rel] = fileManifest{
			RelPath:  rel,
			SHA256:   hex.EncodeToString(hash.Sum(nil)),
			Size:     info.Size(),
			Modified: info.ModTime(),
			Birth:    birthTime(t, path),
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func indexedPaths(songs []Song) []string {
	out := make([]string, 0, len(songs))
	for _, song := range songs {
		out = append(out, song.Path)
	}
	return out
}

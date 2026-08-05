package navidrome

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAuthParamsNeverCarryPlaintextPassword(t *testing.T) {
	client := NewClient("http://localhost:4533", Credentials{Username: "jaa", Password: "hunter2"})
	values, err := client.AuthParams()
	if err != nil {
		t.Fatalf("AuthParams: %v", err)
	}
	encoded := values.Encode()
	if strings.Contains(encoded, "hunter2") {
		t.Fatalf("auth parameters leaked the password: %s", encoded)
	}
	if values.Get("p") != "" {
		t.Fatalf("UDL must never use the plaintext `p` parameter")
	}
	if values.Get("t") == "" || values.Get("s") == "" {
		t.Fatalf("salted token authentication is missing: %s", encoded)
	}
}

func TestAuthParamsUseAFreshSaltPerRequest(t *testing.T) {
	fake := newFakeServer(t)
	client := fake.Client()
	for i := 0; i < 3; i++ {
		if _, err := client.Ping(context.Background()); err != nil {
			t.Fatalf("Ping %d: %v", i, err)
		}
	}
	salts := fake.Salts()
	if len(salts) != 3 {
		t.Fatalf("salts = %v", salts)
	}
	seen := map[string]struct{}{}
	for _, salt := range salts {
		if salt == "" {
			t.Fatalf("empty salt in %v", salts)
		}
		if _, exists := seen[salt]; exists {
			t.Fatalf("salt reused across requests: %v", salts)
		}
		seen[salt] = struct{}{}
	}
}

func TestRequestsNeverPutThePasswordOnTheWire(t *testing.T) {
	fake := newFakeServer(t)
	fake.AddSong(song("1", "One", "/music/one.mp3", "Hard Bounce"))
	client := fake.Client()
	ctx := context.Background()
	if _, err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := client.Songs(ctx); err != nil {
		t.Fatalf("Songs: %v", err)
	}
	if err := client.Star(ctx, "1"); err != nil {
		t.Fatalf("Star: %v", err)
	}
	for _, raw := range fake.RawQueries {
		if strings.Contains(raw, fake.Password) {
			t.Fatalf("request query leaked the password: %s", raw)
		}
	}
}

func TestRedactURLHidesAuthentication(t *testing.T) {
	raw := "http://localhost:4533/rest/ping.view?c=udl&f=json&s=abc123&t=deadbeef&u=jaa&v=1.16.1"
	redacted := RedactURL(raw)
	for _, secret := range []string{"abc123", "deadbeef", "jaa"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted URL still contains %q: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "ping.view") {
		t.Fatalf("redaction must keep the endpoint: %s", redacted)
	}
}

func TestPingReportsServerInfo(t *testing.T) {
	fake := newFakeServer(t)
	info, err := fake.Client().Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if info.ServerVersion != "0.63.2" || !info.OpenSubsonic {
		t.Fatalf("info = %+v", info)
	}
}

func TestPingRejectsOldServer(t *testing.T) {
	fake := newFakeServer(t)
	fake.ServerVersion = "0.62.0"
	_, err := fake.Client().Ping(context.Background())
	if !errors.Is(err, ErrIncompatibleServer) {
		t.Fatalf("error = %v, want ErrIncompatibleServer", err)
	}
	if !strings.Contains(err.Error(), MinimumServerVersion) {
		t.Fatalf("error must name the minimum version: %v", err)
	}
}

func TestPingRejectsNonNavidromeServer(t *testing.T) {
	fake := newFakeServer(t)
	fake.ServerType = "subsonic"
	_, err := fake.Client().Ping(context.Background())
	if !errors.Is(err, ErrIncompatibleServer) {
		t.Fatalf("error = %v, want ErrIncompatibleServer", err)
	}
}

func TestWrongPasswordIsUnauthorized(t *testing.T) {
	fake := newFakeServer(t)
	client := NewClient(fake.URL(), Credentials{Username: "jaa", Password: "wrong"})
	_, err := client.Ping(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
	if strings.Contains(err.Error(), "wrong") {
		t.Fatalf("error leaked the attempted password: %v", err)
	}
}

func TestMissingCredentialsFailBeforeAnyRequest(t *testing.T) {
	fake := newFakeServer(t)
	client := NewClient(fake.URL(), Credentials{Username: "jaa"})
	_, err := client.Ping(context.Background())
	if err == nil {
		t.Fatalf("expected a missing-password error")
	}
	if !strings.Contains(err.Error(), "Keychain") {
		t.Fatalf("error should point at Keychain: %v", err)
	}
	if len(fake.Requests) != 0 {
		t.Fatalf("no request may be sent without credentials: %v", fake.Requests)
	}
}

func TestMalformedResponseIsActionable(t *testing.T) {
	fake := newFakeServer(t)
	fake.Malformed["ping.view"] = true
	_, err := fake.Client().Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Navidrome server") {
		t.Fatalf("error = %v, want a 'is this a Navidrome server' hint", err)
	}
}

func TestHTTPErrorStatusIsReported(t *testing.T) {
	fake := newFakeServer(t)
	fake.SetHTTPStatus("ping.view", http.StatusBadGateway)
	_, err := fake.Client().Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("error = %v, want the HTTP status", err)
	}
}

func TestUnauthorizedHTTPStatusMapsToErrUnauthorized(t *testing.T) {
	fake := newFakeServer(t)
	fake.SetHTTPStatus("ping.view", http.StatusUnauthorized)
	_, err := fake.Client().Ping(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v, want ErrUnauthorized", err)
	}
}

func TestCanceledContextStopsWork(t *testing.T) {
	fake := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fake.Client().Ping(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestTimeoutIsReportedWithoutLeakingTheURL(t *testing.T) {
	fake := newFakeServer(t)
	fake.OnRequest = func(string, url.Values) { time.Sleep(50 * time.Millisecond) }
	client := fake.Client()
	client.HTTP = &http.Client{Timeout: 5 * time.Millisecond}
	// A fixed salt makes the derived token predictable, so the assertion can
	// look for the exact values rather than for a parameter name.
	const salt = "fixedsalt"
	client.Salt = func() (string, error) { return salt, nil }
	sum := md5.Sum([]byte(fake.Password + salt))
	token := hex.EncodeToString(sum[:])

	_, err := client.Ping(context.Background())
	if err == nil {
		t.Fatalf("expected a timeout")
	}
	for _, secret := range []string{salt, token, fake.Password, fake.Username} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("timeout error leaked %q: %v", secret, err)
		}
	}
}

func TestSongsPaginatesAndDeduplicates(t *testing.T) {
	fake := newFakeServer(t)
	for i := 0; i < pageSize+7; i++ {
		fake.AddSong(song(padID(i), "Track", "/music/"+padID(i)+".mp3"))
	}
	// A duplicate ID simulates the library shifting under a paged walk.
	fake.AddSong(song(padID(0), "Track", "/music/"+padID(0)+".mp3"))

	songs, err := fake.Client().Songs(context.Background())
	if err != nil {
		t.Fatalf("Songs: %v", err)
	}
	if len(songs) != pageSize+7 {
		t.Fatalf("songs = %d, want %d", len(songs), pageSize+7)
	}
	seen := map[string]struct{}{}
	for _, item := range songs {
		if _, exists := seen[item.ID]; exists {
			t.Fatalf("duplicate id %q survived pagination", item.ID)
		}
		seen[item.ID] = struct{}{}
	}
}

func TestSongsNormalizeGenresAndPaths(t *testing.T) {
	fake := newFakeServer(t)
	fake.AddSong(song("1", "One", " /music//sub/../one.mp3 ", "Hard Bounce", "hard bounce", "Techno"))
	songs, err := fake.Client().Songs(context.Background())
	if err != nil {
		t.Fatalf("Songs: %v", err)
	}
	if len(songs) != 1 {
		t.Fatalf("songs = %+v", songs)
	}
	if songs[0].Path != "/music/one.mp3" {
		t.Fatalf("path = %q", songs[0].Path)
	}
	// Every distinct spelling is kept: the Hard Bounce rule is an exact,
	// case-sensitive allowlist, so "Hard Bounce" and "hard bounce" are two
	// different genres as far as Navidrome is concerned.
	if len(songs[0].Genres) != 3 {
		t.Fatalf("genres = %v, want three distinct spellings", songs[0].Genres)
	}
}

func TestSongPathsAreUnicodeFolded(t *testing.T) {
	// NFD "é" as reported by a filesystem walk must fold to the NFC form Apple
	// Music hands back, or path matching misses every accented title.
	decomposed := "/music/Café.mp3"
	composed := "/music/Café.mp3"
	fake := newFakeServer(t)
	fake.AddSong(song("1", "Cafe", decomposed))
	songs, err := fake.Client().Songs(context.Background())
	if err != nil {
		t.Fatalf("Songs: %v", err)
	}
	if songs[0].Path != composed {
		t.Fatalf("path = %q, want the NFC form %q", songs[0].Path, composed)
	}
	if NormalizePath(composed) != NormalizePath(decomposed) {
		t.Fatalf("NormalizePath must fold both Unicode forms together")
	}
}

func TestPlaylistReadsOrderedMembership(t *testing.T) {
	fake := newFakeServer(t)
	fake.AddSong(song("1", "One", "/music/one.mp3"))
	fake.AddSong(song("2", "Two", "/music/two.mp3"))
	fake.AddSong(song("3", "Three", "/music/three.mp3"))
	fake.AddPlaylist("pl-1", "All Music", "jaa", "3", "1", "2")

	playlist, songs, err := fake.Client().Playlist(context.Background(), "pl-1")
	if err != nil {
		t.Fatalf("Playlist: %v", err)
	}
	if playlist.Owner != "jaa" {
		t.Fatalf("owner = %q", playlist.Owner)
	}
	got := []string{}
	for _, item := range songs {
		got = append(got, item.ID)
	}
	want := []string{"3", "1", "2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestPlaylistByNameFallsBackToCaseInsensitive(t *testing.T) {
	fake := newFakeServer(t)
	fake.AddPlaylist("pl-1", "HARD BOUNCE", "jaa")
	found, ok, err := fake.Client().PlaylistByName(context.Background(), "hard bounce")
	if err != nil || !ok {
		t.Fatalf("PlaylistByName: %v %v", ok, err)
	}
	if found.ID != "pl-1" {
		t.Fatalf("found = %+v", found)
	}
	if _, ok, err := fake.Client().PlaylistByName(context.Background(), "Nope"); err != nil || ok {
		t.Fatalf("missing playlist must report ok=false: %v %v", ok, err)
	}
}

func TestStarUnstarAndStarredRoundTrip(t *testing.T) {
	fake := newFakeServer(t)
	fake.AddSong(song("1", "One", "/music/one.mp3"))
	fake.AddSong(song("2", "Two", "/music/two.mp3"))
	client := fake.Client()
	ctx := context.Background()

	if err := client.Star(ctx, "1"); err != nil {
		t.Fatalf("Star: %v", err)
	}
	// Starring twice must stay harmless: apply retries rely on it.
	if err := client.Star(ctx, "1"); err != nil {
		t.Fatalf("repeat Star: %v", err)
	}
	starred, err := client.Starred(ctx)
	if err != nil {
		t.Fatalf("Starred: %v", err)
	}
	if len(starred) != 1 || starred[0].ID != "1" || !starred[0].Starred {
		t.Fatalf("starred = %+v", starred)
	}
	if err := client.Unstar(ctx, "1"); err != nil {
		t.Fatalf("Unstar: %v", err)
	}
	starred, err = client.Starred(ctx)
	if err != nil {
		t.Fatalf("Starred after unstar: %v", err)
	}
	if len(starred) != 0 {
		t.Fatalf("starred = %+v", starred)
	}
}

func TestStarMissingSongIsReported(t *testing.T) {
	fake := newFakeServer(t)
	err := fake.Client().Star(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestScanStartAndStatus(t *testing.T) {
	fake := newFakeServer(t)
	fake.AddSong(song("1", "One", "/music/one.mp3"))
	client := fake.Client()
	ctx := context.Background()
	if err := client.StartScan(ctx, false); err != nil {
		t.Fatalf("StartScan: %v", err)
	}
	scanning, count, err := client.ScanStatus(ctx)
	if err != nil {
		t.Fatalf("ScanStatus: %v", err)
	}
	if !scanning || count != 1 {
		t.Fatalf("scanning=%v count=%d", scanning, count)
	}
}

func TestSubsonicErrorCodesAreTranslated(t *testing.T) {
	cases := map[int]string{
		40: "Keychain",
		50: "not authorized",
		70: "not found",
	}
	for code, fragment := range cases {
		fake := newFakeServer(t)
		fake.Fail["ping.view"] = subsonicError{Code: code, Message: "server said no"}
		_, err := fake.Client().Ping(context.Background())
		if err == nil || !strings.Contains(err.Error(), fragment) {
			t.Fatalf("code %d gave %v, want a message containing %q", code, err, fragment)
		}
	}
}

func padID(i int) string {
	digits := "0000" + itoa(i)
	return digits[len(digits)-4:]
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	return out
}

func TestTransientServerErrorIsRetried(t *testing.T) {
	fake := newFakeServer(t)
	attempts := 0
	fake.OnRequest = func(endpoint string, _ url.Values) {
		if endpoint != "ping.view" {
			return
		}
		attempts++
	}
	// Fail the first attempt at the HTTP layer, then let the endpoint succeed.
	fake.SetHTTPStatus("ping.view", http.StatusServiceUnavailable)
	go func() {
		time.Sleep(100 * time.Millisecond)
		fake.ClearHTTPStatus("ping.view")
	}()
	if _, err := fake.Client().Ping(context.Background()); err != nil {
		t.Fatalf("Ping should recover from a 503: %v", err)
	}
	if attempts == 0 {
		t.Fatalf("the retry never reached the endpoint")
	}
}

func TestUnauthorizedIsNotRetried(t *testing.T) {
	fake := newFakeServer(t)
	client := NewClient(fake.URL(), Credentials{Username: "jaa", Password: "wrong"})
	if _, err := client.Ping(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("error = %v", err)
	}
	if len(fake.Requests) != 1 {
		t.Fatalf("rejected credentials must not be retried: %v", fake.Requests)
	}
}

package navidrome

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeServer is a deterministic stand-in for Navidrome. It enforces real
// salted-token authentication so a client that leaked or skipped auth fails
// here rather than in production.
type fakeServer struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	Username string
	Password string

	Songs     []rawSong
	Playlists map[string]*fakePlaylist
	Starred   map[string]bool
	Scanning  bool
	ScanCount int64

	// Requests records endpoint names in call order.
	Requests []string
	// RawQueries records the full query string of every request, so leak
	// assertions can inspect exactly what went over the wire.
	RawQueries []string

	// Fail lets a test script an error response for one endpoint.
	Fail map[string]subsonicError
	// HTTPStatus lets a test script a transport-level status per endpoint.
	HTTPStatus map[string]int
	// Malformed makes an endpoint return a non-Subsonic body.
	Malformed map[string]bool
	// OnRequest runs before each response and may mutate state.
	OnRequest func(endpoint string, query url.Values)
	// ServerVersion is reported by ping.
	ServerVersion string
	// ServerType is reported by ping.
	ServerType string
}

type fakePlaylist struct {
	rawPlaylist
	SongIDs []string
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{
		t:             t,
		Username:      "jaa",
		Password:      "hunter2",
		Playlists:     map[string]*fakePlaylist{},
		Starred:       map[string]bool{},
		Fail:          map[string]subsonicError{},
		HTTPStatus:    map[string]int{},
		Malformed:     map[string]bool{},
		ServerVersion: "0.63.2",
		ServerType:    "navidrome",
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

// Client returns a client wired to the fake server with valid credentials.
func (f *fakeServer) Client() *Client {
	return NewClient(f.server.URL, Credentials{Username: f.Username, Password: f.Password})
}

func (f *fakeServer) URL() string { return f.server.URL }

func (f *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	endpoint := strings.TrimPrefix(r.URL.Path, "/rest/")
	query := r.URL.Query()

	f.mu.Lock()
	f.Requests = append(f.Requests, endpoint)
	f.RawQueries = append(f.RawQueries, r.URL.RawQuery)
	onRequest := f.OnRequest
	scriptedStatus, hasStatus := f.HTTPStatus[endpoint]
	malformed := f.Malformed[endpoint]
	f.mu.Unlock()

	if hasStatus {
		w.WriteHeader(scriptedStatus)
		return
	}
	if malformed {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>not a subsonic server</body></html>"))
		return
	}
	if !f.authOK(query) {
		f.writeError(w, subsonicError{Code: 40, Message: "Wrong username or password"})
		return
	}
	f.mu.Lock()
	apiErr, shouldFail := f.Fail[endpoint]
	f.mu.Unlock()
	if shouldFail {
		f.writeError(w, apiErr)
		return
	}
	if onRequest != nil {
		onRequest(endpoint, query)
	}

	switch endpoint {
	case "ping.view":
		f.write(w, func(resp *envelopeBody) {
			resp.Type = f.ServerType
			resp.ServerVersion = f.ServerVersion
			resp.OpenSubsonic = true
		})
	case "search3.view":
		offset, _ := strconv.Atoi(query.Get("songOffset"))
		count, _ := strconv.Atoi(query.Get("songCount"))
		f.mu.Lock()
		songs := append([]rawSong(nil), f.Songs...)
		f.mu.Unlock()
		page := []rawSong{}
		for i := offset; i < len(songs) && len(page) < count; i++ {
			page = append(page, songs[i])
		}
		f.write(w, func(resp *envelopeBody) {
			resp.SearchResult3 = &searchResult3{Song: page}
		})
	case "getPlaylists.view":
		items := []rawPlaylist{}
		for _, playlist := range f.sortedPlaylists() {
			items = append(items, playlist.rawPlaylist)
		}
		f.write(w, func(resp *envelopeBody) { resp.Playlists = &playlistsResult{Playlist: items} })
	case "getPlaylist.view":
		playlist, ok := f.Playlists[query.Get("id")]
		if !ok {
			f.writeError(w, subsonicError{Code: 70, Message: "Playlist not found"})
			return
		}
		entries := []rawSong{}
		for _, id := range playlist.SongIDs {
			if song, found := f.songByID(id); found {
				entries = append(entries, song)
			}
		}
		detail := &playlistDetail{rawPlaylist: playlist.rawPlaylist, Entry: entries}
		f.write(w, func(resp *envelopeBody) { resp.Playlist = detail })
	case "getStarred2.view":
		starred := []rawSong{}
		f.mu.Lock()
		for _, song := range f.Songs {
			if f.Starred[song.ID] {
				copySong := song
				copySong.Starred = "2026-01-01T00:00:00Z"
				starred = append(starred, copySong)
			}
		}
		f.mu.Unlock()
		f.write(w, func(resp *envelopeBody) { resp.Starred2 = &starredResult{Song: starred} })
	case "star.view", "unstar.view":
		id := query.Get("id")
		if _, ok := f.songByID(id); !ok {
			f.writeError(w, subsonicError{Code: 70, Message: "Song not found"})
			return
		}
		f.mu.Lock()
		f.Starred[id] = endpoint == "star.view"
		f.mu.Unlock()
		f.write(w, nil)
	case "startScan.view":
		f.mu.Lock()
		f.Scanning = true
		f.ScanCount = int64(len(f.Songs))
		f.mu.Unlock()
		f.write(w, nil)
	case "getScanStatus.view":
		f.mu.Lock()
		status := &scanStatus{Scanning: f.Scanning, Count: f.ScanCount}
		f.mu.Unlock()
		f.write(w, func(resp *envelopeBody) { resp.ScanStatus = status })
	default:
		f.writeError(w, subsonicError{Code: 0, Message: "unknown endpoint " + endpoint})
	}
}

func (f *fakeServer) authOK(query url.Values) bool {
	if query.Get("u") != f.Username {
		return false
	}
	salt := query.Get("s")
	if salt == "" {
		return false
	}
	sum := md5.Sum([]byte(f.Password + salt))
	return query.Get("t") == hex.EncodeToString(sum[:])
}

func (f *fakeServer) songByID(id string) (rawSong, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, song := range f.Songs {
		if song.ID == id {
			return song, true
		}
	}
	return rawSong{}, false
}

func (f *fakeServer) sortedPlaylists() []*fakePlaylist {
	ids := make([]string, 0, len(f.Playlists))
	for id := range f.Playlists {
		ids = append(ids, id)
	}
	sortStrings(ids)
	out := make([]*fakePlaylist, 0, len(ids))
	for _, id := range ids {
		out = append(out, f.Playlists[id])
	}
	return out
}

// SetHTTPStatus scripts a transport-level status for one endpoint.
func (f *fakeServer) SetHTTPStatus(endpoint string, status int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.HTTPStatus[endpoint] = status
}

// ClearHTTPStatus removes a scripted status so the endpoint behaves normally.
func (f *fakeServer) ClearHTTPStatus(endpoint string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.HTTPStatus, endpoint)
}

// AddSong registers a track in the fake library.
func (f *fakeServer) AddSong(song rawSong) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Songs = append(f.Songs, song)
}

// AddPlaylist registers a playlist with ordered membership.
func (f *fakeServer) AddPlaylist(id, name, owner string, songIDs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Playlists[id] = &fakePlaylist{
		rawPlaylist: rawPlaylist{ID: id, Name: name, Owner: owner, SongCount: len(songIDs)},
		SongIDs:     songIDs,
	}
}

// Salts returns every salt the client used, so replay protection is testable.
func (f *fakeServer) Salts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, raw := range f.RawQueries {
		values, err := url.ParseQuery(raw)
		if err != nil {
			continue
		}
		out = append(out, values.Get("s"))
	}
	return out
}

type envelopeBody struct {
	Status        string           `json:"status"`
	Version       string           `json:"version"`
	Type          string           `json:"type,omitempty"`
	ServerVersion string           `json:"serverVersion,omitempty"`
	OpenSubsonic  bool             `json:"openSubsonic,omitempty"`
	Error         *subsonicError   `json:"error,omitempty"`
	SearchResult3 *searchResult3   `json:"searchResult3,omitempty"`
	Playlists     *playlistsResult `json:"playlists,omitempty"`
	Playlist      *playlistDetail  `json:"playlist,omitempty"`
	Starred2      *starredResult   `json:"starred2,omitempty"`
	ScanStatus    *scanStatus      `json:"scanStatus,omitempty"`
}

func (f *fakeServer) write(w http.ResponseWriter, fill func(*envelopeBody)) {
	body := envelopeBody{Status: "ok", Version: SubsonicAPIVersion}
	if fill != nil {
		fill(&body)
	}
	f.writeBody(w, body)
}

func (f *fakeServer) writeError(w http.ResponseWriter, apiErr subsonicError) {
	f.writeBody(w, envelopeBody{Status: "failed", Version: SubsonicAPIVersion, Error: &apiErr})
}

func (f *fakeServer) writeBody(w http.ResponseWriter, body envelopeBody) {
	payload, err := json.Marshal(map[string]envelopeBody{"subsonic-response": body})
	if err != nil {
		f.t.Fatalf("encode fake response: %v", err)
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(payload); err != nil {
		f.t.Fatalf("write fake response: %v", err)
	}
}

func song(id, title, path string, genres ...string) rawSong {
	raw := rawSong{
		ID:      id,
		Title:   title,
		Artist:  "Artist " + id,
		Album:   "Album " + id,
		Path:    path,
		Created: "2026-01-0" + fmt.Sprint(len(id)%9+1) + "T00:00:00Z",
	}
	if len(genres) > 0 {
		raw.Genre = genres[0]
		for _, name := range genres {
			raw.Genres = append(raw.Genres, struct {
				Name string `json:"name"`
			}{Name: name})
		}
	}
	return raw
}

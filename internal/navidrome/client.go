package navidrome

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	// SubsonicAPIVersion is the protocol version UDL negotiates.
	SubsonicAPIVersion = "1.16.1"
	// ClientName identifies UDL in Navidrome's activity log.
	ClientName = "udl"

	// maxResponseBytes bounds any single API response so a misbehaving or
	// wrong server cannot exhaust memory.
	maxResponseBytes = 64 << 20

	// pageSize is the Subsonic search3 page size used to walk the library.
	pageSize = 500
)

// ErrUnauthorized means the server rejected the credentials.
var ErrUnauthorized = errors.New("navidrome rejected the credentials")

// ErrIncompatibleServer means the server is too old or is not Navidrome.
var ErrIncompatibleServer = errors.New("navidrome server is incompatible")

// Credentials are the shared account's username and password. The password is
// used only to derive a per-request salted token and is never sent, logged, or
// serialized in plaintext.
type Credentials struct {
	Username string
	Password string
}

// Client talks to a Navidrome server over the Subsonic API.
type Client struct {
	BaseURL     string
	Credentials Credentials
	HTTP        *http.Client
	// Salt overrides random salt generation in tests only.
	Salt func() (string, error)
}

// NewClient builds a client with sane timeouts.
func NewClient(baseURL string, creds Credentials) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		Credentials: creds,
		HTTP:        &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

func (c *Client) salt() (string, error) {
	if c.Salt != nil {
		return c.Salt()
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate authentication salt: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// AuthParams builds the salted-token authentication parameters for one
// request. A fresh salt per request means a captured URL cannot be replayed
// against a different one, and the plaintext password never leaves this func.
func (c *Client) AuthParams() (url.Values, error) {
	username := strings.TrimSpace(c.Credentials.Username)
	if username == "" {
		return nil, fmt.Errorf("navidrome username is not configured")
	}
	if c.Credentials.Password == "" {
		return nil, fmt.Errorf("navidrome password is not available; save it in macOS Keychain first")
	}
	salt, err := c.salt()
	if err != nil {
		return nil, err
	}
	sum := md5.Sum([]byte(c.Credentials.Password + salt))
	values := url.Values{}
	values.Set("u", username)
	values.Set("t", hex.EncodeToString(sum[:]))
	values.Set("s", salt)
	values.Set("v", SubsonicAPIVersion)
	values.Set("c", ClientName)
	values.Set("f", "json")
	return values, nil
}

// RedactURL removes every authentication parameter so a URL is safe to log or
// put in an error message.
func RedactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "<unparseable url>"
	}
	query := parsed.Query()
	for _, key := range []string{"t", "s", "p", "u", "token", "salt", "password"} {
		if query.Has(key) {
			query.Set(key, "REDACTED")
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

type subsonicEnvelope struct {
	Response struct {
		Status        string           `json:"status"`
		Version       string           `json:"version"`
		Type          string           `json:"type"`
		ServerVersion string           `json:"serverVersion"`
		OpenSubsonic  bool             `json:"openSubsonic"`
		Error         *subsonicError   `json:"error"`
		SearchResult3 *searchResult3   `json:"searchResult3"`
		Playlists     *playlistsResult `json:"playlists"`
		Playlist      *playlistDetail  `json:"playlist"`
		Starred2      *starredResult   `json:"starred2"`
		ScanStatus    *scanStatus      `json:"scanStatus"`
	} `json:"subsonic-response"`
}

type subsonicError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type searchResult3 struct {
	Song []rawSong `json:"song"`
}

type playlistsResult struct {
	Playlist []rawPlaylist `json:"playlist"`
}

type playlistDetail struct {
	rawPlaylist
	Entry []rawSong `json:"entry"`
}

type starredResult struct {
	Song []rawSong `json:"song"`
}

type scanStatus struct {
	Scanning    bool  `json:"scanning"`
	Count       int64 `json:"count"`
	FolderCount int64 `json:"folderCount"`
}

type rawPlaylist struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SongCount int    `json:"songCount"`
	Owner     string `json:"owner"`
	Public    bool   `json:"public"`
	Comment   string `json:"comment"`
	Changed   string `json:"changed"`
}

type rawSong struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Genre  string `json:"genre"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Path     string `json:"path"`
	Duration int    `json:"duration"`
	Created  string `json:"created"`
	Starred  string `json:"starred"`
	Suffix   string `json:"suffix"`
	Size     int64  `json:"size"`
	Track    int    `json:"track"`
	Year     int    `json:"year"`
	BitRate  int    `json:"bitRate"`
}

// Song is a Navidrome library track in UDL's shape.
type Song struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Artist   string    `json:"artist,omitempty"`
	Album    string    `json:"album,omitempty"`
	Genres   []string  `json:"genres,omitempty"`
	Path     string    `json:"path,omitempty"`
	Duration string    `json:"duration,omitempty"`
	Created  time.Time `json:"created,omitempty"`
	Starred  bool      `json:"starred,omitempty"`
}

// Playlist is a Navidrome playlist summary.
type Playlist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Owner      string `json:"owner,omitempty"`
	TrackCount int    `json:"track_count"`
	Smart      bool   `json:"smart"`
}

// ServerInfo is the result of a successful ping.
type ServerInfo struct {
	Type          string `json:"type"`
	Version       string `json:"version"`
	ServerVersion string `json:"server_version"`
	OpenSubsonic  bool   `json:"open_subsonic"`
}

// maxAttempts covers one transient network hiccup or 5xx without turning a
// genuinely down server into a long stall. Every endpoint UDL calls is
// idempotent, including star/unstar, so a retry can never double-apply.
const maxAttempts = 3

func (c *Client) get(ctx context.Context, endpoint string, params url.Values) (*subsonicEnvelope, error) {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
			}
		}
		envelope, err := c.attempt(ctx, endpoint, params)
		if err == nil {
			return envelope, nil
		}
		lastErr = err
		if !retryable(ctx, err) {
			return nil, err
		}
	}
	return nil, lastErr
}

// retryable is true only for transport failures and 5xx. An unauthorized or
// incompatible server, or a canceled context, is final.
func retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if errors.Is(err, ErrUnauthorized) || errors.Is(err, ErrIncompatibleServer) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return errors.Is(err, errTransient)
}

var errTransient = errors.New("transient navidrome failure")

func (c *Client) attempt(ctx context.Context, endpoint string, params url.Values) (*subsonicEnvelope, error) {
	base := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("navidrome server URL is not configured")
	}
	auth, err := c.AuthParams()
	if err != nil {
		return nil, err
	}
	for key, values := range params {
		for _, value := range values {
			auth.Add(key, value)
		}
	}
	full := fmt.Sprintf("%s/rest/%s?%s", base, endpoint, auth.Encode())
	// Everything below reports `safe`, never `full`: the real URL carries the
	// auth token and salt.
	safe := fmt.Sprintf("%s/rest/%s", base, endpoint)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", safe, err)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("request %s failed: %w: %w", safe, errTransient, redactError(err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w (%s returned %d)", ErrUnauthorized, safe, resp.StatusCode)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%s returned HTTP %d: %w", safe, resp.StatusCode, errTransient)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned HTTP %d", safe, resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("read response from %s: %w", safe, redactError(err))
	}
	var envelope subsonicEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("%s returned a response UDL could not parse; is this a Navidrome server?", safe)
	}
	if envelope.Response.Status == "failed" && envelope.Response.Error != nil {
		return nil, translateSubsonicError(safe, *envelope.Response.Error)
	}
	if envelope.Response.Status == "" {
		return nil, fmt.Errorf("%s did not return a Subsonic response; is this a Navidrome server?", safe)
	}
	return &envelope, nil
}

func translateSubsonicError(endpoint string, apiErr subsonicError) error {
	switch apiErr.Code {
	case 40:
		return fmt.Errorf("%w: check the username and the password saved in Keychain", ErrUnauthorized)
	case 41, 42, 43, 44:
		return fmt.Errorf("%w: %s", ErrUnauthorized, apiErr.Message)
	case 30:
		return fmt.Errorf("%w: the server requires a newer Subsonic protocol than %s", ErrIncompatibleServer, SubsonicAPIVersion)
	case 20:
		return fmt.Errorf("%w: the server is older than Navidrome %s", ErrIncompatibleServer, MinimumServerVersion)
	case 50:
		return fmt.Errorf("the Navidrome account is not authorized for this operation")
	case 70:
		return fmt.Errorf("the requested item was not found on the server")
	default:
		return fmt.Errorf("%s failed: %s (code %d)", endpoint, apiErr.Message, apiErr.Code)
	}
}

// redactError strips any URL that survived into a transport error message.
func redactError(err error) error {
	if err == nil {
		return nil
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s %s: %w", urlErr.Op, RedactURL(urlErr.URL), urlErr.Err)
	}
	return err
}

// Ping verifies connectivity, credentials, and server compatibility.
func (c *Client) Ping(ctx context.Context) (ServerInfo, error) {
	envelope, err := c.get(ctx, "ping.view", nil)
	if err != nil {
		return ServerInfo{}, err
	}
	info := ServerInfo{
		Type:          envelope.Response.Type,
		Version:       envelope.Response.Version,
		ServerVersion: envelope.Response.ServerVersion,
		OpenSubsonic:  envelope.Response.OpenSubsonic,
	}
	if info.Type != "" && !strings.EqualFold(info.Type, "navidrome") {
		return info, fmt.Errorf("%w: server reports itself as %q, not Navidrome", ErrIncompatibleServer, info.Type)
	}
	if version, ok := ParseVersion(info.ServerVersion); ok && CompareVersions(version, MinimumServerVersion) < 0 {
		return info, fmt.Errorf("%w: server is %s but UDL requires %s or newer",
			ErrIncompatibleServer, version, MinimumServerVersion)
	}
	return info, nil
}

// Songs enumerates the whole library, page by page.
func (c *Client) Songs(ctx context.Context) ([]Song, error) {
	out := []Song{}
	seen := map[string]struct{}{}
	for offset := 0; ; offset += pageSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		params := url.Values{}
		params.Set("query", "")
		params.Set("songCount", strconv.Itoa(pageSize))
		params.Set("songOffset", strconv.Itoa(offset))
		params.Set("artistCount", "0")
		params.Set("albumCount", "0")
		envelope, err := c.get(ctx, "search3.view", params)
		if err != nil {
			return nil, err
		}
		if envelope.Response.SearchResult3 == nil || len(envelope.Response.SearchResult3.Song) == 0 {
			break
		}
		for _, raw := range envelope.Response.SearchResult3.Song {
			// A duplicate ID across pages means the library changed mid-walk;
			// keeping the first occurrence keeps the result self-consistent.
			if _, exists := seen[raw.ID]; exists {
				continue
			}
			seen[raw.ID] = struct{}{}
			out = append(out, convertSong(raw))
		}
		if len(envelope.Response.SearchResult3.Song) < pageSize {
			break
		}
	}
	return out, nil
}

// Playlists lists every playlist visible to the account.
func (c *Client) Playlists(ctx context.Context) ([]Playlist, error) {
	envelope, err := c.get(ctx, "getPlaylists.view", nil)
	if err != nil {
		return nil, err
	}
	out := []Playlist{}
	if envelope.Response.Playlists == nil {
		return out, nil
	}
	for _, raw := range envelope.Response.Playlists.Playlist {
		out = append(out, convertPlaylist(raw))
	}
	return out, nil
}

// Playlist reads ordered playlist membership.
func (c *Client) Playlist(ctx context.Context, id string) (Playlist, []Song, error) {
	if strings.TrimSpace(id) == "" {
		return Playlist{}, nil, fmt.Errorf("playlist id must not be empty")
	}
	params := url.Values{}
	params.Set("id", id)
	envelope, err := c.get(ctx, "getPlaylist.view", params)
	if err != nil {
		return Playlist{}, nil, err
	}
	if envelope.Response.Playlist == nil {
		return Playlist{}, nil, fmt.Errorf("playlist %q returned no detail", id)
	}
	detail := envelope.Response.Playlist
	songs := make([]Song, 0, len(detail.Entry))
	for _, raw := range detail.Entry {
		songs = append(songs, convertSong(raw))
	}
	return convertPlaylist(detail.rawPlaylist), songs, nil
}

// PlaylistByName finds a playlist by exact name, then case-insensitively.
func (c *Client) PlaylistByName(ctx context.Context, name string) (Playlist, bool, error) {
	items, err := c.Playlists(ctx)
	if err != nil {
		return Playlist{}, false, err
	}
	target := strings.TrimSpace(name)
	for _, item := range items {
		if item.Name == target {
			return item, true, nil
		}
	}
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item.Name), target) {
			return item, true, nil
		}
	}
	return Playlist{}, false, nil
}

// Starred returns every starred song.
func (c *Client) Starred(ctx context.Context) ([]Song, error) {
	envelope, err := c.get(ctx, "getStarred2.view", nil)
	if err != nil {
		return nil, err
	}
	out := []Song{}
	if envelope.Response.Starred2 == nil {
		return out, nil
	}
	for _, raw := range envelope.Response.Starred2.Song {
		song := convertSong(raw)
		song.Starred = true
		out = append(out, song)
	}
	return out, nil
}

// SortStarredSongs orders starred songs deterministically, in place.
//
// getStarred2 defines no ordering. Every consumer — the snapshot builder, the
// CLI listing, the agent method — must agree on one order, or a snapshot
// checksum churns on refreshes that changed nothing and Rekordbox sees adds and
// removes that never happened. The ID breaks ties so path-less songs still sort
// stably.
func SortStarredSongs(songs []Song) {
	sort.SliceStable(songs, func(i, j int) bool {
		left, right := starredSortKey(songs[i].Path), starredSortKey(songs[j].Path)
		if left != right {
			return left < right
		}
		return songs[i].ID < songs[j].ID
	})
}

func starredSortKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// Star stars one song. It is idempotent on the server.
func (c *Client) Star(ctx context.Context, songID string) error {
	return c.setStar(ctx, "star.view", songID)
}

// Unstar removes a star. It is idempotent on the server.
func (c *Client) Unstar(ctx context.Context, songID string) error {
	return c.setStar(ctx, "unstar.view", songID)
}

func (c *Client) setStar(ctx context.Context, endpoint, songID string) error {
	if strings.TrimSpace(songID) == "" {
		return fmt.Errorf("song id must not be empty")
	}
	params := url.Values{}
	params.Set("id", songID)
	_, err := c.get(ctx, endpoint, params)
	return err
}

// StartScan asks Navidrome to rescan the library.
func (c *Client) StartScan(ctx context.Context, full bool) error {
	params := url.Values{}
	if full {
		params.Set("fullScan", "true")
	}
	_, err := c.get(ctx, "startScan.view", params)
	return err
}

// ScanStatus reports whether a scan is running and how many files are indexed.
func (c *Client) ScanStatus(ctx context.Context) (bool, int64, error) {
	envelope, err := c.get(ctx, "getScanStatus.view", nil)
	if err != nil {
		return false, 0, err
	}
	if envelope.Response.ScanStatus == nil {
		return false, 0, nil
	}
	return envelope.Response.ScanStatus.Scanning, envelope.Response.ScanStatus.Count, nil
}

func convertPlaylist(raw rawPlaylist) Playlist {
	return Playlist{
		ID:         raw.ID,
		Name:       strings.TrimSpace(raw.Name),
		Owner:      strings.TrimSpace(raw.Owner),
		TrackCount: raw.SongCount,
		// Navidrome marks smart playlists by the .nsp comment; the Subsonic
		// shape has no dedicated flag, so treat an unmodifiable-by-us managed
		// name as informational only.
		Smart: false,
	}
}

func convertSong(raw rawSong) Song {
	song := Song{
		ID:      raw.ID,
		Title:   strings.TrimSpace(raw.Title),
		Artist:  strings.TrimSpace(raw.Artist),
		Album:   strings.TrimSpace(raw.Album),
		Path:    NormalizePath(raw.Path),
		Starred: strings.TrimSpace(raw.Starred) != "",
	}
	if raw.Duration > 0 {
		song.Duration = formatDuration(raw.Duration)
	}
	if created, err := time.Parse(time.RFC3339, strings.TrimSpace(raw.Created)); err == nil {
		song.Created = created.UTC()
	}
	genres := []string{}
	if trimmed := strings.TrimSpace(raw.Genre); trimmed != "" {
		genres = append(genres, trimmed)
	}
	for _, item := range raw.Genres {
		if trimmed := strings.TrimSpace(item.Name); trimmed != "" {
			genres = append(genres, trimmed)
		}
	}
	song.Genres = NormalizeGenres(genres)
	return song
}

func formatDuration(seconds int) string {
	minutes := seconds / 60
	rest := seconds % 60
	return fmt.Sprintf("%d:%02d", minutes, rest)
}

// NormalizePath canonicalizes a path the same way rekordbox/playlistsync does:
// decode a file: URL, clean, then NFC-fold. Apple Music hands back decomposed
// (NFD) filenames on macOS while Navidrome reports what it read from the
// filesystem, so both sides must fold identically or path matching silently
// misses every accented title.
func NormalizePath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "file:") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" {
			trimmed = parsed.Path
		}
	}
	cleaned := filepath.Clean(trimmed)
	if utf8.ValidString(cleaned) {
		cleaned = norm.NFC.String(cleaned)
	}
	return cleaned
}

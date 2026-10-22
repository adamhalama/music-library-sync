package navidrome

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// In remote mode stars, plays and playlists live only in the server's
// database. `udl navidrome export-db` pulls a verified copy of it to this Mac
// (weekly, from launchd) through the gate's `udl-mirror export-db` verb, which
// answers with one JSON header line followed by exactly size_bytes raw bytes
// of a fresh, quick_check-verified backup.

const (
	// ServerDBExportDirName is the default folder, inside the local data
	// directory, that holds the pulled copies.
	ServerDBExportDirName = "server-db-exports"

	// DefaultServerDBExportKeep keeps two months of weekly copies.
	DefaultServerDBExportKeep = 8

	// maxServerDBExportBytes refuses a reply that cannot be a phone
	// library's database; the whole reply is held in memory.
	maxServerDBExportBytes = 512 << 20

	// maxServerDBExportHeaderBytes bounds the JSON line before the bytes.
	maxServerDBExportHeaderBytes = 64 << 10

	// serverDBExportStaleAfter is when status starts warning: two missed
	// weekly runs.
	serverDBExportStaleAfter = 14 * 24 * time.Hour

	// serverDBExportTimeout ends a transfer that stalls, e.g. when the Mac
	// sleeps mid-download.
	serverDBExportTimeout = 30 * time.Minute

	serverDBExportPrefix = "navidrome-server-"
	serverDBExportStamp  = "20060102T150405Z"
)

// sqliteMagic starts every SQLite 3 database file.
var sqliteMagic = []byte("SQLite format 3\x00")

// serverDBExportName matches only files this command writes; rotation never
// touches anything else in the folder.
var serverDBExportName = regexp.MustCompile(`^navidrome-server-\d{8}T\d{6}Z\.db$`)

// ServerDBExportStateFileName records the export's outcome on this Mac. It is
// separate from the mirror state so neither writer can drop the other's
// fields, and the export never waits for a push's lock.
const ServerDBExportStateFileName = "udl-export-state.json"

// ErrServerDBExportBusy means another export is using the same data directory
// (the weekly launchd job and a manual run); this one did nothing.
var ErrServerDBExportBusy = errors.New("another `udl navidrome export-db` is running; this one was skipped")

// errServerDBExportTooLarge stops reading a reply past the cap.
var errServerDBExportTooLarge = fmt.Errorf("the server's database export is larger than %d MiB; refusing it", maxServerDBExportBytes>>20)

// ServerDBExportOptions selects where copies go and how many are kept.
type ServerDBExportOptions struct {
	// OutDir defaults to <data_dir>/server-db-exports.
	OutDir string
	// Keep is the number of newest copies retained; zero uses the default.
	Keep int
}

// ServerDBExportResult reports one pulled copy.
type ServerDBExportResult struct {
	Path            string    `json:"path"`
	SizeBytes       int64     `json:"size_bytes"`
	SHA256          string    `json:"sha256"`
	ExportedAt      time.Time `json:"exported_at"`
	ServerPath      string    `json:"server_path"`
	ServerCreatedAt string    `json:"server_created_at"`
	Kept            int       `json:"kept"`
	Removed         []string  `json:"removed"`
	Warnings        []string  `json:"warnings"`
}

// serverDBExportHeader is the gate's first line.
type serverDBExportHeader struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error"`
	Format    string `json:"format"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	CreatedAt string `json:"created_at"`
	Path      string `json:"path"`
}

// ServerDBExportDir is the default folder for pulled copies.
func ServerDBExportDir(resolved Resolved) string {
	return filepath.Join(resolved.DataDir, ServerDBExportDirName)
}

// ServerDBExportState is what this Mac knows about its off-server copies.
type ServerDBExportState struct {
	LastExportAt      *time.Time `json:"last_export_at,omitempty"`
	LastExportError   string     `json:"last_export_error,omitempty"`
	LastExportErrorAt *time.Time `json:"last_export_error_at,omitempty"`
}

// ServerDBExportStatePath is the export's state file in the data directory.
func ServerDBExportStatePath(resolved Resolved) string {
	return filepath.Join(resolved.DataDir, ServerDBExportStateFileName)
}

// LoadServerDBExportState reads the state. If the separate state file does
// not exist, it migrates the previous build's last_export_* fields from the
// mirror state under the export lock. No history in either file means no
// export has run yet.
func LoadServerDBExportState(resolved Resolved) (ServerDBExportState, error) {
	state, err := readServerDBExportState(ServerDBExportStatePath(resolved))
	if !errors.Is(err, os.ErrNotExist) {
		return state, err
	}
	legacy, err := readLegacyServerDBExportState(resolved)
	if err != nil || legacy == (ServerDBExportState{}) {
		return legacy, err
	}
	unlock, err := lockServerDBExport(resolved)
	if err != nil {
		return ServerDBExportState{}, fmt.Errorf("migrate legacy export history: %w", err)
	}
	defer unlock()
	// Re-read both files under the lock: another exporter or migrator may
	// have written the new state since our first read. Never replay legacy
	// history over a recorded outcome, even an explicitly empty state.
	return loadServerDBExportStateLocked(resolved)
}

func readServerDBExportState(path string) (ServerDBExportState, error) {
	var state ServerDBExportState
	payload, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		return ServerDBExportState{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return state, nil
}

func readLegacyServerDBExportState(resolved Resolved) (ServerDBExportState, error) {
	state, err := readServerDBExportState(MirrorStatePath(resolved))
	if errors.Is(err, os.ErrNotExist) {
		return ServerDBExportState{}, nil
	}
	return state, err
}

// loadServerDBExportStateLocked is also used when recording an export whose
// caller already owns the lock; acquiring it again would refuse our own run.
func loadServerDBExportStateLocked(resolved Resolved) (ServerDBExportState, error) {
	state, err := readServerDBExportState(ServerDBExportStatePath(resolved))
	if !errors.Is(err, os.ErrNotExist) {
		return state, err
	}
	state, err = readLegacyServerDBExportState(resolved)
	if err != nil || state == (ServerDBExportState{}) {
		return state, err
	}
	if err := saveServerDBExportState(resolved, state); err != nil {
		return ServerDBExportState{}, fmt.Errorf("migrate legacy export history: %w", err)
	}
	return state, nil
}

// preserveLegacyServerDBExportState runs before a mirror load/write can
// discard fields its typed state no longer owns. If migration is busy or
// fails, the mirror writer must leave the original bytes intact. Once those
// fields are gone, export state errors cannot block independent mirror work.
func preserveLegacyServerDBExportState(resolved Resolved) error {
	legacy, err := readLegacyServerDBExportState(resolved)
	if err != nil || legacy == (ServerDBExportState{}) {
		return err
	}
	_, err = LoadServerDBExportState(resolved)
	return err
}

func saveServerDBExportState(resolved Resolved, state ServerDBExportState) error {
	if err := os.MkdirAll(resolved.DataDir, 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(ServerDBExportStatePath(resolved), append(payload, '\n'), 0o600)
}

// ExportServerDB pulls a verified copy of the server's Navidrome database,
// writes it atomically (0600) and keeps the newest opts.Keep copies. The
// outcome is recorded in the export's own state file, which status reports.
//
// One export runs at a time per data directory (ErrServerDBExportBusy otherwise); it
// never takes the mirror lock, so a running push neither blocks it nor
// loses its own state writes.
func (m *Manager) ExportServerDB(ctx context.Context, opts ServerDBExportOptions) (ServerDBExportResult, error) {
	result := ServerDBExportResult{Removed: []string{}, Warnings: []string{}}
	remote, err := m.requireRemote()
	if err != nil {
		return result, err
	}
	if opts.Keep < 0 {
		return result, errors.New("keep must be at least 1")
	}
	if opts.Keep == 0 {
		opts.Keep = DefaultServerDBExportKeep
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		opts.OutDir = ServerDBExportDir(m.Resolved)
	}
	unlock, err := lockServerDBExport(m.Resolved)
	if err != nil {
		// The running export owns the state file; record nothing.
		return result, err
	}
	defer unlock()
	result, err = m.exportServerDB(ctx, remote, opts, result)
	at := m.now()
	if err == nil {
		at = result.ExportedAt
	}
	if recordErr := m.recordServerDBExport(at, err); recordErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not record the export: %v", recordErr))
	}
	return result, err
}

func (m *Manager) exportServerDB(ctx context.Context, remote *RemoteConfig, opts ServerDBExportOptions, result ServerDBExportResult) (ServerDBExportResult, error) {
	ctx, cancel := context.WithTimeout(ctx, serverDBExportTimeout)
	defer cancel()
	run := m.Remote
	if run == nil {
		// Header line, newline, database.
		run = cappedRemoteRunner(maxServerDBExportHeaderBytes + 1 + maxServerDBExportBytes)
	}
	raw, err := run(ctx, nil, remote.SSHPath, "-o", "BatchMode=yes", remote.SSHTarget, "udl-mirror export-db")
	if errors.Is(err, errServerDBExportTooLarge) {
		return result, err
	}
	if err != nil {
		return result, fmt.Errorf("udl-mirror export-db: %w", err)
	}
	header, payload, err := parseServerDBExport(raw)
	if err != nil {
		if raw.ExitCode == 255 && len(raw.Stdout) == 0 {
			return result, fmt.Errorf("the mirror server is not reachable over SSH (%s): %s",
				remote.SSHTarget, lastLines(strings.TrimSpace(string(raw.Stderr)), 3))
		}
		return result, err
	}
	stamp := m.now()
	path, err := writeServerDBExport(opts.OutDir, stamp, payload)
	if err != nil {
		return result, err
	}
	result.Path, result.SizeBytes, result.SHA256 = path, header.SizeBytes, strings.ToLower(header.SHA256)
	result.ExportedAt, result.ServerPath, result.ServerCreatedAt = stamp, header.Path, header.CreatedAt
	kept, removed, rotateErr := rotateServerDBExports(opts.OutDir, opts.Keep)
	result.Kept, result.Removed = kept, removed
	if rotateErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not prune older copies: %v", rotateErr))
	}
	return result, nil
}

// parseServerDBExport checks the gate's header and the bytes after it: the
// declared size, the sha256 and the SQLite file magic. The server already
// ran PRAGMA quick_check on the file it streamed.
func parseServerDBExport(raw RemoteOutput) (serverDBExportHeader, []byte, error) {
	var header serverDBExportHeader
	stdout := raw.Stdout
	newline := bytes.IndexByte(stdout, '\n')
	if newline < 0 || newline > maxServerDBExportHeaderBytes {
		limit := len(stdout)
		if limit > maxServerDBExportHeaderBytes {
			limit = maxServerDBExportHeaderBytes
		}
		newline = limit
	}
	line := bytes.TrimSpace(stdout[:newline])
	if err := json.Unmarshal(line, &header); err != nil {
		return header, nil, fmt.Errorf("udl-mirror export-db returned no usable reply (exit %d): %s",
			raw.ExitCode, lastLines(strings.TrimSpace(string(raw.Stderr)), 3))
	}
	if !header.OK {
		if header.Error == "" {
			header.Error = "the server refused the export without a reason"
		}
		return header, nil, fmt.Errorf("udl-mirror export-db: %s", header.Error)
	}
	if header.Format != "sqlite" {
		return header, nil, fmt.Errorf("udl-mirror export-db sent format %q, not sqlite", header.Format)
	}
	if header.SizeBytes <= 0 {
		return header, nil, errors.New("udl-mirror export-db declared an empty database")
	}
	if header.SizeBytes > maxServerDBExportBytes {
		return header, nil, errServerDBExportTooLarge
	}
	payload := []byte{}
	if newline < len(stdout) {
		payload = stdout[newline+1:]
	}
	if int64(len(payload)) != header.SizeBytes {
		detail := ""
		if stderr := strings.TrimSpace(string(raw.Stderr)); stderr != "" {
			detail = ": " + lastLines(stderr, 3)
		}
		return header, nil, fmt.Errorf("udl-mirror export-db sent %d bytes but declared %d (exit %d)%s",
			len(payload), header.SizeBytes, raw.ExitCode, detail)
	}
	if raw.ExitCode != 0 {
		return header, nil, fmt.Errorf("udl-mirror export-db exited %d after sending the database", raw.ExitCode)
	}
	sum := sha256.Sum256(payload)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, header.SHA256) {
		return header, nil, fmt.Errorf("udl-mirror export-db sha256 mismatch: received %s, server declared %s", got, header.SHA256)
	}
	if !bytes.HasPrefix(payload, sqliteMagic) {
		return header, nil, errors.New("udl-mirror export-db sent a file that is not an SQLite database")
	}
	return header, payload, nil
}

// writeServerDBExport stores the copy through a synced temp file that is
// hard-linked to its final name, so the folder only ever holds complete,
// verified copies and an existing copy is never replaced: link(2) fails with
// EEXIST where a rename would overwrite.
func writeServerDBExport(dir string, at time.Time, payload []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	final := filepath.Join(dir, serverDBExportPrefix+at.UTC().Format(serverDBExportStamp)+".db")
	exists := fmt.Errorf("%s already exists; not replacing it", final)
	// Checked first only to fail before writing; the link below decides.
	if _, err := os.Lstat(final); err == nil {
		return "", exists
	}
	tmp, err := os.CreateTemp(dir, ".navidrome-server-*.db.partial")
	if err != nil {
		return "", fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	// After a link the temp name is a second name for the copy; after a
	// failure it is the only one. Either way it goes.
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("sync %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := installNoClobber(tmpPath, final, os.Link); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", exists
		}
		return "", fmt.Errorf("install %s: %w", final, err)
	}
	if handle, err := os.Open(dir); err == nil {
		_ = handle.Sync()
		_ = handle.Close()
	}
	return final, nil
}

// installNoClobber gives tmp's file the name final unless final exists; the
// caller removes tmp. A failed hard link must never fall back to a checked
// rename: another configuration or process can publish after the check, and
// rename would overwrite it. Filesystems without hard links are refused.
// link is os.Link in production, supplied here for filesystem-fault tests.
func installNoClobber(tmp, final string, link func(string, string) error) error {
	err := link(tmp, final)
	if err == nil || errors.Is(err, fs.ErrExist) {
		return err
	}
	return fmt.Errorf("the destination filesystem at %s must support hard links for no-replace publication; export refused: %w", filepath.Dir(final), err)
}

// rotateServerDBExports keeps the newest `keep` copies. Only regular files
// named like this command's output are considered; the stamp in the name
// orders them.
func rotateServerDBExports(dir string, keep int) (int, []string, error) {
	removed := []string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, removed, err
	}
	names := []string{}
	for _, entry := range entries {
		if entry.Type().IsRegular() && serverDBExportName.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	if len(names) <= keep {
		return len(names), removed, nil
	}
	var errs []error
	for _, name := range names[keep:] {
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			errs = append(errs, err)
			continue
		}
		removed = append(removed, path)
	}
	return len(names) - len(removed), removed, errors.Join(errs...)
}

// recordServerDBExport stores the outcome; the caller holds the export lock.
// A failure keeps the time of the last good copy.
func (m *Manager) recordServerDBExport(at time.Time, exportErr error) error {
	state, err := loadServerDBExportStateLocked(m.Resolved)
	if err != nil {
		// Do not replace a state file this build cannot read.
		return err
	}
	if exportErr == nil {
		state.LastExportAt, state.LastExportError, state.LastExportErrorAt = &at, "", nil
	} else {
		state.LastExportError, state.LastExportErrorAt = exportErr.Error(), &at
	}
	return saveServerDBExportState(m.Resolved, state)
}

func (m *Manager) serverDBExportProblems() []string {
	state, err := LoadServerDBExportState(m.Resolved)
	if err != nil {
		return []string{err.Error()}
	}
	return exportProblems(state, m.now())
}

// exportProblems flags a failed or stale off-server copy. A Mac that never
// exported is not flagged: the copy is opt-in until the launchd job exists.
func exportProblems(state ServerDBExportState, now time.Time) []string {
	problems := []string{}
	last := "never"
	if state.LastExportAt != nil {
		last = state.LastExportAt.Local().Format("2006-01-02 15:04")
	}
	switch {
	case state.LastExportError != "":
		when := "at an unknown time"
		if state.LastExportErrorAt != nil {
			when = state.LastExportErrorAt.Local().Format("2006-01-02 15:04")
		}
		problems = append(problems, fmt.Sprintf(
			"the last off-server copy of the server's database failed (%s): %s — last good copy: %s; run `udl navidrome export-db`",
			when, state.LastExportError, last))
	case state.LastExportAt != nil && now.Sub(*state.LastExportAt) > serverDBExportStaleAfter:
		problems = append(problems, fmt.Sprintf(
			"the newest off-server copy of the server's database is from %s (%d days ago); stars and plays since then exist only on the server — run `udl navidrome export-db` and check the weekly launchd job",
			last, int(now.Sub(*state.LastExportAt).Hours()/24)))
	}
	return problems
}

// cappedBuffer collects output up to a limit and then fails the write, which
// makes os/exec close its end of the pipe. onOverflow, when set, runs once at
// that point. Wait reports the child's exit rather than this error, hence the
// flag.
type cappedBuffer struct {
	buf        bytes.Buffer
	limit      int
	overflow   bool
	onOverflow func()
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.overflow || c.buf.Len()+len(p) > c.limit {
		if !c.overflow && c.onOverflow != nil {
			c.onOverflow()
		}
		c.overflow = true
		return 0, errServerDBExportTooLarge
	}
	return c.buf.Write(p)
}

// cappedRunnerWaitDelay bounds the wait for the pipes after the subprocess
// has exited or was killed.
const cappedRunnerWaitDelay = 500 * time.Millisecond

// cappedRemoteRunner is DefaultRemoteRunner with a bounded stdout. Passing
// the cap kills the subprocess at once: a closed pipe alone does not end a
// child that has gone quiet, which would then hold the buffer until the
// transfer timeout.
func cappedRemoteRunner(limit int) RemoteRunner {
	return func(parent context.Context, stdin []byte, name string, args ...string) (RemoteOutput, error) {
		ctx, cancel := context.WithCancel(parent)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		// ssh writes everything before it exits, so this only bounds how
		// long a killed child's leftovers (e.g. a ProxyCommand) may hold
		// the stderr pipe open.
		cmd.WaitDelay = cappedRunnerWaitDelay
		if stdin != nil {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		stdout := &cappedBuffer{limit: limit, onOverflow: cancel}
		stderr := &cappedBuffer{limit: 1 << 20}
		cmd.Stdout, cmd.Stderr = stdout, stderr
		err := cmd.Run()
		out := RemoteOutput{Stdout: stdout.buf.Bytes(), Stderr: stderr.buf.Bytes()}
		if stdout.overflow {
			return out, errServerDBExportTooLarge
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && ctx.Err() == nil {
			out.ExitCode = exitErr.ExitCode()
			return out, nil
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return out, ctxErr
			}
			return out, err
		}
		return out, nil
	}
}

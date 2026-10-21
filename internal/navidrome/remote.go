package navidrome

import (
	"bytes"
	"context"
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

// Remote mode moves serving to another machine while this Mac stays the only
// place the library is edited. The server holds a mirror of MusicDir that UDL
// pushes with rsync over SSH; the server's single key for this Mac runs a
// forced command (udl-mirror-gate) that hands rsync to `rrsync -wo` and
// otherwise answers only `udl-mirror status|backup|apply-dates`.
//
// The mirror is mounted inside the server's container at the same absolute
// path as MusicDir, so every path Navidrome reports is a real path on this
// Mac and the exact-path identity rule needs no translation.

const (
	// remoteRsyncTimeoutSeconds ends a push whose stream stalls, e.g. when the
	// Mac sleeps mid-transfer. The next push resumes from the kept partials.
	remoteRsyncTimeoutSeconds = 120

	// scanIdleTimeout bounds the wait for a running scan to finish before UDL
	// requests its own: Navidrome silently ignores startScan while scanning.
	scanIdleTimeout = 5 * time.Minute

	// scanFinishTimeout bounds the wait for the requested scan.
	scanFinishTimeout = 10 * time.Minute
)

// rsync exit codes the push treats specially.
const (
	rsyncExitPartial   = 23
	rsyncExitVanished  = 24
	rsyncExitMaxDelete = 25
)

// mirrorAudioExtensions are the files the Date Added manifest covers. Other
// files (covers, `.nsp` playlists) are mirrored but have no media_file row.
var mirrorAudioExtensions = map[string]struct{}{
	".aac": {}, ".aif": {}, ".aiff": {}, ".alac": {}, ".ape": {}, ".dff": {},
	".dsf": {}, ".flac": {}, ".m4a": {}, ".m4b": {}, ".mp3": {}, ".mp4": {},
	".mpc": {}, ".oga": {}, ".ogg": {}, ".opus": {}, ".wav": {}, ".wma": {},
	".wv": {},
}

var rsync3Version = regexp.MustCompile(`(?m)^rsync\s+version\s+v?3\.`)

// RemoteOutput is what a remote command produced.
type RemoteOutput struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// RemoteRunner runs a local client command (rsync or ssh) with optional
// stdin. A non-zero exit is reported through ExitCode, not the error; the
// error means the command could not run at all or the context ended.
type RemoteRunner func(ctx context.Context, stdin []byte, name string, args ...string) (RemoteOutput, error)

// DefaultRemoteRunner runs the command for real.
func DefaultRemoteRunner(ctx context.Context, stdin []byte, name string, args ...string) (RemoteOutput, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := RemoteOutput{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
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

// PushResult reports one mirror push.
type PushResult struct {
	Command    string    `json:"command"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	// Changed counts files created or updated on the server, Deleted files
	// removed. Directory-only changes are not counted.
	Changed int      `json:"changed"`
	Deleted int      `json:"deleted"`
	Items   []string `json:"items,omitempty"`
	Warning string   `json:"warning,omitempty"`
}

// HasChanges reports whether the server's library changed.
func (r PushResult) HasChanges() bool {
	return r.Changed > 0 || r.Deleted > 0
}

// maxReportedPushItems caps the itemized lines kept for display.
const maxReportedPushItems = 200

// RsyncPushArgs builds the push command line. Every flag is a decision:
//   - --delay-updates/--delete-delay: changed files land in a hidden
//     `.~tmp~` and are renamed into place together at the end, deletions after
//     that, so a scan never sees a half-written file and a rename never shows
//     as a gap. Navidrome skips hidden files and folders.
//   - --times: Navidrome and rsync both detect changes by size and mtime.
//   - no --checksum: it would read the whole library on both ends each time.
//   - no --iconv: names are mirrored byte-for-byte, which keeps Navidrome's
//     relative-path identity equal to this Mac's.
//   - `- .*`: Finder and udl temp files stay local, and hidden server-side
//     files (rsync's own partials) are protected from --delete.
func RsyncPushArgs(cfg Config, resolved Resolved) []string {
	remote := cfg.Remote
	if remote == nil {
		remote = &RemoteConfig{}
	}
	return []string{
		"--recursive", "--times", "--perms", "--chmod=D755,F644",
		"--delete-delay", "--delay-updates", "--partial", "--fuzzy",
		fmt.Sprintf("--max-delete=%d", remote.MaxDelete),
		fmt.Sprintf("--timeout=%d", remoteRsyncTimeoutSeconds),
		"--filter=- .*",
		"--filter=- *.part", "--filter=- *.partial", "--filter=- *.tmp", "--filter=- *.crdownload",
		"--itemize-changes", "--out-format=%i %n%L",
		"-e", remote.SSHPath + " -o BatchMode=yes",
		strings.TrimRight(resolved.MusicDir, "/") + "/",
		remote.SSHTarget + ":",
	}
}

// ParseItemizedChanges counts what an itemized push changed.
func ParseItemizedChanges(output string) (changed, deleted int, items []string) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 3 {
			continue
		}
		switch {
		case strings.HasPrefix(line, "*deleting"):
			// A folder emptied by the push is listed too; only files count.
			if strings.HasSuffix(line, "/") {
				continue
			}
			deleted++
		case line[1] == 'f' && strings.ContainsRune("<>ch.", rune(line[0])):
			changed++
		default:
			continue
		}
		if len(items) < maxReportedPushItems {
			items = append(items, line)
		}
	}
	return changed, deleted, items
}

func (m *Manager) remoteRunner() RemoteRunner {
	if m.Remote != nil {
		return m.Remote
	}
	return DefaultRemoteRunner
}

func (m *Manager) requireRemote() (*RemoteConfig, error) {
	if !m.Config.IsRemote() || m.Config.Remote == nil {
		return nil, errors.New("this operation needs navidrome.mode: remote")
	}
	return m.Config.Remote, nil
}

// Push mirrors MusicDir to the server. It never requests a scan; see
// SyncMirror for the full push, scan and Date Added sequence.
func (m *Manager) Push(ctx context.Context) (PushResult, error) {
	remote, err := m.requireRemote()
	if err != nil {
		return PushResult{}, err
	}
	if err := checkPushSource(m.Resolved.MusicDir); err != nil {
		return PushResult{}, err
	}
	run := m.remoteRunner()
	version, err := run(ctx, nil, remote.RsyncPath, "--version")
	if err != nil || version.ExitCode != 0 {
		return PushResult{}, fmt.Errorf("rsync at %s is not usable (install it with `brew install rsync`): %v", remote.RsyncPath, err)
	}
	if !rsync3Version.Match(version.Stdout) {
		return PushResult{}, fmt.Errorf("%s is not rsync 3.x; macOS's /usr/bin/rsync is openrsync, so point navidrome.remote.rsync_path at Homebrew's rsync", remote.RsyncPath)
	}

	args := RsyncPushArgs(m.Config, m.Resolved)
	result := PushResult{Command: remote.RsyncPath + " " + strings.Join(args, " "), StartedAt: m.now()}
	out, err := run(ctx, nil, remote.RsyncPath, args...)
	result.FinishedAt = m.now()
	result.Changed, result.Deleted, result.Items = ParseItemizedChanges(string(out.Stdout))
	if err != nil {
		return result, fmt.Errorf("mirror push did not finish: %w", err)
	}
	stderr := strings.TrimSpace(string(out.Stderr))
	switch out.ExitCode {
	case 0:
	case rsyncExitVanished:
		result.Warning = "some files changed or vanished during the push; the next push will catch up"
	case rsyncExitMaxDelete:
		return result, fmt.Errorf(
			"the push stopped after deleting %d files on the server (navidrome.remote.max_delete); the mirror is incomplete. If this many deletions are intended, raise max_delete and push again",
			remote.MaxDelete)
	default:
		if strings.Contains(stderr, "Another instance of rrsync") {
			return result, errors.New("another push to the mirror is still running; try again shortly")
		}
		return result, fmt.Errorf("mirror push failed (rsync exit %d): %s", out.ExitCode, lastLines(stderr, 5))
	}
	return result, nil
}

// checkPushSource refuses to mirror a missing or empty library: with
// --delete, that would empty the server.
func checkPushSource(musicDir string) error {
	info, err := os.Stat(musicDir)
	if err != nil {
		return fmt.Errorf("music_dir %s is not available: %w", musicDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("music_dir %s is not a directory", musicDir)
	}
	entries, err := os.ReadDir(musicDir)
	if err != nil {
		return fmt.Errorf("read music_dir %s: %w", musicDir, err)
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
	}
	return fmt.Errorf("music_dir %s is empty; refusing to push an empty library over the mirror", musicDir)
}

func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// gateReply is the envelope every udl-mirror verb answers with.
type gateReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// callGate runs one `udl-mirror <verb>` on the server and decodes its reply.
func (m *Manager) callGate(ctx context.Context, stdin []byte, out any, verb string, flags ...string) error {
	remote, err := m.requireRemote()
	if err != nil {
		return err
	}
	command := strings.Join(append([]string{"udl-mirror", verb}, flags...), " ")
	args := []string{"-o", "BatchMode=yes", remote.SSHTarget, command}
	raw, err := m.remoteRunner()(ctx, stdin, remote.SSHPath, args...)
	if err != nil {
		return fmt.Errorf("udl-mirror %s: %w", verb, err)
	}
	var envelope gateReply
	if decodeErr := json.Unmarshal(bytes.TrimSpace(raw.Stdout), &envelope); decodeErr != nil {
		detail := lastLines(strings.TrimSpace(string(raw.Stderr)), 3)
		if raw.ExitCode == 255 {
			return fmt.Errorf("the mirror server is not reachable over SSH (%s): %s", remote.SSHTarget, detail)
		}
		return fmt.Errorf("udl-mirror %s returned no usable reply (exit %d): %s", verb, raw.ExitCode, detail)
	}
	if !envelope.OK {
		return fmt.Errorf("udl-mirror %s: %s", verb, envelope.Error)
	}
	if out != nil {
		if err := json.Unmarshal(bytes.TrimSpace(raw.Stdout), out); err != nil {
			return fmt.Errorf("decode udl-mirror %s reply: %w", verb, err)
		}
	}
	return nil
}

// RemoteStatus is the server side of the mirror as the gate reports it.
type RemoteStatus struct {
	SSHTarget string `json:"ssh_target"`
	Library   struct {
		Path        string     `json:"path"`
		Files       int        `json:"files"`
		Bytes       int64      `json:"bytes"`
		NewestMtime *time.Time `json:"newest_mtime,omitempty"`
	} `json:"library"`
	Database struct {
		Path      string `json:"path"`
		Present   bool   `json:"present"`
		SizeBytes int64  `json:"size_bytes,omitempty"`
		Tracks    int    `json:"tracks,omitempty"`
		Libraries []struct {
			ID   int    `json:"id"`
			Path string `json:"path"`
		} `json:"libraries,omitempty"`
		Error string `json:"error,omitempty"`
	} `json:"database"`
	UDLBackups       []Backup `json:"udl_backups"`
	NavidromeBackups []Backup `json:"navidrome_backups"`
	Disk             struct {
		FreeBytes  int64 `json:"free_bytes"`
		TotalBytes int64 `json:"total_bytes"`
	} `json:"disk"`
}

// RemoteStatus asks the server about the mirror, its database and backups.
func (m *Manager) RemoteStatus(ctx context.Context) (RemoteStatus, error) {
	var status RemoteStatus
	if err := m.callGate(ctx, nil, &status, "status"); err != nil {
		return RemoteStatus{}, err
	}
	status.SSHTarget = m.Config.Remote.SSHTarget
	return status, nil
}

// remoteProblems flags a server layout that breaks exact-path identity.
func (m *Manager) remoteProblems(status RemoteStatus) []string {
	problems := []string{}
	if !status.Database.Present {
		return append(problems, "the server has no Navidrome database yet; start the container and finish its first scan")
	}
	rooted := false
	for _, library := range status.Database.Libraries {
		if strings.TrimRight(library.Path, "/") == strings.TrimRight(m.Resolved.MusicDir, "/") {
			rooted = true
		}
	}
	if !rooted {
		problems = append(problems, fmt.Sprintf(
			"the server's Navidrome library is not rooted at %s; mount the mirror at exactly that path inside the container so reported paths match this Mac",
			m.Resolved.MusicDir))
	}
	return problems
}

// RemoteBackup makes a verified database backup on the server.
func (m *Manager) RemoteBackup(ctx context.Context) (BackupResult, error) {
	var reply struct {
		Backup Backup `json:"backup"`
	}
	if err := m.callGate(ctx, nil, &reply, "backup"); err != nil {
		return BackupResult{}, err
	}
	if reply.Backup.Path == "" || reply.Backup.SizeBytes == 0 {
		return BackupResult{}, errors.New("the server reported a backup without a file; refusing to continue")
	}
	return BackupResult{
		Backup:  reply.Backup,
		Command: "udl-mirror backup",
		Message: fmt.Sprintf("created %s on %s", reply.Backup.Path, m.Config.Remote.SSHTarget),
	}, nil
}

// BirthTimeEntry is one file's APFS creation time, keyed by its path relative
// to MusicDir exactly as stored on disk.
type BirthTimeEntry struct {
	Path        string `json:"path"`
	BirthTimeNS int64  `json:"birth_time_ns"`
}

// BirthTimeManifest is what `udl-mirror apply-dates` reads on stdin.
type BirthTimeManifest struct {
	Version  int              `json:"version"`
	MusicDir string           `json:"music_dir"`
	Entries  []BirthTimeEntry `json:"entries"`
}

// BirthTimeFunc reads a file's creation time.
type BirthTimeFunc func(path string) (time.Time, error)

// BuildBirthTimeManifest walks MusicDir the way Navidrome does (hidden
// entries skipped) and records the creation time of every audio file. The
// managed playlists folder holds no audio and is skipped.
func BuildBirthTimeManifest(resolved Resolved, birth BirthTimeFunc) (BirthTimeManifest, error) {
	if birth == nil {
		birth = FileBirthTime
	}
	root := strings.TrimRight(resolved.MusicDir, "/")
	manifest := BirthTimeManifest{Version: 1, MusicDir: root, Entries: []BirthTimeEntry{}}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if resolved.PlaylistsDir != "" && path == resolved.PlaylistsDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if _, audio := mirrorAudioExtensions[strings.ToLower(filepath.Ext(entry.Name()))]; !audio {
			return nil
		}
		created, err := birth(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		manifest.Entries = append(manifest.Entries, BirthTimeEntry{
			Path:        filepath.ToSlash(rel),
			BirthTimeNS: created.UnixNano(),
		})
		return nil
	})
	if err != nil {
		return BirthTimeManifest{}, fmt.Errorf("read creation times under %s: %w", root, err)
	}
	sort.Slice(manifest.Entries, func(i, j int) bool { return manifest.Entries[i].Path < manifest.Entries[j].Path })
	return manifest, nil
}

// remoteDatesReply is the gate's apply-dates answer.
type remoteDatesReply struct {
	DryRun          bool     `json:"dry_run"`
	Entries         int      `json:"entries"`
	Matched         int      `json:"matched"`
	Unmatched       int      `json:"unmatched"`
	UnmatchedSample []string `json:"unmatched_sample"`
	Ambiguous       int      `json:"ambiguous"`
	Candidates      int      `json:"candidates"`
	Updated         int      `json:"updated"`
	AlbumsUpdated   int      `json:"albums_updated"`
	Backup          *Backup  `json:"backup"`
}

// reconcileDateAddedRemote is ReconcileDateAdded for a mirror: the server's
// own birth times are push times, so this Mac supplies the real ones and the
// server applies them (backing up first when anything changes).
func (m *Manager) reconcileDateAddedRemote(ctx context.Context, dryRun bool) (ReconcileResult, error) {
	manifest, err := BuildBirthTimeManifest(m.Resolved, m.BirthTime)
	if err != nil {
		return ReconcileResult{}, err
	}
	payload, err := json.Marshal(manifest)
	if err != nil {
		return ReconcileResult{}, err
	}
	flags := []string{}
	if dryRun {
		flags = append(flags, "--dry-run")
	}
	var reply remoteDatesReply
	if err := m.callGate(ctx, payload, &reply, "apply-dates", flags...); err != nil {
		return ReconcileResult{}, err
	}
	result := ReconcileResult{
		DatabasePath:  m.Config.Remote.SSHTarget + ":navidrome.db",
		Candidates:    reply.Candidates,
		Updated:       reply.Updated,
		AlbumsUpdated: reply.AlbumsUpdated,
		Unmatched:     reply.Unmatched,
	}
	if reply.Backup != nil {
		result.BackupPath = reply.Backup.Path
	}
	switch {
	case reply.Candidates == 0:
		result.Message = "Date Added already matches file creation time; nothing to do"
	case dryRun:
		result.Message = fmt.Sprintf("%d track(s) would get Date Added from file creation time", reply.Candidates)
	default:
		result.Message = fmt.Sprintf("Date Added now matches file creation time for %d track(s)", reply.Updated)
	}
	if reply.Unmatched > 0 {
		result.Message += fmt.Sprintf(" (%d file(s) are not in the server library yet)", reply.Unmatched)
	}
	return result, nil
}

// scanStatusClient is the slice of the API the scan wait needs.
type scanStatusClient interface {
	StartScan(ctx context.Context, full bool) error
	ScanState(ctx context.Context) (ScanState, error)
}

// scanPollInterval is a var so tests do not sleep.
var scanPollInterval = time.Second

// scanAfterPush waits for any running scan, requests one, and waits for it to
// finish. A scan that starts after the push completed includes the pushed
// files, so being idle first is what makes the request count.
func scanAfterPush(ctx context.Context, client scanStatusClient) error {
	idleCtx, cancelIdle := context.WithTimeout(ctx, scanIdleTimeout)
	defer cancelIdle()
	before, err := waitScanIdle(idleCtx, client)
	if err != nil {
		return fmt.Errorf("wait for the running Navidrome scan: %w", err)
	}
	if err := client.StartScan(ctx, false); err != nil {
		return fmt.Errorf("request a Navidrome scan: %w", err)
	}
	finishCtx, cancelFinish := context.WithTimeout(ctx, scanFinishTimeout)
	defer cancelFinish()
	sawScanning := false
	polls := 0
	for {
		state, err := client.ScanState(finishCtx)
		if err != nil {
			return fmt.Errorf("read Navidrome scan status: %w", err)
		}
		polls++
		switch {
		case state.Scanning:
			sawScanning = true
		case sawScanning:
			return nil
		case !state.LastScan.IsZero():
			// A quick scan can start and finish between two polls; lastScan
			// moving past its pre-request value is the signal for that.
			if !state.LastScan.Equal(before.LastScan) {
				return nil
			}
		case polls >= 3:
			// A server that omits lastScan and never showed the scan running
			// has nothing more precise to offer.
			return nil
		}
		if err := sleepCtx(finishCtx, scanPollInterval); err != nil {
			return fmt.Errorf("the Navidrome scan did not finish in time: %w", err)
		}
	}
}

func waitScanIdle(ctx context.Context, client scanStatusClient) (ScanState, error) {
	for {
		state, err := client.ScanState(ctx)
		if err != nil {
			return ScanState{}, err
		}
		if !state.Scanning {
			return state, nil
		}
		if err := sleepCtx(ctx, scanPollInterval); err != nil {
			return ScanState{}, err
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// MirrorOptions selects what SyncMirror does after the push.
type MirrorOptions struct {
	// Scan requests a scan when the push changed anything.
	Scan bool
	// ForceScan scans even when the push changed nothing.
	ForceScan bool
	// Dates re-applies Date Added after a scan.
	Dates bool
}

// MirrorResult reports a push and what followed it.
type MirrorResult struct {
	Push     PushResult       `json:"push"`
	Scanned  bool             `json:"scanned"`
	Dates    *ReconcileResult `json:"dates,omitempty"`
	Warnings []string         `json:"warnings"`
}

// SyncMirror pushes the library, rescans when anything changed, and then
// re-applies Date Added. Only the push can fail the call: the server keeps
// serving the previous mirror, so scan and date problems are warnings.
func (m *Manager) SyncMirror(ctx context.Context, opts MirrorOptions) (MirrorResult, error) {
	result := MirrorResult{Warnings: []string{}}
	push, err := m.Push(ctx)
	result.Push = push
	if err != nil {
		return result, err
	}
	if push.Warning != "" {
		result.Warnings = append(result.Warnings, push.Warning)
	}
	if !opts.Scan || (!push.HasChanges() && !opts.ForceScan) {
		return result, nil
	}
	client, err := m.Client()
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("pushed, but no scan was requested: %v", err))
		return result, nil
	}
	if err := scanAfterPush(ctx, client); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("pushed, but the scan did not complete: %v", err))
		return result, nil
	}
	result.Scanned = true
	if opts.Dates {
		dates, err := m.reconcileDateAddedRemote(ctx, false)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Date Added was not reconciled: %v", err))
		} else {
			result.Dates = &dates
		}
	}
	return result, nil
}

// pushingRefreshClient makes the playlist refresh push the rewritten `.nsp`
// files before the scan that imports them; on a mirror the server would
// otherwise rescan the old files.
type pushingRefreshClient struct {
	manager *Manager
	client  *Client
}

func (p pushingRefreshClient) StartScan(ctx context.Context, _ bool) error {
	if _, err := p.manager.Push(ctx); err != nil {
		return err
	}
	return scanAfterPush(ctx, p.client)
}

func (p pushingRefreshClient) Playlists(ctx context.Context) ([]Playlist, error) {
	return p.client.Playlists(ctx)
}

func (p pushingRefreshClient) Playlist(ctx context.Context, id string) (Playlist, []Song, error) {
	return p.client.Playlist(ctx, id)
}

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

	// deleteSlack lets the real push delete the folders and orphaned temp
	// files the preflight also listed, on top of the files it counted.
	deleteSlack = 25

	// MirrorStateFileName records push and scan outcomes on this Mac, so a
	// failure is visible in status and an unfinished scan is retried.
	MirrorStateFileName = "udl-mirror-state.json"
)

// mirrorFilters decide what the mirror holds. Navidrome treats a name as
// hidden only when it starts with "." and not ".." — verified against 0.63.2,
// which indexes "...Ready For It.mp3" — so ".."-names are mirrored while every
// other dot-name stays on the Mac. The excludes are sender-side only (`-s`):
// hidden files on the server, such as temp files orphaned by a crash, are
// deleted like any other stray file, and rsync still protects its own
// `.~tmp~` partial folder.
var mirrorFilters = []string{
	"--filter=+ ..*",
	"--filter=-s .*",
	"--filter=-s *.part", "--filter=-s *.partial", "--filter=-s *.tmp", "--filter=-s *.crdownload",
}

// isHiddenName mirrors Navidrome's rule for hidden files and folders.
func isHiddenName(name string) bool {
	return strings.HasPrefix(name, ".") && !strings.HasPrefix(name, "..")
}

// hasHiddenSegment reports whether any segment of a mirror-relative path is
// hidden, e.g. rsync's `X/.~tmp~/song.mp3` partial.
func hasHiddenSegment(rel string) bool {
	for _, segment := range strings.Split(rel, "/") {
		if isHiddenName(segment) {
			return true
		}
	}
	return false
}

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

// totalTransferred reads `--stats` "Total transferred file size: 1,234 bytes".
var totalTransferred = regexp.MustCompile(`(?m)^Total transferred file size: ([0-9.,]+) bytes`)

// ErrDeleteLimit marks a push refused by the deletion preflight; nothing was
// changed on the server.
var ErrDeleteLimit = errors.New("push refused by the delete limit")

// ErrMirrorBusy means another udl push to the mirror is running on this Mac.
var ErrMirrorBusy = errors.New("another udl push to the mirror is running; this one was skipped")

// mirrorFreeFloor is the free space a push must leave on the server; the
// gate enforces the same floor per session.
const mirrorFreeFloor = 20 << 30

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
	// rsync's ssh child can outlive a killed rsync and hold its pipes open.
	cmd.WaitDelay = 5 * time.Second
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
	// PlannedDeletes is the preflight's count of files the push would delete.
	PlannedDeletes int `json:"planned_deletes,omitempty"`
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
//   - mirrorFilters: see there.
//
// A dry run (the deletion preflight) omits --max-delete; the real push passes
// the preflight's count plus slack, so it never stops half-way through.
func RsyncPushArgs(cfg Config, resolved Resolved, maxDelete int, dryRun bool) []string {
	remote := cfg.Remote
	if remote == nil {
		remote = &RemoteConfig{}
	}
	args := []string{
		"--recursive", "--times", "--perms", "--chmod=D755,F644",
		"--delete-delay", "--delay-updates", "--partial", "--fuzzy",
	}
	if dryRun {
		// --stats sizes the transfer for the free-space check.
		args = append(args, "--dry-run", "--stats")
	} else {
		args = append(args, fmt.Sprintf("--max-delete=%d", maxDelete))
	}
	args = append(args, fmt.Sprintf("--timeout=%d", remoteRsyncTimeoutSeconds))
	args = append(args, mirrorFilters...)
	return append(args,
		"--itemize-changes", "--out-format=%i %n%L",
		"-e", remote.SSHPath+" -o BatchMode=yes",
		strings.TrimRight(resolved.MusicDir, "/")+"/",
		remote.SSHTarget+":",
	)
}

// countPlannedDeletions reads a dry run: tracks are deleted files that are
// not hidden; total is every deletion rsync will count against --max-delete.
func countPlannedDeletions(output string) (tracks, total int) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "*deleting") {
			continue
		}
		total++
		name := strings.TrimSpace(strings.TrimPrefix(line, "*deleting"))
		if strings.HasSuffix(name, "/") || hasHiddenSegment(name) {
			continue
		}
		tracks++
	}
	return tracks, total
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
			// A folder emptied by the push is listed too, and so is a hidden
			// temp file being cleaned up; only real files count.
			name := strings.TrimSpace(strings.TrimPrefix(line, "*deleting"))
			if strings.HasSuffix(name, "/") || hasHiddenSegment(name) {
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
//
// A dry run comes first and the push is refused outright — nothing copied,
// nothing deleted — when it would delete more than max_delete (or the
// explicit allowDeletes) files. rsync's own --max-delete would instead stop
// half-way and leave old and new paths side by side for the next scan to
// index as different tracks.
func (m *Manager) Push(ctx context.Context, allowDeletes int) (PushResult, error) {
	remote, err := m.requireRemote()
	if err != nil {
		return PushResult{}, err
	}
	if err := checkPushSource(m.Resolved.MusicDir, m.Resolved.PlaylistsPath); err != nil {
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

	result := PushResult{StartedAt: m.now()}
	preflight, err := run(ctx, nil, remote.RsyncPath, RsyncPushArgs(m.Config, m.Resolved, 0, true)...)
	if err != nil {
		return result, fmt.Errorf("mirror preflight did not finish: %w", err)
	}
	if err := rsyncExitError(preflight, "preflight"); err != nil {
		return result, err
	}
	tracks, total := countPlannedDeletions(string(preflight.Stdout))
	result.PlannedDeletes = tracks
	limit := remote.MaxDelete
	if allowDeletes > limit {
		limit = allowDeletes
	}
	if tracks > limit {
		result.FinishedAt = m.now()
		return result, fmt.Errorf(
			"%w: it would delete %d files from the server (limit %d), so nothing was pushed. A moved, renamed or missing library folder looks like this; if the deletions are intended, run `udl navidrome push --allow-deletes %d`",
			ErrDeleteLimit, tracks, limit, tracks)
	}
	if err := m.checkServerSpace(ctx, string(preflight.Stdout)); err != nil {
		result.FinishedAt = m.now()
		return result, err
	}

	args := RsyncPushArgs(m.Config, m.Resolved, total+deleteSlack, false)
	result.Command = remote.RsyncPath + " " + strings.Join(args, " ")
	out, err := run(ctx, nil, remote.RsyncPath, args...)
	result.FinishedAt = m.now()
	result.Changed, result.Deleted, result.Items = ParseItemizedChanges(string(out.Stdout))
	if err != nil {
		return result, fmt.Errorf("mirror push did not finish: %w", err)
	}
	switch {
	case out.ExitCode == rsyncExitVanished:
		result.Warning = "some files changed or vanished during the push; the next push will catch up"
		return result, nil
	case out.ExitCode == rsyncExitPartial && !deletionsSkipped(out):
		// A file that cannot be read is simply not updated; everything else,
		// deletions included, went through and is safe to scan.
		result.Warning = "some files could not be read on this Mac and were not pushed: " +
			lastLines(strings.TrimSpace(string(out.Stderr)), 5)
		return result, nil
	}
	return result, rsyncExitError(out, "push")
}

// deletionsSkipped reports rsync's "IO error encountered -- skipping file
// deletion": after an unreadable folder it deletes nothing, so moved files
// exist under both names until a clean push.
func deletionsSkipped(out RemoteOutput) bool {
	return strings.Contains(string(out.Stderr), "skipping file deletion")
}

// checkServerSpace refuses a push that would leave the server below the free
// space floor. The gate's own check runs once per session, before rsync
// writes anything, so a single large push needs this one.
func (m *Manager) checkServerSpace(ctx context.Context, preflight string) error {
	match := totalTransferred.FindStringSubmatch(preflight)
	if match == nil {
		return nil
	}
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, match[1])
	var size int64
	if _, err := fmt.Sscan(digits, &size); err != nil || size == 0 {
		return nil
	}
	statusCtx, cancel := context.WithTimeout(ctx, remoteStatusTimeout)
	defer cancel()
	status, err := m.RemoteStatus(statusCtx)
	if err != nil || status.Disk.FreeBytes == 0 {
		// The gate still refuses a session below the floor.
		return nil
	}
	if size > status.Disk.FreeBytes-mirrorFreeFloor {
		return fmt.Errorf("the push would transfer %d MiB but the server has only %d MiB free (it keeps %d GiB free); nothing was pushed",
			size>>20, status.Disk.FreeBytes>>20, mirrorFreeFloor>>30)
	}
	return nil
}

// rsyncExitError explains a failed rsync run; exit 24 (files vanished while
// being read) is not a failure.
func rsyncExitError(out RemoteOutput, step string) error {
	stderr := strings.TrimSpace(string(out.Stderr))
	switch out.ExitCode {
	case 0, rsyncExitVanished:
		return nil
	case rsyncExitPartial:
		if deletionsSkipped(out) {
			// After an unreadable folder rsync deletes nothing, so moved files
			// exist on the server under both names; no scan may run until a
			// clean push has removed the old ones.
			return fmt.Errorf("the mirror %s could not read a folder on this Mac, so rsync skipped all deletions; fix it and push again: %s",
				step, lastLines(stderr, 5))
		}
		return fmt.Errorf("the mirror %s could not read some files on this Mac: %s", step, lastLines(stderr, 5))
	case rsyncExitMaxDelete:
		return fmt.Errorf("the mirror %s stopped at its delete limit; push again", step)
	}
	if strings.Contains(stderr, "Another instance of rrsync") {
		return errors.New("another push to the mirror is still running; try again shortly")
	}
	return fmt.Errorf("mirror %s failed (rsync exit %d): %s", step, out.ExitCode, lastLines(stderr, 5))
}

// checkPushSource refuses to mirror a missing or empty library: with
// --delete, that would empty the server. The managed playlists folder does
// not count, because a playlist refresh recreates it on its own.
func checkPushSource(musicDir, playlistsPath string) error {
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
		if !isHiddenName(entry.Name()) && entry.Name() != filepath.Clean(playlistsPath) {
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

// mirrorExcluded are the non-hidden names the push never sends.
var mirrorExcluded = []string{"*.part", "*.partial", "*.tmp", "*.crdownload"}

// localLibraryShape counts the files the push would mirror and finds the
// newest modification time, matching the gate's own `status` walk.
func localLibraryShape(musicDir string) (files int, newest time.Time, err error) {
	root, err := filepath.EvalSymlinks(musicDir)
	if err != nil {
		return 0, time.Time{}, err
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if isHiddenName(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		for _, pattern := range mirrorExcluded {
			if matched, _ := filepath.Match(pattern, entry.Name()); matched {
				return nil
			}
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files++
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return files, newest, err
}

// staleMirrorProblems compares this Mac's library with the server's mirror.
// It catches what the state file cannot: a launchd push that never runs, or
// changes nobody pushed.
func (m *Manager) staleMirrorProblems(status RemoteStatus) []string {
	files, newest, err := localLibraryShape(m.Resolved.MusicDir)
	if err != nil {
		return []string{fmt.Sprintf("could not compare the library with the mirror: %v", err)}
	}
	problems := []string{}
	if files != status.Library.Files {
		problems = append(problems, fmt.Sprintf(
			"the server's mirror has %d files but this library has %d; run `udl navidrome push`",
			status.Library.Files, files))
	} else if server := status.Library.NewestMtime; server != nil && newest.Sub(*server) > 2*time.Second {
		problems = append(problems, fmt.Sprintf(
			"the server's mirror is behind this library (newest file %s here, %s there); run `udl navidrome push`",
			newest.Local().Format("2006-01-02 15:04"), server.Local().Format("2006-01-02 15:04")))
	}
	return problems
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
	// The manifest names the configured path — the server's library root —
	// but the walk follows it if it is a symlink, as rsync's `src/` does;
	// WalkDir would otherwise stop at the link and report an empty library.
	configured := strings.TrimRight(resolved.MusicDir, "/")
	manifest := BirthTimeManifest{Version: 1, MusicDir: configured, Entries: []BirthTimeEntry{}}
	root, err := filepath.EvalSymlinks(configured)
	if err != nil {
		return BirthTimeManifest{}, fmt.Errorf("resolve music_dir %s: %w", configured, err)
	}
	playlists := filepath.Clean(resolved.PlaylistsPath)
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if isHiddenName(entry.Name()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if rel, relErr := filepath.Rel(root, path); relErr == nil && rel == playlists {
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
	Rejected        int      `json:"rejected"`
	RejectedSample  []string `json:"rejected_sample"`
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
	if len(manifest.Entries) == 0 {
		// Reporting "already matches" for an empty walk would hide a broken
		// music_dir for good.
		return ReconcileResult{}, fmt.Errorf("found no audio files under %s; not reconciling Date Added", m.Resolved.MusicDir)
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
		Rejected:      reply.Rejected,
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
	if reply.Rejected > 0 {
		result.Message += fmt.Sprintf("; %d file(s) have an implausible creation time and were left alone, e.g. %s",
			reply.Rejected, strings.Join(reply.RejectedSample[:min(3, len(reply.RejectedSample))], ", "))
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
// files, so being idle first is what makes the request count. Zero waits use
// the defaults.
func scanAfterPush(ctx context.Context, client scanStatusClient, idleWait, finishWait time.Duration) error {
	if idleWait <= 0 {
		idleWait = scanIdleTimeout
	}
	if finishWait <= 0 {
		finishWait = scanFinishTimeout
	}
	idleCtx, cancelIdle := context.WithTimeout(ctx, idleWait)
	defer cancelIdle()
	before, err := waitScanIdle(idleCtx, client)
	if err != nil {
		return fmt.Errorf("wait for the running Navidrome scan: %w", err)
	}
	if err := client.StartScan(ctx, false); err != nil {
		return fmt.Errorf("request a Navidrome scan: %w", err)
	}
	finishCtx, cancelFinish := context.WithTimeout(ctx, finishWait)
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
	// Scan requests a scan when the push changed anything, or when an
	// earlier push's scan never finished.
	Scan bool
	// ForceScan scans even when the push changed nothing.
	ForceScan bool
	// Dates re-applies Date Added after a scan.
	Dates bool
	// AllowDeletes raises the delete limit for one intended reorganisation.
	AllowDeletes int
	// IdleWait and FinishWait bound the scan waits; zero uses the defaults.
	// A wait that runs out leaves the scan pending for the next push.
	IdleWait, FinishWait time.Duration
}

// MirrorState is what this Mac knows about the mirror between runs.
type MirrorState struct {
	LastPushAt    *time.Time `json:"last_push_at,omitempty"`
	LastScanAt    *time.Time `json:"last_scan_at,omitempty"`
	LastErrorAt   *time.Time `json:"last_error_at,omitempty"`
	LastPushError string     `json:"last_push_error,omitempty"`
	// ScanPending means files reached the server but its scan has not
	// completed yet. The server never scans on its own, so the next push
	// forces one.
	ScanPending bool `json:"scan_pending,omitempty"`
	// DatesPending means a scan completed but the Date Added reconcile
	// after it did not; the next push retries it without another scan.
	DatesPending bool `json:"dates_pending,omitempty"`
}

// MirrorStatePath is the state file inside the (local) data directory.
func MirrorStatePath(resolved Resolved) string {
	return filepath.Join(resolved.DataDir, MirrorStateFileName)
}

// LoadMirrorState reads the state; a missing file is a fresh state.
func LoadMirrorState(resolved Resolved) (MirrorState, error) {
	var state MirrorState
	payload, err := os.ReadFile(MirrorStatePath(resolved))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(payload, &state); err != nil {
		return MirrorState{}, fmt.Errorf("parse %s: %w", MirrorStatePath(resolved), err)
	}
	return state, nil
}

func saveMirrorState(resolved Resolved, state MirrorState) error {
	if err := os.MkdirAll(resolved.DataDir, 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(MirrorStatePath(resolved), append(payload, '\n'), 0o600)
}

func (m *Manager) mirrorProblems() []string {
	state, err := LoadMirrorState(m.Resolved)
	if err != nil {
		return []string{err.Error()}
	}
	problems := []string{}
	switch {
	case state.LastPushError != "":
		when := "at an unknown time"
		if state.LastErrorAt != nil {
			when = state.LastErrorAt.Local().Format("2006-01-02 15:04")
		}
		problems = append(problems, fmt.Sprintf(
			"the last mirror push failed (%s): %s — the server is serving the previous mirror",
			when, state.LastPushError))
	case state.LastPushAt == nil:
		problems = append(problems, "this Mac has never pushed the mirror; run `udl navidrome push`")
	}
	if state.ScanPending {
		problems = append(problems, "the server has not finished scanning the last push; the next `udl navidrome push` retries it")
	}
	if state.DatesPending {
		problems = append(problems, "Date Added was not reconciled after the last scan; the next `udl navidrome push` retries it (or run `udl navidrome dates reconcile`)")
	}
	return problems
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
	unlock, err := lockMirror(m.Resolved)
	if err != nil {
		// A concurrent run owns the state file; record nothing.
		return result, err
	}
	defer unlock()
	state, stateErr := LoadMirrorState(m.Resolved)
	if stateErr != nil {
		result.Warnings = append(result.Warnings, stateErr.Error())
	}
	record := func() {
		if err := saveMirrorState(m.Resolved, state); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not record the mirror state: %v", err))
		}
	}
	push, err := m.Push(ctx, opts.AllowDeletes)
	result.Push = push
	now := m.now()
	if err != nil {
		state.LastPushError, state.LastErrorAt = err.Error(), &now
		// A push that failed after transferring files still owes a scan.
		state.ScanPending = state.ScanPending || push.HasChanges()
		record()
		return result, err
	}
	state.LastPushAt, state.LastPushError, state.LastErrorAt = &now, "", nil
	if push.Warning != "" {
		result.Warnings = append(result.Warnings, push.Warning)
	}
	state.ScanPending = state.ScanPending || push.HasChanges() || (opts.Scan && opts.ForceScan)
	if !opts.Scan || (!state.ScanPending && !(opts.Dates && state.DatesPending)) {
		record()
		return result, nil
	}
	record()
	if state.ScanPending {
		client, err := m.Client()
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("pushed, but no scan was requested: %v", err))
			return result, nil
		}
		if err := scanAfterPush(ctx, client, opts.IdleWait, opts.FinishWait); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("pushed, but the scan did not complete: %v", err))
			return result, nil
		}
		result.Scanned = true
		scanned := m.now()
		state.LastScanAt, state.ScanPending, state.DatesPending = &scanned, false, opts.Dates
		record()
	}
	if opts.Dates && state.DatesPending {
		dates, err := m.reconcileDateAddedRemote(ctx, false)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Date Added was not reconciled: %v", err))
			return result, nil
		}
		result.Dates = &dates
		state.DatesPending = false
		record()
	}
	return result, nil
}

// pushingRefreshClient makes the playlist refresh push the rewritten `.nsp`
// files before the scan that imports them; on a mirror the server would
// otherwise rescan the old files. It runs the full mirror sequence, so audio
// that travels with the playlists gets its Date Added too.
type pushingRefreshClient struct {
	manager *Manager
	client  *Client
}

func (p pushingRefreshClient) StartScan(ctx context.Context, _ bool) error {
	result, err := p.manager.SyncMirror(ctx, MirrorOptions{Scan: true, ForceScan: true, Dates: true})
	if err != nil {
		return err
	}
	if !result.Scanned {
		return errors.New(strings.Join(result.Warnings, "; "))
	}
	return nil
}

func (p pushingRefreshClient) Playlists(ctx context.Context) ([]Playlist, error) {
	return p.client.Playlists(ctx)
}

func (p pushingRefreshClient) Playlist(ctx context.Context, id string) (Playlist, []Song, error) {
	return p.client.Playlist(ctx, id)
}

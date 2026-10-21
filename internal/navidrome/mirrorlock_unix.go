//go:build unix

package navidrome

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// lockMirror takes the per-Mac mirror lock without waiting. The post-sync
// hook, the launchd fallback and a manual push would otherwise interleave
// their state-file updates and lose a pending scan.
func lockMirror(resolved Resolved) (func(), error) {
	if err := os.MkdirAll(resolved.DataDir, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(resolved.DataDir, "udl-mirror.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrMirrorBusy
		}
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}, nil
}

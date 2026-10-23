//go:build darwin || linux

package navidrome

import (
	"os"

	"golang.org/x/sys/unix"
)

// readablePushSourceFile probes access without reading any audio or updating
// its access time through a read. Nonblocking, no-follow open prevents a file
// replaced by a FIFO or symlink after enumeration from blocking or following
// the link. Recheck the opened descriptor's type before counting it.
func readablePushSourceFile(path string) bool {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	return statErr == nil && info.Mode().IsRegular() && closeErr == nil
}

//go:build darwin

package navidrome

import (
	"syscall"
	"time"
)

// FileBirthTime reads the APFS creation time, which is what Date Added means.
func FileBirthTime(path string) (time.Time, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		return time.Time{}, err
	}
	return time.Unix(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec).UTC(), nil
}

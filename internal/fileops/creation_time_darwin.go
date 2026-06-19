//go:build darwin

package fileops

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ReadCreationTime returns the filesystem birth time shown by Finder.
func ReadCreationTime(path string) (CreationTime, error) {
	info, err := os.Stat(path)
	if err != nil {
		return CreationTime{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return CreationTime{}, fmt.Errorf("read creation time: unsupported stat type %T", info.Sys())
	}
	ts := stat.Birthtimespec
	return CreationTime{
		Time:      time.Unix(ts.Sec, ts.Nsec),
		Available: true,
	}, nil
}

// RestoreCreationTime sets only the filesystem birth time, leaving mtime intact.
func RestoreCreationTime(path string, creation CreationTime) error {
	if !creation.Available {
		return nil
	}
	ts := unix.NsecToTimespec(creation.Time.UnixNano())
	attrs := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_CRTIME,
	}
	buffer := unsafe.Slice((*byte)(unsafe.Pointer(&ts)), int(unsafe.Sizeof(ts)))
	if err := unix.Setattrlist(path, &attrs, buffer, 0); err != nil {
		return fmt.Errorf("restore creation time for %q: %w", path, err)
	}
	return nil
}

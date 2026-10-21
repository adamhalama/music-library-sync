//go:build !darwin

package navidrome

import (
	"errors"
	"time"
)

// FileBirthTime is only meaningful on the Mac that owns the library: other
// systems cannot report the APFS creation time Date Added is defined by.
func FileBirthTime(string) (time.Time, error) {
	return time.Time{}, errors.New("file creation times are only read on macOS")
}

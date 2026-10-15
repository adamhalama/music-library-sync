//go:build darwin

package navidrome

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// birthTime reads the APFS creation time. Date Added comes from this value, so
// the acceptance manifest has to compare it directly rather than settle for
// modification time.
func birthTime(t *testing.T, path string) time.Time {
	t.Helper()
	var stat syscall.Stat_t
	if err := syscall.Stat(path, &stat); err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return time.Unix(stat.Birthtimespec.Sec, stat.Birthtimespec.Nsec).UTC()
}

// setBirthTime rewrites a file's creation time so the acceptance fixtures have
// a controlled Date Added order, including a deliberate tie.
func setBirthTime(t *testing.T, path string, when time.Time) {
	t.Helper()
	// `SetFile -d` is the only supported way to move an APFS birth time; it
	// ships with the Xcode command line tools.
	setFile, err := exec.LookPath("SetFile")
	if err != nil {
		t.Skipf("SetFile is required to control fixture creation times: %v", err)
	}
	stamp := when.Local().Format("01/02/2006 15:04:05")
	if out, err := exec.Command(setFile, "-d", stamp, path).CombinedOutput(); err != nil {
		t.Fatalf("SetFile -d %q %s: %v: %s", stamp, path, err, out)
	}
}

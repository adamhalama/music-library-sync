//go:build darwin || linux

package navidrome

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadablePushSourceFileRejectsReplacementTypes(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "regular.mp3")
	writeFile(t, regular)
	link := filepath.Join(root, "link.mp3")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "fifo.mp3")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		want bool
	}{
		{path: regular, want: true},
		{path: link},
		{path: fifo},
		{path: root},
		{path: filepath.Join(root, "missing.mp3")},
	} {
		t.Run(filepath.Base(tc.path), func(t *testing.T) {
			done := make(chan bool, 1)
			go func() { done <- readablePushSourceFile(tc.path) }()
			select {
			case got := <-done:
				if got != tc.want {
					t.Fatalf("readablePushSourceFile = %v, want %v", got, tc.want)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("the access probe blocked on a non-regular replacement")
			}
		})
	}
}

func TestReadablePushSourceFileDoesNotChangeAccessTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "track.mp3")
	writeFile(t, path)
	old := time.Unix(1_600_000_000, 123_000_000)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	// Stat_t uses platform-specific access-time fields; compare the complete
	// stat snapshots except descriptor-open bookkeeping, which changes none
	// of their timestamps on the supported filesystems.
	var statBefore, statAfter unix.Stat_t
	if err := unix.Stat(path, &statBefore); err != nil {
		t.Fatal(err)
	}
	if !readablePushSourceFile(path) {
		t.Fatal("the regular file must be readable")
	}
	if err := unix.Stat(path, &statAfter); err != nil {
		t.Fatal(err)
	}
	if statBefore != statAfter {
		t.Fatal("open/stat/close changed file metadata without reading contents")
	}
}

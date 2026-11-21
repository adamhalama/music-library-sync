package playlistsync

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLibraryWriterProcessNamesMatchesExecutableNamesOnly(t *testing.T) {
	ps := `/sbin/launchd
/Applications/rekordbox 7/rekordbox.app/Contents/MacOS/rekordbox
/Applications/rbxport.app/Contents/MacOS/rbxport
/Library/Application Support/Pioneer/rekordboxAgent.app/Contents/MacOS/rekordboxAgent
/Users/dj/.t3/worktrees/update-downloads/feat-rekordbox-spotify/udl
/Users/dj/dev/rekordbox-tools/bin/udl
`
	got := libraryWriterProcessNames(ps)
	want := []string{"rbxport", "rekordbox", "rekordboxAgent"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCopyOverrideRejectsLiveAndSymlinkedLibraries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	live := filepath.Join(home, "Library", "Pioneer", "rekordbox")
	if err := os.MkdirAll(live, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(live, "master.db"), []byte("live"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLibraryCopy(live); err == nil {
		t.Fatal("accepted live default")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(live, alias); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLibraryCopy(alias); err == nil {
		t.Fatal("accepted live alias")
	}
	copyDir := t.TempDir()
	if err := os.Link(filepath.Join(live, "master.db"), filepath.Join(copyDir, "master.db")); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLibraryCopy(copyDir); err == nil {
		t.Fatal("accepted hard-linked database")
	}
}

func TestCopyOverrideStillRejectsSidecars(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "master.db-wal"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	err := CheckRekordboxClosedForCopy(context.Background(), dir, true)
	if err == nil || !strings.Contains(err.Error(), "sidecar") {
		t.Fatalf("got %v", err)
	}
}

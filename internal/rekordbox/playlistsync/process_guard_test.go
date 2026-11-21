package playlistsync

import (
	"reflect"
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

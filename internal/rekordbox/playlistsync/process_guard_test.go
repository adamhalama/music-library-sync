package playlistsync

import (
	"context"
	"encoding/json"
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

func TestCopyOverrideRejectsRekordboxConfiguredLibrary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configured := filepath.Join(t.TempDir(), "moved-rekordbox")
	if err := os.MkdirAll(filepath.Join(configured, "share"), 0700); err != nil {
		t.Fatal(err)
	}
	const configuredDB = "configured.db"
	if err := os.WriteFile(filepath.Join(configured, configuredDB), []byte("live"), 0600); err != nil {
		t.Fatal(err)
	}
	optionsDir := filepath.Join(home, "Library", "Application Support", "Pioneer", "rekordboxAgent", "storage")
	if err := os.MkdirAll(optionsDir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(configured, alias); err != nil {
		t.Fatal(err)
	}
	for _, configRoot := range []string{configured, alias} {
		options, err := json.Marshal(map[string]any{"options": [][]string{{"db-path", filepath.Join(configRoot, configuredDB)}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(optionsDir, "options.json"), options, 0600); err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{configured, alias, filepath.Join(configured, "share")} {
			if err := CheckRekordboxClosedForCopy(context.Background(), target, true); err == nil {
				t.Fatalf("accepted live path %s with configured path %s", target, configRoot)
			}
		}
		for name, liveName := range map[string]string{"master.db": configuredDB, "share": "share"} {
			copyDir := t.TempDir()
			if err := os.Symlink(filepath.Join(configured, liveName), filepath.Join(copyDir, name)); err != nil {
				t.Fatal(err)
			}
			if err := ValidateLibraryCopy(copyDir); err == nil {
				t.Fatalf("accepted copied library sharing configured %s", name)
			}
		}
		copyDir := t.TempDir()
		if err := os.Link(filepath.Join(configured, configuredDB), filepath.Join(copyDir, "master.db")); err != nil {
			t.Fatal(err)
		}
		if err := ValidateLibraryCopy(copyDir); err == nil {
			t.Fatal("accepted hard link to configured database")
		}
		independent := t.TempDir()
		if err := os.WriteFile(filepath.Join(independent, "master.db"), []byte("copy"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := CheckRekordboxClosedForCopy(context.Background(), independent, true); err != nil {
			t.Fatalf("rejected independent library: %v", err)
		}
	}
}

func TestCopyOverrideRejectsInvalidRekordboxOptions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	optionsDir := filepath.Join(home, "Library", "Application Support", "Pioneer", "rekordboxAgent", "storage")
	if err := os.MkdirAll(optionsDir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{`, `{"options":[["db-path",null]]}`, `{"options":[["db-path",""]]}`, `{"options":[["db-path","relative/master.db"]]}`} {
		if err := os.WriteFile(filepath.Join(optionsDir, "options.json"), []byte(payload), 0600); err != nil {
			t.Fatal(err)
		}
		if err := ValidateLibraryCopy(t.TempDir()); err == nil {
			t.Fatalf("accepted invalid options: %s", payload)
		}
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

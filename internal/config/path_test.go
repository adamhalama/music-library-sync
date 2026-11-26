package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTargetDirResolvesExistingParentOfMissingFolder(t *testing.T) {
	target, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveTargetDir(filepath.Join(alias, "missing", "music"))
	if want := filepath.Join(target, "missing", "music"); err != nil || got != want {
		t.Fatalf("resolved target = %q, want %q: %v", got, want, err)
	}
}

func TestResolveArchiveFilePerSourceForSimpleName(t *testing.T) {
	got, err := ResolveArchiveFile("/tmp/state", "archive.txt", "soundcloud-a")
	if err != nil {
		t.Fatalf("resolve archive file: %v", err)
	}
	want := filepath.Clean("/tmp/state/soundcloud-a.archive.txt")
	if got != want {
		t.Fatalf("unexpected archive path. got=%q want=%q", got, want)
	}
}

func TestResolveArchiveFileKeepsNestedRelativePath(t *testing.T) {
	got, err := ResolveArchiveFile("/tmp/state", "archives/soundcloud.txt", "soundcloud-a")
	if err != nil {
		t.Fatalf("resolve archive file: %v", err)
	}
	want := filepath.Clean("/tmp/state/archives/soundcloud.txt")
	if got != want {
		t.Fatalf("unexpected archive path. got=%q want=%q", got, want)
	}
}

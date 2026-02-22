package config

import (
	"path/filepath"
	"testing"
)

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

package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportSourceModeFlags(t *testing.T) {
	root := t.TempDir()
	write := func(path, body string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "A.mp3"), "fixture", 0o600)
	write(filepath.Join(root, "B.mp3"), "fixture", 0o600)
	write(filepath.Join(root, "state.scdl"), "soundcloud 11 A.mp3\nsoundcloud 12 B.mp3\n", 0o600)
	write(filepath.Join(root, "yt-dlp"), "#!/bin/sh\nprintf '11\\tA\\thttps://soundcloud.com/example/a\\n12\\tB\\thttps://soundcloud.com/example/b\\n'\n", 0o700)
	t.Setenv("PATH", root)
	python := filepath.Join(root, "python")
	write(python, fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-c" ]; then
  printf 'test-version\n'
  exit 0
fi
IFS= read -r request
case "$request" in
  *'"op":"source_import_inspect"'*) printf '%%s\n' '%s';;
  *) exit 1;;
esac
`, fmt.Sprintf(`{"contents":[{"id":"21","folder_path":%q,"created_at":"2026-01-02T00:00:00.500Z","stock_date":"2026-01-02"},{"id":"22","folder_path":%q,"created_at":"2026-01-01T00:00:00.500Z","stock_date":"2026-01-01"}]}`,
		filepath.Join(root, "A.mp3"), filepath.Join(root, "B.mp3"))), 0o700)
	rbConfig := filepath.Join(root, "rekordbox.yaml")
	write(rbConfig, "version: 1\n", 0o600)

	for _, tc := range []struct {
		name, setting, flag, mode string
		appendOnly                bool
		dateChanges               int
	}{
		{name: "default", mode: "new-only (default)"},
		{name: "saved_false", setting: "false", mode: "full reorder (rekordbox_import.new_only=false)", dateChanges: 1},
		{name: "flag_false", flag: "--new-only=false", mode: "full reorder (--new-only=false)", dateChanges: 1},
		{name: "flag_true_overrides_saved_false", setting: "false", flag: "--new-only", mode: "new-only (--new-only=true)"},
		{name: "append_only_overrides_flag_false", flag: "--new-only=false", appendOnly: true, mode: "new-only (sync.append_only=true)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			app := &AppContext{IO: IOStreams{Out: &out, ErrOut: &errOut}}
			path := filepath.Join(root, "config.yaml")
			body := fmt.Sprintf(`version: 1
defaults:
  state_dir: %q
sources:
  - id: sample
    type: soundcloud
    target_dir: %q
    url: https://soundcloud.com/example/sets/sample
    state_file: state.scdl
    sync:
      append_only: %t
    adapter:
      kind: scdl
`, root, root, tc.appendOnly)
			if tc.setting != "" {
				body += "    rekordbox_import:\n      new_only: " + tc.setting + "\n"
			}
			write(path, body, 0o600)
			cmd := newRootCommand(app)
			args := []string{"--config", path, "--rekordbox-config", rbConfig, "rekordbox", "import-source", "sample", "--dry-run", "--python-bin", python,
				"--rekordbox-db-dir", filepath.Join(root, "db"), "--backup-dir", filepath.Join(root, "backup")}
			if tc.flag != "" {
				args = append(args, tc.flag)
			}
			cmd.SetArgs(args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("import-source dry run: %v\n%s", err, &errOut)
			}
			if !strings.Contains(out.String(), "Import mode: "+tc.mode) || !strings.Contains(out.String(), fmt.Sprintf("Date Added changes: %d", tc.dateChanges)) {
				t.Fatalf("unexpected import plan:\n%s", &out)
			}
		})
	}
}

package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A separate process gives scdl's cached runtime detection its own fake binary.
func TestSoundCloudSyncCLIHelper(t *testing.T) {
	if os.Getenv("UDL_TEST_SC_HELPER") != "1" {
		return
	}
	app := &AppContext{IO: IOStreams{In: strings.NewReader(""), Out: os.Stdout, ErrOut: os.Stderr}}
	root := newRootCommand(app)
	root.SetArgs([]string{"--config", os.Getenv("UDL_TEST_SC_CONFIG"), "--no-input", "sync"})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

const policyFakeSCDL = `#!/bin/sh
if [ "$1" = --version ]; then echo 3.0.4; exit 0; fi
if [ "$1" = -h ]; then echo --yt-dlp-args; exit 0; fi
dir=$PWD
archive=$UDL_TEST_SC_ARCHIVE
while [ "$#" -gt 0 ]; do
 case "$1" in
  -l) url=$2; shift;;
  --sync) state=$2; shift;;
  --path) dir=$2; archive="$(dirname "$dir")/archive.txt"; shift;;
  --yt-dlp-args) yargs=$2; shift;;
 esac
 shift
done
if [ "$UDL_TEST_SC_FAIL" = 1 ]; then
 printf 'failed audio' > "$dir/vanished.m4a"
 printf 'partial' > "$dir/vanished.m4a.part"
 exit 1
fi
# Model scdl 3.0.4: remove sync files not visited remotely, truncate the state,
# and leave previously recorded yt-dlp archive IDs in place.
if [ -f "$state" ]; then
 while read -r provider id file; do
  if [ "$id" = 101 ]; then rm -f "$file"; fi
 done < "$state"
fi
: > "$state"
case "$url" in
 */301) ids=301;;
 */302) ids=302;;
 *)
  case "$yargs" in
   *--playlist-items*) items=${yargs#*--playlist-items }; items=${items%% *};
     case "$items" in 2,1) ids='302 301';; *) ids='301 302';; esac;;
   *) ids='301 302';;
  esac;;
esac
for id in $ids; do
 echo "$id" >> "$UDL_TEST_SC_LOG"
 file="$dir/$id.m4a"
 printf 'new audio %s\n' "$id" > "$file"
 printf 'soundcloud %s %s\n' "$id" "$file" >> "$state"
 printf 'soundcloud %s\n' "$id" >> "$archive"
done
`

type policyFixture struct {
	target, state, archive, log, config string
	stateBefore, archiveBefore          []byte
}

func newPolicyFixture(t *testing.T, appendOnly bool, order string) policyFixture {
	t.Helper()
	root := t.TempDir()
	f := policyFixture{target: filepath.Join(root, "target"), state: filepath.Join(root, "state", "source.scdl"), archive: filepath.Join(root, "state", "sample.archive.txt"), log: filepath.Join(root, "execution.log"), config: filepath.Join(root, "config.yaml")}
	for _, dir := range []string{f.target, filepath.Dir(f.state), filepath.Join(root, "bin")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := filepath.Join(f.target, "vanished.m4a")
	if err := os.WriteFile(old, []byte("original vanished audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.stateBefore = []byte("soundcloud 101 " + old + "\n")
	f.archiveBefore = []byte("soundcloud 101\n")
	if appendOnly {
		f.stateBefore = []byte("# preserve spacing and blank lines\r\n\r\nsoundcloud 101 " + old + "\r\n")
		f.archiveBefore = []byte("soundcloud 101\r\n\r\n")
		// Changed metadata and missing local files must not schedule known IDs.
		f.stateBefore = append(f.stateBefore, []byte("soundcloud\t102\tchanged.m4a\nsoundcloud 103 missing.m4a\n")...)
		f.archiveBefore = append(f.archiveBefore, []byte("soundcloud 103\nsoundcloud 104\n")...)
		if err := os.WriteFile(filepath.Join(f.target, "changed.m4a"), []byte("original changed audio"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for path, data := range map[string][]byte{f.state: f.stateBefore, f.archive: f.archiveBefore} {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "bin", "scdl")
	if err := os.WriteFile(bin, []byte(policyFakeSCDL), 0o755); err != nil {
		t.Fatal(err)
	}
	remote := "301\tNewer\thttps://soundcloud.com/example/301\n302\tOlder\thttps://soundcloud.com/example/302\n"
	if appendOnly {
		remote = "102\tChanged title\thttps://soundcloud.com/example/102\n" + remote + "103\tMissing\thttps://soundcloud.com/example/103\n104\tArchive only\thttps://soundcloud.com/example/104\n"
	}
	fakeYTDLP := "#!/bin/sh\ncat <<'TRACKS'\n" + remote + "TRACKS\n"
	if err := os.WriteFile(filepath.Join(root, "bin", "yt-dlp"), []byte(fakeYTDLP), 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := fmt.Sprintf(`version: 1
defaults:
  state_dir: %q
  archive_file: archive.txt
  threads: 1
  continue_on_error: false
  command_timeout_seconds: 30
sources:
  - id: sample
    type: soundcloud
    enabled: true
    target_dir: %q
    url: https://soundcloud.com/example
    state_file: source.scdl
    sync:
      append_only: %t
      download_order: %q
    adapter:
      kind: scdl
`, filepath.Dir(f.state), f.target, appendOnly, order)
	if err := os.WriteFile(f.config, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UDL_TEST_SC_HELPER", "1")
	t.Setenv("UDL_TEST_SC_CONFIG", f.config)
	t.Setenv("UDL_TEST_SC_LOG", f.log)
	t.Setenv("UDL_TEST_SC_ARCHIVE", f.archive)
	t.Setenv("UDL_SCDL_BIN", bin)
	t.Setenv("SCDL_CLIENT_ID", "test-client-id")
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	return f
}

func runPolicySync(t *testing.T) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSoundCloudSyncCLIHelper$")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("non-interactive sync failed: %v\n%s", err, output)
	}
	return string(output)
}

func policyRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSyncSoundCloudAppendOnlyPreservesExistingBytes(t *testing.T) {
	f := newPolicyFixture(t, true, "oldest_first")
	out := runPolicySync(t)
	t.Log(out)
	if got := policyRead(t, filepath.Join(f.target, "vanished.m4a")); string(got) != "original vanished audio" {
		t.Fatalf("vanished track changed: %q", got)
	}
	if got := policyRead(t, filepath.Join(f.target, "changed.m4a")); string(got) != "original changed audio" {
		t.Fatalf("existing track changed: %q", got)
	}
	if got := policyRead(t, f.state); !bytes.Equal(got, append(append([]byte{}, f.stateBefore...), []byte("soundcloud 302 302.m4a\nsoundcloud 301 301.m4a\n")...)) {
		t.Fatalf("state was rewritten: %q", got)
	}
	if got := policyRead(t, f.archive); !bytes.Equal(got, append(append([]byte{}, f.archiveBefore...), []byte("soundcloud 302\nsoundcloud 301\n")...)) {
		t.Fatalf("archive was rewritten: %q", got)
	}
	if got := string(policyRead(t, f.log)); got != "302\n301\n" {
		t.Fatalf("wrong order or known IDs downloaded: %q", got)
	}
	for _, id := range []string{"301", "302"} {
		if got := string(policyRead(t, filepath.Join(f.target, id+".m4a"))); got != "new audio "+id+"\n" {
			t.Fatalf("new track not added: %q", got)
		}
	}
	// A subsequent run must not invoke scdl or append duplicate state/archive lines.
	state, archive := policyRead(t, f.state), policyRead(t, f.archive)
	runPolicySync(t)
	if !bytes.Equal(state, policyRead(t, f.state)) || !bytes.Equal(archive, policyRead(t, f.archive)) {
		t.Fatal("second run changed existing state/archive")
	}
	if string(policyRead(t, f.log)) != "302\n301\n" {
		t.Fatal("second run re-downloaded known tracks")
	}
	stages, err := filepath.Glob(filepath.Join(f.target, ".udl-append-*"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("staging files remain: %v %v", stages, err)
	}
}

func TestSyncSoundCloudDefaultStillRemovesVanishedTracks(t *testing.T) {
	f := newPolicyFixture(t, false, "")
	runPolicySync(t)
	if _, err := os.Stat(filepath.Join(f.target, "vanished.m4a")); !os.IsNotExist(err) {
		t.Fatalf("default sync did not remove vanished file: %v", err)
	}
	if bytes.Contains(policyRead(t, f.state), []byte("soundcloud 101")) {
		t.Fatal("default sync retained vanished state")
	}
	if !bytes.HasPrefix(policyRead(t, f.archive), f.archiveBefore) {
		t.Fatal("archive entries changed")
	}
}

func TestSyncSoundCloudConfiguredOrderNonInteractive(t *testing.T) {
	f := newPolicyFixture(t, false, "oldest_first")
	runPolicySync(t)
	if got := string(policyRead(t, f.log)); got != "302\n301\n" {
		t.Fatalf("configured order ignored: %q", got)
	}
}

func TestSyncSoundCloudAppendOnlyFailurePreservesExistingBytes(t *testing.T) {
	f := newPolicyFixture(t, true, "oldest_first")
	t.Setenv("UDL_TEST_SC_FAIL", "1")
	cmd := exec.Command(os.Args[0], "-test.run=^TestSoundCloudSyncCLIHelper$")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("failed download reported success: %s", output)
	}
	if string(policyRead(t, filepath.Join(f.target, "vanished.m4a"))) != "original vanished audio" {
		t.Fatal("failure overwrote existing file")
	}
	if !bytes.Equal(policyRead(t, f.state), f.stateBefore) || !bytes.Equal(policyRead(t, f.archive), f.archiveBefore) {
		t.Fatal("failure changed original state/archive")
	}
	stages, err := filepath.Glob(filepath.Join(f.target, ".udl-append-*"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("failed staging remains: %v %v", stages, err)
	}
}

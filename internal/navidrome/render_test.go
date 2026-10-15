package navidrome

import (
	"strings"
	"testing"
)

func TestRenderTOMLContainsPlanDecisions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	toml := RenderTOML(cfg, resolved, "/opt/homebrew/bin/ffmpeg")

	required := []string{
		OwnershipMarker,
		"RecentlyAddedByModTime = false",
		`PlaylistsPath = "` + DefaultPlaylistsPath + `"`,
		"AutoImportPlaylists = true",
		"EnableFavourites = true",
		"EnableStarRating = true",
		"EnableDownloads = true",
		"EnableExternalServices = false",
		"EnableInsightsCollector = false",
		"EnableLogRedacting = true",
		"DefaultReportRealPath = true",
		"[Scanner]",
		"[Backup]",
		"Count = 7",
	}
	for _, fragment := range required {
		if !strings.Contains(toml, fragment) {
			t.Fatalf("rendered TOML is missing %q:\n%s", fragment, toml)
		}
	}
	if !IsUDLOwned(toml) {
		t.Fatalf("rendered TOML must be recognised as UDL-owned")
	}
}

func TestRenderTOMLOmitsCredentialFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.Server.Username = "jaa"
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	lower := strings.ToLower(RenderTOML(cfg, resolved, "/opt/homebrew/bin/ffmpeg"))
	for _, forbidden := range []string{"password", "secret", "apikey", "api_key"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("rendered TOML must not contain %q", forbidden)
		}
	}
}

func TestRenderLaunchAgentIsOwnedAndEscaped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	cfg.Paths.DataDir = "~/Library/Application Support/UDL & Co/Navidrome"
	normalize(&cfg)
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	plist := RenderLaunchAgent("/opt/homebrew/bin/navidrome", "/opt/homebrew/bin/ffmpeg", resolved)
	if !IsUDLOwned(plist) {
		t.Fatalf("plist must carry the ownership marker")
	}
	if !strings.Contains(plist, "<string>"+LaunchAgentLabel+"</string>") {
		t.Fatalf("plist is missing the label:\n%s", plist)
	}
	if !strings.Contains(plist, "UDL &amp; Co") {
		t.Fatalf("plist must XML-escape paths:\n%s", plist)
	}
	if strings.Contains(plist, "UDL & Co") {
		t.Fatalf("plist contains a raw ampersand:\n%s", plist)
	}
	if !strings.Contains(plist, "--configfile") {
		t.Fatalf("plist must pass the managed config file:\n%s", plist)
	}
}

// A missing ffmpeg made every m4a and wav in the real library fail to stream
// with "Internal Server Error: invalid argument", because launchd's default
// PATH has no Homebrew prefix. Both halves of the fix are pinned here.
func TestRenderPinsFFmpegForTheLaunchdEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	toml := RenderTOML(cfg, resolved, "/opt/homebrew/bin/ffmpeg")
	if !strings.Contains(toml, `FFmpegPath = "/opt/homebrew/bin/ffmpeg"`) {
		t.Fatalf("TOML must pin ffmpeg by absolute path:\n%s", toml)
	}

	plist := RenderLaunchAgent("/opt/homebrew/bin/navidrome", "/opt/homebrew/bin/ffmpeg", resolved)
	if !strings.Contains(plist, "<key>PATH</key>") {
		t.Fatalf("plist must set a PATH:\n%s", plist)
	}
	if !strings.Contains(plist, "/opt/homebrew/bin") {
		t.Fatalf("the service PATH must include the Homebrew prefix:\n%s", plist)
	}
}

// Named without the setting it asserts on: t.TempDir() embeds the test name in
// the paths that get rendered into the file, so a literal name would match
// itself. The same trap already caught the credential-omission test.
func TestRenderTOMLLeavesTheTranscoderUnsetWhenUnknown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := DefaultConfig()
	resolved, err := Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// An empty setting would be worse than none: Navidrome would take it as a
	// configured path and stop searching PATH at all.
	if strings.Contains(RenderTOML(cfg, resolved, ""), "FFmpegPath") {
		t.Fatalf("an unknown ffmpeg must leave FFmpegPath unset entirely")
	}
}

func TestServicePathPrefersTheDiscoveredBinaries(t *testing.T) {
	path := ServicePath("/usr/local/bin/navidrome", "/opt/custom/bin/ffmpeg")
	dirs := strings.Split(path, ":")
	if dirs[0] != "/opt/custom/bin" {
		t.Fatalf("the discovered ffmpeg directory must come first: %q", path)
	}
	seen := map[string]int{}
	for _, dir := range dirs {
		seen[dir]++
		if seen[dir] > 1 {
			t.Fatalf("%q is duplicated in the service PATH: %q", dir, path)
		}
	}
	for _, required := range []string{"/usr/local/bin", "/usr/bin", "/bin"} {
		if seen[required] == 0 {
			t.Fatalf("the service PATH must retain %q: %q", required, path)
		}
	}
}

func TestIsUDLOwnedRejectsForeignContent(t *testing.T) {
	if IsUDLOwned("# hand written navidrome config\nPort = 4533\n") {
		t.Fatalf("a foreign file must never be treated as UDL-owned")
	}
}

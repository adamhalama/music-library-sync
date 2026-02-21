package config

import "fmt"

func DefaultTemplate() string {
	return fmt.Sprintf(`version: 1
defaults:
  state_dir: %q
  archive_file: %q
  threads: %d
  continue_on_error: true
  command_timeout_seconds: %d
sources:
  - id: "soundcloud-likes"
    type: "soundcloud"
    enabled: true
    target_dir: "~/Music/downloaded/sc-likes"
    url: "https://soundcloud.com/your-user"
    adapter:
      kind: "scdl"
      extra_args: ["-f"]

  - id: "spotify-groove"
    type: "spotify"
    enabled: true
    target_dir: "~/Music/downloaded/spotify-groove"
    url: "https://open.spotify.com/playlist/replace-me"
    state_file: "spotify-groove.sync.spotdl"
    adapter:
      kind: "spotdl"
      extra_args: ["--headless", "--print-errors"]
`, defaultStateDir(), "archive.txt", 1, 900)
}

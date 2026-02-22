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
    state_file: "soundcloud-likes.sync.scdl"
    sync:
      break_on_existing: true
      ask_on_existing: false
      local_index_cache: false
    adapter:
      kind: "scdl"
      extra_args: ["-f"]

  - id: "spotify-groove"
    type: "spotify"
    enabled: true
    target_dir: "~/Music/downloaded/spotify-groove"
    url: "https://open.spotify.com/playlist/replace-me"
    state_file: "spotify-groove.sync.spotify"
    adapter:
      kind: "deemix"
      extra_args: []
    sync:
      break_on_existing: true
      ask_on_existing: false

  # Optional legacy spotify adapter:
  # - id: "spotify-groove-legacy"
  #   type: "spotify"
  #   enabled: false
  #   target_dir: "~/Music/downloaded/spotify-groove"
  #   url: "https://open.spotify.com/playlist/replace-me"
  #   state_file: "spotify-groove-legacy.sync.spotify"
  #   adapter:
  #     kind: "spotdl"
  #     extra_args: ["--headless", "--print-errors"]
`, defaultStateDir(), "archive.txt", 1, 900)
}

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

  # Optional SoundCloud free-download flow (separate from scdl stream ripping):
  # - id: "soundcloud-likes-free"
  #   type: "soundcloud"
  #   enabled: false
  #   target_dir: "~/Music/downloaded/sc-likes-free"
  #   url: "https://soundcloud.com/your-user"
  #   state_file: "soundcloud-likes-free.sync.scdl"
  #   adapter:
  #     kind: "scdl-freedl"
  #   sync:
  #     break_on_existing: true
  #     ask_on_existing: false
  #     local_index_cache: false

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

# Optional Rekordbox playlist sync from Music.app:
# rekordbox:
#   db_dir: "~/Library/Pioneer/rekordbox"
#   # python_bin/python_path are optional advanced overrides. If omitted,
#   # UDL manages a private pyrekordbox venv under defaults.state_dir.
#   python_bin: ""
#   python_path: ""
#   backup_dir: "~/Music/rb-library-export"
#   playlist_sync:
#     jobs:
#       - id: "apple-favourites"
#         music_playlist: "Favourites"
#         music_playlist_id: "70C641CA78BB0F3C"
#         rekordbox_playlist: "fav_imports"
#         rekordbox_playlist_id: "3150438241"
#         mode: "mirror"
`, defaultStateDir(), "archive.txt", 1, 900)
}

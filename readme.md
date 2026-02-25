# update-downloads (`udl`)

`udl` is a production-style CLI for syncing local music folders from configured SoundCloud and Spotify sources.

It replaces hardcoded orchestration with:
- YAML config (XDG user config + optional project override)
- Validation and doctor checks
- Deterministic sync execution
- Human output by default and structured `--json` output for automation

The legacy script remains available during migration: `bin/update-downloads`.

## Current Status

- CLI name: `udl`
- v1 model: Go app orchestrating external adapters (`deemix`, `spotdl`, `scdl`)
- Scope: `init`, `validate`, `doctor`, `sync`, `version`

## Requirements

Runtime tools:
- `scdl` (required for `adapter.kind: scdl`)
- `deemix` (recommended for Spotify)
- `spotdl` (legacy/fallback Spotify path)
- `yt-dlp` (required for SoundCloud preflight diff mode and `adapter.kind: scdl-freedl`)

Dependency policy:
- `scdl`/`yt-dlp` are managed as strict external dependencies with a supported compatibility matrix enforced by `udl doctor`.
- See `docs/dependency-matrix.md` for supported ranges, upgrade flow, and rollback behavior.
- `spotdl` remains supported but is transitional and outside the strict matrix scope for this phase.

Environment:
- `SCDL_CLIENT_ID` (required for SoundCloud sources)
- `UDL_DEEMIX_ARL` (or macOS Keychain item `service=udl.deemix account=default`) for Spotify+deemix
- `UDL_SPOTIFY_CLIENT_ID` + `UDL_SPOTIFY_CLIENT_SECRET` for Spotify+deemix conversion (fallback: `~/.spotdl/config.json`)

Build tooling:
- Go 1.22+

## Install

Build and install the CLI:

```bash
make install
```

This installs `udl` into your local Homebrew prefix `bin` directory (for example `/opt/homebrew/bin/udl` or `/usr/local/bin/udl`).

Legacy script install (optional during migration):

```bash
make legacy-install
```

## Quick Start

Create starter config:

```bash
udl init
```

Validate config:

```bash
udl validate
```

Run environment and dependency diagnostics:

```bash
udl doctor
```

Dry-run execution plan:

```bash
udl sync --dry-run
```

Run sync:

```bash
udl sync
```

## Command Surface

```text
udl [global flags] <command>

Commands:
  init
  validate
  doctor
  sync
  version
  help
```

Global flags:
- `-c, --config <path>`
- `--json`
- `-q, --quiet`
- `-v, --verbose`
- `--no-color`
- `--no-input`
- `-n, --dry-run`
- `--version`

`sync` flags:
- `--source <id>` (repeatable)
- `--timeout <duration>`
- `--ask-on-existing`
- `--scan-gaps`
- `--no-preflight`
- `--progress <auto|always|never>`
- `--preflight-summary <auto|always|never>`
- `--track-status <names|count|none>`

## Config

Precedence (highest to lowest):
1. flags
2. environment variables
3. project config (`./udl.yaml`)
4. user config (`$XDG_CONFIG_HOME/udl/config.yaml` or `~/.config/udl/config.yaml`)
5. defaults

Supported config env overrides:
- `UDL_CONFIG`
- `UDL_STATE_DIR`
- `UDL_ARCHIVE_FILE`
- `UDL_THREADS`
- `UDL_CONTINUE_ON_ERROR`
- `UDL_COMMAND_TIMEOUT_SECONDS`
- `UDL_SPOTDL_BIN`
- `UDL_SCDL_BIN`
- `UDL_DEEMIX_BIN`
- `UDL_DEEMIX_ARL`
- `UDL_SPOTIFY_CLIENT_ID`
- `UDL_SPOTIFY_CLIENT_SECRET`

`udl` also loads `.env` and `.env.local` from the current working directory at startup.
- `.env.local` is intended for developer-machine overrides (for example `UDL_DEEMIX_BIN=/Users/you/.local/bin/deemix-bambanah`).
- Existing process env vars still win (dotenv files do not override already-set variables).
- Keep secrets out of committed files; `.env.local` is gitignored in this repo.

Example:

```yaml
version: 1
defaults:
  state_dir: "~/.local/state/udl"
  archive_file: "archive.txt"
  threads: 1
  continue_on_error: true
  command_timeout_seconds: 900
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

  # Optional separate SoundCloud free-download flow:
  # - id: "soundcloud-likes-free"
  #   type: "soundcloud"
  #   enabled: false
  #   target_dir: "~/Music/downloaded/sc-likes-free"
  #   url: "https://soundcloud.com/your-user"
  #   state_file: "soundcloud-likes-free.sync.scdl"
  #   adapter:
  #     kind: "scdl-freedl"

  - id: "spotify-groove"
    type: "spotify"
    enabled: true
    target_dir: "~/Music/downloaded/spotify-groove"
    url: "https://open.spotify.com/playlist/replace-me"
    state_file: "spotify-groove.sync.spotify"
    adapter:
      kind: "deemix"
      extra_args: []

  # Optional legacy Spotify source using spotdl:
  # - id: "spotify-groove-legacy"
  #   type: "spotify"
  #   enabled: false
  #   target_dir: "~/Music/downloaded/spotify-groove"
  #   url: "https://open.spotify.com/playlist/replace-me"
  #   state_file: "spotify-groove-legacy.sync.spotify"
  #   adapter:
  #     kind: "spotdl"
  #     extra_args: ["--headless", "--print-errors"]
```

Notes:
- Spotify sources must explicitly set `adapter.kind` (`deemix` or `spotdl`); there is no silent default for Spotify.
- SoundCloud sources support `adapter.kind: scdl` (default stream-rip flow) and `adapter.kind: scdl-freedl` (separate free-download-link flow).
- Recommended Spotify path is `adapter.kind: deemix`; `spotdl` remains available as fallback/legacy.
- Spotify+`deemix` supports the same preflight planning controls as SoundCloud (`break_on_existing`, `ask_on_existing`, `--scan-gaps`, `--no-preflight`) and tracks known Spotify IDs in the source state file.
- Spotify+`deemix` preflight now treats known tracks missing from `target_dir` as `known_gaps` (SCDL-style), so deleted local files are re-planned automatically.
- In default compact mode, Spotify+`deemix` progress is rendered as live per-track/global bars with persistent `[done]/[skip]/[fail]` lines. Raw deemix stack/progress chatter is suppressed; use `--verbose` for raw subprocess output.
- Compact mode now preserves source preflight summary lines by default. Use `--preflight-summary never` to hide them.
- Use `--progress` to control bar rendering (`auto` by TTY, `always`, `never`).
- Use `--track-status` to control persistent per-track lines (`names`, `count`, `none`).
- For Spotify playlists with `--no-preflight`, `udl` still enumerates public playlist tracks and executes deemix per track so metadata cache priming remains active.
- `deemix` binary resolution prefers `UDL_DEEMIX_BIN`, then `deemix` from `PATH`.
- Deezer ARL resolution order is `UDL_DEEMIX_ARL` then macOS Keychain (`service=udl.deemix account=default`). Interactive sync can prompt and store ARL in Keychain.
- Spotify app credential resolution order for deemix conversion is `UDL_SPOTIFY_CLIENT_ID`/`UDL_SPOTIFY_CLIENT_SECRET`, then macOS Keychain (`service=udl.spotify` accounts `client_id` and `client_secret`), then `~/.spotdl/config.json` (`client_id`/`client_secret`).
- For Spotify+`deemix`, `udl` primes deemix's Spotify cache per track (title/artist/album) before each run to avoid known upstream Spotify plugin crash paths.
- For Spotify+`deemix`, `udl` now treats `GWAPIError: Track unavailable on Deezer` as a per-track skip (keeps source running, does not append skipped IDs to state).
- Spotify state entries now persist optional metadata (`title`, `path`) for stronger local-existence detection when Spotify API metadata is unavailable.
- If Spotify Web API playlist preflight is blocked (for example `403`), `udl` falls back to parsing public playlist HTML to enumerate track IDs and keep deterministic planning.
- Upstream `deemix`/`deezer-sdk` transport behavior is security-sensitive (historically includes insecure request paths). Treat Deezer ARL and Spotify app credentials as secrets and run only on trusted networks.
- Use `bin/setup-udl-secrets.sh` to store ARL and optional Spotify credentials in Keychain.
- Legacy Spotify path uses `spotdl` and prefers a managed binary at `~/.venvs/udl-spotdl/bin/spotdl` when present (or `UDL_SPOTDL_BIN` when set), falling back to `spotdl` from `PATH`.
- If `spotdl` reports `Valid user authentication required` and prompts are allowed (TTY, no `--no-input`), `udl` retries once with `--user-auth`.
- For SoundCloud sources, `udl` injects `--yt-dlp-args "--embed-thumbnail --embed-metadata"` automatically when `--yt-dlp-args` is not explicitly provided.
- `udl` also injects a per-source SoundCloud download archive file under `defaults.state_dir` (for example `soundcloud-clean-test.archive.txt`) unless `--download-archive` is explicitly set in custom `--yt-dlp-args`.
- SoundCloud sync uses a state file (`scdl --sync`) and preflight diff by default to estimate remote-vs-local changes before execution.
- SoundCloud sources support two separate adapter flows:
  - `adapter.kind: scdl` (current/default stream-rip flow)
  - `adapter.kind: scdl-freedl` (new free-download-link flow using each track's SoundCloud `FREE DL`/purchase URL)
- `scdl-freedl` keeps deterministic preflight/state/archive behavior but skips tracks that do not expose a free-download link.
- `scdl-freedl` currently downloads only HypeEdit free-DL links (browser handoff opens the gate URL and waits for a completed file in `~/Downloads`). Non-HypeEdit free-DL hosts are skipped.
- `scdl-freedl` tags downloaded files with track metadata and attempts to embed SoundCloud artwork thumbnails into the resulting media file.
- Override watched browser download directory with `UDL_FREEDL_BROWSER_DOWNLOAD_DIR`.
- On macOS, set `UDL_FREEDL_BROWSER_APP` (for example `Helium`) to force a specific browser app for HypeEdit handoff.
- HypeEdit browser handoff now uses idle-timeout behavior: default idle wait is 1 minute (even if source command timeout is higher), and active partial download activity (`.crdownload`, `.download`, `.part`, etc.) keeps the wait alive up to the source max timeout.
- Override idle timeout with `UDL_FREEDL_BROWSER_IDLE_TIMEOUT` (Go duration format, for example `45s` or `90s`).
- Preflight known/gap counts are computed from both sync-state entries and SoundCloud download-archive IDs, which keeps counts accurate across interrupted runs where `scdl --sync` may not flush state.
- SoundCloud preflight is split into explicit stages (`enumerate`, `load-state`, `load-archive`, `local-index`, `plan`) and skips local media scans when there are no archive-only known entries for a source.
- `sync.local_index_cache` enables a persisted local index cache (per source under `defaults.state_dir`) to avoid repeated full target-dir rescans; cache rebuilds on miss, schema mismatch, hash mismatch, or target signature change.
- Default SoundCloud behavior breaks at first existing track; use `--scan-gaps` to scan full remote list and repair gaps. `--ask-on-existing` prompts once per source (TTY only, unless `--no-input`).
- When preflight in break mode finds `planned=0`, `udl` marks the source up-to-date and skips launching `scdl`.
- If a sync is interrupted or a source command fails, `udl` automatically cleans newly created partial artifacts (`*.part`, `*.ytdl`, and `*.scdl.lock` for `scdl`).
- Compact mode progress now derives planned/global totals from structured engine events rather than parsing human log text.

## Exit Codes

- `0` success
- `1` runtime failure
- `2` invalid usage
- `3` invalid config
- `4` missing dependency/auth prerequisite
- `5` partial success (at least one source failed)
- `130` interrupted

## Testing

Run all tests:

```bash
make test
```

## Distribution

- CI test workflow: `.github/workflows/ci.yml`
- Tagged release build workflow: `.github/workflows/release.yml`
- Homebrew formula template: `packaging/homebrew/udl.rb`

## Migration Notes

Dual-run transition is supported:
1. Keep using `bin/update-downloads` while configuring `udl`.
2. Validate parity with `udl sync --dry-run` and then `udl sync`.
3. Switch to `udl` as the default command.
4. Remove legacy script after stable releases.

## License

MIT — see `LICENCE`.

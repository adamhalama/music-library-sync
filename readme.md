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
- v1 model: Go app orchestrating external adapters (`spotdl`, `scdl`)
- Scope: `init`, `validate`, `doctor`, `sync`, `version`

## Requirements

Runtime tools:
- `spotdl`
- `scdl`
- `yt-dlp` (required for SoundCloud preflight diff mode)

Dependency policy:
- `scdl`/`yt-dlp` are managed as strict external dependencies with a supported compatibility matrix enforced by `udl doctor`.
- See `docs/dependency-matrix.md` for supported ranges, upgrade flow, and rollback behavior.
- `spotdl` remains supported but is transitional and outside the strict matrix scope for this phase.

Environment:
- `SCDL_CLIENT_ID` (required for SoundCloud sources)

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

  - id: "spotify-groove"
    type: "spotify"
    enabled: true
    target_dir: "~/Music/downloaded/spotify-groove"
    url: "https://open.spotify.com/playlist/replace-me"
    state_file: "spotify-groove.sync.spotdl"
    adapter:
      kind: "spotdl"
      extra_args: ["--headless", "--print-errors"]
```

Notes:
- Spotify sources use `spotdl` and prefer a managed binary at `~/.venvs/udl-spotdl/bin/spotdl` when present (or `UDL_SPOTDL_BIN` when set), falling back to `spotdl` from `PATH`.
- If Spotify returns `Valid user authentication required` and prompts are allowed (TTY, no `--no-input`), `udl` retries the source once with `spotdl --user-auth`.
- For SoundCloud sources, `udl` injects `--yt-dlp-args "--embed-thumbnail --embed-metadata"` automatically when `--yt-dlp-args` is not explicitly provided.
- `udl` also injects a per-source SoundCloud download archive file under `defaults.state_dir` (for example `soundcloud-clean-test.archive.txt`) unless `--download-archive` is explicitly set in custom `--yt-dlp-args`.
- SoundCloud sync uses a state file (`scdl --sync`) and preflight diff by default to estimate remote-vs-local changes before execution.
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

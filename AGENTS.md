# AGENTS.md — Working Effectively In This Repo

## Project Overview
- `update-downloads` is a Go CLI (`udl`) that orchestrates music sync workflows from source URLs into local libraries with deterministic, source-by-source execution.
- Core runtime stays in Go (config loading/validation, preflight planning, retries, logging, and UX), while v1 download engines remain external adapters (`scdl`, `spotdl`).
- Primary workflows are `init`, `validate`, `doctor`, and `sync`, with YAML config + state/archive files used to track known tracks, detect gaps, and control break/scan behavior.


## Commit & Pull Request Guidelines
- Use short, imperative prefixes for both commit messages and PR titles: `feat:`, `fix:`, `test:`,`menu:`, `settings:`, `docs:`, `chore:`, `refactor:`.
- Keep commit/PR titles concise, present tense, and without a trailing period. Example: `test: cover scdl binary selection fallback`.
- PRs should include a brief summary, linked issue/ticket (if any), terminal output snippet or short clip for CLI UX/output changes, and the exact validation commands run (for example: `go test ./...`, `go vet ./...`, `go test -race ./...`, plus any manual `udl ...` checks).


## Security & Configuration Tips
- Keep GitHub App secrets/private key out of the repo; tokens live in Keychain. 

If you encounter a violation in the repo or in a proposed change, explicitly call it out and propose a safer alternative.
Do not quietly change security-sensitive behavior. Call it out.


## A Note To The Agent

We are building this together. When you learn something non-obvious, add it here so future changes go faster.

- Spotify adapter binary resolution now prefers `UDL_SPOTDL_BIN`, then `~/.venvs/udl-spotdl/bin/spotdl`, then `spotdl` from `PATH`.
- Deemix auth resolution order is `UDL_DEEMIX_ARL` then macOS Keychain (`service=udl.deemix`, `account=default`); prompted ARLs are saved back to Keychain.
- Deemix Spotify conversion credentials resolve from `UDL_SPOTIFY_CLIENT_ID`/`UDL_SPOTIFY_CLIENT_SECRET` first, then macOS Keychain (`service=udl.spotify`, `account=client_id|client_secret`), then `~/.spotdl/config.json`.
- Upstream deemix/deezer-sdk transport behavior remains security-sensitive; keep doctor/docs warnings visible and do not silently suppress them.
- CLI startup now loads `.env` and `.env.local` from the current working directory without overriding already-exported process env vars; use `.env.local` for non-secret local overrides like `UDL_DEEMIX_BIN`.
- `udl` now primes deemix's Spotify plugin cache (`config/spotify/cache.json`) with track metadata before per-track execution; this avoids the upstream Spotify plugin crash path for public track URLs.
- When Spotify Web API playlist preflight fails (for example `403`), `udl` falls back to parsing public `open.spotify.com/playlist/...` HTML to enumerate track IDs and continue deterministic deemix planning.
- For Spotify playlist sources run with `--no-preflight`, `udl` now enumerates tracks from the public playlist page and executes per-track (instead of passing the whole playlist URL once), so deemix cache priming still applies.
- Compact output now normalizes deemix progress chatter (`[Track] Downloading: X%`) into live per-track/global bars and keeps only persistent `[done]/[fail]` lines.
- Spotify deemix preflight now mirrors SCDL gap logic: IDs known in state but missing as local media in `target_dir` are treated as `known_gaps` and get re-planned automatically (including when the folder is emptied).
- Compact mode now keeps Spotify/SoundCloud `preflight:` summary lines visible by default; operators can hide with `--preflight-summary never`.
- Spotify deemix now treats `GWAPIError: Track unavailable on Deezer` as a per-track skip (`[skip] ... (unavailable-on-deezer)`), continues the source run, and does not append skipped IDs to `<source>.sync.spotify`.
- Spotify state file format supports v2 metadata lines (`<id>\ttitle=...\tpath=...`) and remains backward-compatible with v1 ID-only lines; v2 metadata improves local-file detection when Spotify preflight metadata is sparse (for example HTML fallback).
- Shell-based subprocess tests can buffer stdout unexpectedly; for deterministic rate-limit guard tests, emit the trigger line on stderr so `SubprocessRunner` observers see it immediately.

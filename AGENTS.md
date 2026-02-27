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

- As of Spotify Web API changes (Feb 2026), upstream `spotdl` `4.4.3` can fail on playlist metadata (`/playlists/{id}/tracks` 403) and missing artist fields (for example `genres`). A patched build from PR #2610 commit `27f3a0e33174170cbeebbcc0738ceb41a9baf947` works in local validation.
- If Spotify retries run with `--headless`, OAuth stays in manual copy/paste mode; removing `--headless` on the retry allows browser-led auth and is much clearer for interactive `udl sync` flows.
- `bambanah` `deemix-cli@0.1.0` can print a Spotify plugin stack trace (for example `TypeError: Cannot read properties of undefined (reading 'error')`) while still exiting `0`; `udl` must treat this as failure to avoid false-positive source success/state writes.
- For Spotify playlist sources run with `--no-preflight`, `udl` now enumerates tracks from the public playlist page and executes per-track (instead of passing the whole playlist URL once), so deemix cache priming still applies.
- Spotify state file format supports v2 metadata lines (`<id>\ttitle=...\tpath=...`) and remains backward-compatible with v1 ID-only lines; v2 metadata improves local-file detection when Spotify preflight metadata is sparse (for example HTML fallback).
- Shell-based subprocess tests can buffer stdout unexpectedly; for deterministic rate-limit guard tests, emit the trigger line on stderr so `SubprocessRunner` observers see it immediately.
- pyrekordbox `db6.tables.datetime_to_str` drops second precision when `datetime.microsecond == 0` (stores `YYYY-MM-DD HH:MM +00:00`); when writing `djmdContent.created_at` for deterministic Date Added ordering, always use non-zero microseconds (for example `.900000`) so seconds survive serialization.

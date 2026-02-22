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

- For Spotify sources, `spotdl` shared/default client credentials can get globally throttled (`Retry after: 86400`) before any download starts. Prefer user-owned Spotify app credentials in `~/.spotdl/config.json`.
- As of Spotify Web API changes (Feb 2026), upstream `spotdl` `4.4.3` can fail on playlist metadata (`/playlists/{id}/tracks` 403) and missing artist fields (for example `genres`). A patched build from PR #2610 commit `27f3a0e33174170cbeebbcc0738ceb41a9baf947` works in local validation.
- `udl` now retries Spotify sources once with `--user-auth` when `spotdl` reports `Valid user authentication required` (TTY only; disabled by `--no-input`).
- Spotify adapter binary resolution now prefers `UDL_SPOTDL_BIN`, then `~/.venvs/udl-spotdl/bin/spotdl`, then `spotdl` from `PATH`.
- If Spotify retries run with `--headless`, OAuth stays in manual copy/paste mode; removing `--headless` on the retry allows browser-led auth and is much clearer for interactive `udl sync` flows.
- `udl` now fails fast when SpotDL reports a long Spotify API retry window (`rate/request limit ... Retry will occur after ...`) instead of waiting for that full backoff period.
- Compact mode now normalizes SpotDL progress to `[done]/[skip]` lines and suppresses noisy SpotDL traceback/chatter by default; use `--verbose` for raw subprocess output.
- Shell-based subprocess tests can buffer stdout unexpectedly; for deterministic rate-limit guard tests, emit the trigger line on stderr so `SubprocessRunner` observers see it immediately.

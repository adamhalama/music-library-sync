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
- Public macOS releases now ship `udl` only; Homebrew installs `scdl` and `yt-dlp` as external dependencies, and tarball installs rely on `doctor` / TUI repair guidance when those tools are missing.
- Standalone playlist snapshots are cache-first and never refresh on open; Apple Music is read only after an explicit refresh, and failed or canceled refreshes must preserve the previous valid snapshot.
- Masked credential editors must keep the existing secret outside the editable buffer. Preloading a masked buffer causes a pasted replacement to append to the old secret (an ARL becomes two identical 192-character halves) while the UI makes the duplication invisible.
- Interactive plan rebuilds cross the engine/TUI channel boundary; when a selector changes a per-source option such as the Spotify plan window, update both the engine request and the TUI interaction's copied per-source state before rendering the rebuilt rows.
- Rekordbox process detection matches process names containing `rekordbox`; when building a temporary validation binary, name it `udl` or another neutral name so the safety guard does not mistake the validator for the Rekordbox application.
- `VerifyPlanChecksum` re-marshals the parsed `playlistsync.Plan`, so any new plan field must be `omitempty` (or the plan version bumped): an always-emitted field changes the JSON of plans written by older builds and makes them fail with an opaque "plan checksum mismatch" instead of a regenerate hint.
- Go nil slices in valid agent responses encode as JSON `null`; Swift collection DTOs for optional/empty protocol fields must use `decodeIfPresent(... ) ?? []` rather than requiring an array.
- Unexpected backend EOF can race the process termination handler. Native recovery UI must distinguish deliberate shutdown by cancellation of the connection observer, not by checking whether `AgentProcess.state` is still `.running`.
- Destructive-path Rekordbox acceptance is opt-in: set `UDL_REKORDBOX_ACCEPTANCE_PYTHON` to an absolute pyrekordbox-capable Python and run `TestRekordboxIsolatedApplyAndRestore`; it copies the checked-in sandbox DB to `t.TempDir`, applies there, and verifies exact-backup restoration byte-for-byte.
- First-run onboarding is suppressed when either an enabled standalone Free DL job or a valid Rekordbox configuration exists, even if the main config has no sync sources. Tests for the no-source route must isolate user-level standalone configs explicitly.
- The Free DL hardening smoke's browser-launch-failure leg requires a track whose SoundCloud purchase URL uses a supported gate such as Hypeddit; a generally free-to-use track may still classify as `unsupported-free-download-host` and never reach the browser launcher. Promotion logs use `library <= free source` direction.
- macOS app distribution intentionally uses only free ad-hoc signing (`codesign --sign -`), never Developer ID or notarization. Release docs must call the app unidentified, publish a checksum, and describe macOS's one-time Privacy & Security → Open Anyway flow without claiming Apple verification.
- On a Mac with Command Line Tools but no full Xcode, use `make app-dev` (or `make app-dev-open`): `packaging/dev/build_macos_app.sh` directly compiles the current-architecture Swift sources, embeds `bin/udl`, materializes the `UDL Dev` plist (`com.jaa.udl.dev`), and ad-hoc signs the bundle.

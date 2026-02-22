# AGENTS.md — Working Effectively In This Repo

## Project Overview
- `update-downloads` is a Go CLI (`udl`) that orchestrates music sync workflows from source URLs into local libraries with deterministic, source-by-source execution.
- Core runtime stays in Go (config loading/validation, preflight planning, retries, logging, and UX), while v1 download engines remain external adapters (`scdl`, `spotdl`).
- Primary workflows are `init`, `validate`, `doctor`, and `sync`, with YAML config + state/archive files used to track known tracks, detect gaps, and control break/scan behavior.


## Commit & Pull Request Guidelines
- Use short, imperative prefixes for both commit messages and PR titles: `feat:`, `fix:`, `test:`, `docs:`, `chore:`, `refactor:`.
- Keep commit/PR titles concise, present tense, and without a trailing period. Example: `test: cover scdl binary selection fallback`.
- PRs should include a brief summary, linked issue/ticket (if any), terminal output snippet or short clip for CLI UX/output changes, and the exact validation commands run (for example: `go test ./...`, `go vet ./...`, `go test -race ./...`, plus any manual `udl ...` checks).


## Security & Configuration Tips
- Keep GitHub App secrets/private key out of the repo; tokens live in Keychain. 

If you encounter a violation in the repo or in a proposed change, explicitly call it out and propose a safer alternative.
Do not quietly change security-sensitive behavior. Call it out.


## A Note To The Agent

We are building this together. When you learn something non-obvious, add it here so future changes go faster.

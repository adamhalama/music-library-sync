# Homebrew Release Flow

This repo now treats the public macOS release as:

- GitHub Release assets for `darwin-amd64` and `darwin-arm64`
- tarballs that contain `udl` only
- a rendered Homebrew formula that installs `udl` and depends on external `scdl` and `yt-dlp` formulas

## Required GitHub Variables

- `HOMEBREW_TAP_REPO` if the workflow should push `Formula/udl.rb` into a tap repo

## Required GitHub Secret

- `HOMEBREW_TAP_TOKEN` for tap updates

## Tag Release Behavior

On `v*` tags the workflow:

1. Runs `go test ./...`
2. Runs `go vet ./...`
3. Builds native macOS tarballs on Intel and Apple Silicon runners
4. Verifies the tarball with:
   - `udl version`
   - `udl doctor` showing missing external SoundCloud tools in the isolated smoke environment
5. Produces `SHA256SUMS`
6. Renders a Homebrew formula with exact artifact URLs and checksums
7. Publishes release assets to GitHub Releases
8. Updates the tap formula when the tap repo configuration is present

Current macOS runner labels:

- Intel: `macos-15-intel`
- Apple Silicon: `macos-15`

## Manual Validation Before Announcement

- Install from the tap on a clean Intel Mac
- Install from the tap on a clean Apple Silicon Mac
- Run:
  - `udl version`
  - `udl doctor`
  - `udl tui`
  - `udl sync --dry-run`
- Confirm `Get Started` auto-opens when there is no config
- Confirm Homebrew installed `scdl` and `yt-dlp` as formula dependencies
- Confirm tarball installs show explicit repair guidance when `scdl` or `yt-dlp` are missing

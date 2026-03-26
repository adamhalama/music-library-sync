# Homebrew Release Flow

This repo now treats the public macOS release as:

- GitHub Release assets for `darwin-amd64` and `darwin-arm64`
- tarballs that contain:
  - `udl`
  - bundled `scdl`
  - bundled `yt-dlp`
- a rendered Homebrew formula that installs the bundle under `libexec` and wraps `udl` with `PATH` pointing at the bundled tools

Current pinned bundled sources:

- `scdl` `3.0.1` from `adamhalama/udl-bundled-tools` release assets
- `yt-dlp` `2026.02.21` from the official upstream GitHub release asset `yt-dlp_macos`

## Security Gate

Bundling third-party binaries is security-sensitive and license-sensitive.

- The release workflow is blocked unless `ALLOW_BUNDLED_TOOL_REDISTRIBUTION=true` is set in GitHub Actions variables.
- The workflow also requires per-arch URLs and SHA256 values for the bundled `scdl` and `yt-dlp` executables.
- If redistribution or provenance is not approved, keep the release blocked. Do not silently switch to shipping unreviewed binaries.

## Required GitHub Variables

- `ALLOW_BUNDLED_TOOL_REDISTRIBUTION=true`
- `BUNDLED_SCDL_URL_DARWIN_AMD64`
- `BUNDLED_SCDL_SHA256_DARWIN_AMD64`
- `BUNDLED_YTDLP_URL_DARWIN_AMD64`
- `BUNDLED_YTDLP_SHA256_DARWIN_AMD64`
- `BUNDLED_SCDL_URL_DARWIN_ARM64`
- `BUNDLED_SCDL_SHA256_DARWIN_ARM64`
- `BUNDLED_YTDLP_URL_DARWIN_ARM64`
- `BUNDLED_YTDLP_SHA256_DARWIN_ARM64`
- `HOMEBREW_TAP_REPO` if the workflow should push `Formula/udl.rb` into a tap repo

## Required GitHub Secret

- `HOMEBREW_TAP_TOKEN` for tap updates

## Tag Release Behavior

On `v*` tags the workflow:

1. Runs `go test ./...`
2. Runs `go vet ./...`
3. Builds native macOS bundles on Intel and Apple Silicon runners
4. Verifies the bundle with:
   - `udl version`
   - `udl doctor`
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
- Confirm SoundCloud uses the bundled tools without extra PATH setup

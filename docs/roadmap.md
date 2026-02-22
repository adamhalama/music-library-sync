# UDL Roadmap

## Track A: SoundCloud sync redesign (implemented)
- [x] Add `udl sync` flags: `--ask-on-existing`, `--scan-gaps`, `--no-preflight`.
- [x] Add SoundCloud sync policy config keys: `sources[].sync.break_on_existing`, `sources[].sync.ask_on_existing`.
- [x] Add SoundCloud state file support via `sources[].state_file` with default `${source.id}.sync.scdl`.
- [x] Implement SoundCloud preflight using `yt-dlp --flat-playlist`.
- [x] Compute preflight diff metrics (remote, known, archive gaps, known gaps, first existing, planned).
- [x] Emit `source_preflight` events in human and JSON output.
- [x] Execute SoundCloud via `scdl --sync <state-file>` with break-mode and gap-scan behavior.
- [x] In break mode, plan downloads up to first local existing track and use temporary archive/state filtering so planned known gaps can redownload before break.

## Track B: CLI v1 release readiness (current priority)

### Phase 4: Release integrity and publish gates (P1)
- [ ] Add checksum generation and upload (`SHA256SUMS`) in `/Users/jaa/dev/utils/update-downloads/.github/workflows/release.yml`.
- [ ] Gate release publish on verification steps in release workflow (`go test ./...`, `go vet ./...`).
- [ ] Add a pre-publish smoke step for built artifacts (at least `udl version` per target binary).

### Phase 5: Packaging readiness (P1)
- [ ] Replace placeholder SHA values in `/Users/jaa/dev/utils/update-downloads/packaging/homebrew/udl.rb`.
- [ ] Define formula update flow per release tag (manual checklist or scripted step).
- [ ] Validate formula install/test end-to-end from release artifacts before announcing.

### Phase 6: CLI UX contract cleanup (P2)
- [ ] Implement `--no-color` behavior or remove the flag from command surface.
- [ ] Refine `doctor` behavior for zero configured sources to avoid misleading hard failures on initial setup.
- [ ] Document doctor exit-code expectations for CI/automation consumers.

### Phase 7: Hardening and hygiene (P2/P3)
- [ ] Add integration coverage for key SoundCloud preflight + prompt branches in CI.
- [ ] Decide whether personal source URLs remain in tracked fixtures; replace with generic test data if needed.
- [ ] Add a short release checklist doc to keep tags reproducible and low-risk.

### Phase 8: Adapter dependency packaging strategy (P1/P2)
- [ ] Evaluate practical delivery options for `scdl`/`yt-dlp` pinning:
  - bundled per-platform toolchain inside release assets
  - first-run installer into a managed tools directory
  - strict external dependency contract with tested version matrix
- [ ] Choose one default strategy and document upgrade/rollback behavior.
- [ ] Add `udl doctor` compatibility checks against the pinned/supported matrix.
- [ ] Decide Homebrew behavior: depend on external formulas vs install/use managed adapter binaries.

### Phase 9: Preflight performance and code cleanup (P2, after archive behavior freeze)
- [ ] Add benchmarks for preflight on realistic libraries (1k/5k/10k tracks) covering:
  - remote list parse time
  - archive/state parse time
  - local directory scan time
  - full preflight planning time
- [ ] Split preflight pipeline into explicit stages (`enumerate`, `load-state`, `load-archive`, `local-index`, `plan`) with narrow data contracts to reduce coupling in `/Users/jaa/dev/utils/update-downloads/internal/engine`.
- [ ] Avoid unnecessary local media scans when no archive-only known entries exist for a source.
- [ ] Introduce optional persisted local index (source-level cache) to avoid full target-dir rescans on every run; rebuild automatically on cache miss/invalid hash.
- [ ] Add deterministic perf regression guardrails in CI (benchmark thresholds or trend checks).

## Finalization acceptance checklist
1. `go test ./...`
2. `go test -race ./...`
3. `go vet ./...`
4. `go run ./cmd/udl validate --config /Users/jaa/dev/utils/update-downloads/testdata/current-bash-equivalent.yaml`
5. `go run ./cmd/udl doctor --config /Users/jaa/dev/utils/update-downloads/testdata/current-bash-equivalent.yaml --json`
6. Tag build produces binaries plus checksums and Homebrew formula values are updated for that exact tag.

## Open risks
- Upstream behavior drift in `scdl` and `yt-dlp` can break preflight parsing and scan semantics.
- Release artifacts without integrity metadata reduce trust and make external packaging brittle.
- Prompt-based flows depend on TTY detection and must stay deterministic in non-interactive contexts.

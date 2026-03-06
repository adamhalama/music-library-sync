### Three-Stage Plan: Seam Refactor, `sync --plan`, Full Normalization

**Summary**
Implement in three ordered stages:
1. Create a small structural seam with no UX/output changes.
2. Build `udl sync --plan` on top of that seam (scdl first, modular for more sources).
3. Future pass: complete adapter log normalization and unify renderer inputs.

Key existing touchpoints:
- [internal/engine/syncer.go](/Users/jaa/dev/utils/update-downloads/internal/engine/syncer.go)
- [internal/cli/sync.go](/Users/jaa/dev/utils/update-downloads/internal/cli/sync.go)
- [internal/output/compact_writer.go](/Users/jaa/dev/utils/update-downloads/internal/output/compact_writer.go)

---

### Stage 1 — Pre-refactor seam (small, no UX change)

**Objective**
Reduce coupling in `Sync()` and establish parser/event seams without changing current command surface, human output, JSON output shape, or compact behavior.

**Implementation plan**
1. Split source execution orchestration inside `syncer.go` into explicit flow methods:
- `runSource(...)` dispatcher by `(source.Type, source.Adapter.Kind)`.
- `runSoundCloudSCDL(...)`.
- `runSoundCloudFreeDL(...)` (wrap existing free-DL function, unchanged behavior).
- `runSpotifyDeemix(...)`.
- `runGenericAdapter(...)` (current fallback path, including spotdl execution behavior).
- Keep counter updates (`Attempted/Succeeded/Failed/Skipped/DependencyFailures`) centralized in one post-flow reducer to avoid divergence.

2. Introduce internal progress model seam (engine-internal only, not output-facing yet):
- Add `internal/engine/progress` package with:
  - `TrackEventKind`: `track_started`, `track_progress`, `track_done`, `track_skip`, `track_fail`.
  - `TrackEvent` payload: `source_id`, `adapter_kind`, `track_id`, `track_name`, `index`, `total`, `percent`, `reason`.
- Add `ProgressSink` interface to engine flow context.
- Default sink is no-op; no emitter wiring in this stage.

3. Add adapter parser seam interface (not fully adopted yet):
- Add `internal/engine/adapterlog` with:
  - `Parser` interface (`OnStdoutLine`, `OnStderrLine`, `Flush` -> `[]TrackEvent`).
  - Registry keyed by adapter kind.
- Implement only stub parsers returning no events; no behavior changes.

4. Add runner hook seam for line observers:
- Extend `SubprocessRunner` internals to accept optional observers (stdout/stderr line callbacks) via `ExecSpec` or internal run options.
- Keep current behavior if no observers are attached.
- Preserve current rate-limit observer behavior exactly as-is.

5. Keep compact writer unchanged functionally:
- No removal of regex suppression logic.
- No new public events.
- No output formatting changes.

**Non-functional constraints**
- No flag changes.
- No message text changes.
- No event name changes in `output.Event`.
- No config schema changes.

**Stage 1 acceptance criteria**
1. `go test ./...` passes unchanged.
2. Snapshot-sensitive tests for compact output remain green.
3. `udl sync` behavior parity on existing smoke scripts.

---

### Stage 2 — Implement `sync --plan` (on seam)

**Objective**
Add interactive source-check + local-compare + track-toggle workflow for scdl, with modular architecture to add other sources later.

**CLI and UX contract**
1. New sync flags:
- `--plan` (bool, default `false`).
- `--plan-limit <int>` (default `10`; `0` means unlimited).
2. Validation rules:
- `--plan-limit` requires `--plan`.
- `--plan-limit < 0` is invalid usage (exit `2`).
- `--plan` is incompatible with `--json`, `--no-input`, non-TTY stdin/stdout (exit `2` with actionable message).
3. Multi-source UX:
- One full-screen selector per source, in deterministic source order.
4. Unsupported sources in `--plan` mode:
- Non-`adapter.kind=scdl` sources are skipped with warning; run continues.

**Engine design for modularity**
1. Add planner abstraction:
- `PlanProvider` interface per adapter/source family.
- v1 implementation: `SCDLPlanProvider`.
- Future providers can be added without changing sync command semantics.
2. Add plan data model:
- `PlanRow`: `index`, `remote_id`, `title`, `status`, `toggleable`, `selected_by_default`.
- Status values: `already_downloaded`, `missing_new`, `missing_known_gap`.
3. Add interactive selector callback in `SyncOptions`:
- Engine asks CLI for selected row indices; engine remains UI-framework-agnostic.

**Selector behavior (full-screen TUI)**
1. Dependency: Bubble Tea ecosystem (`tea` + `bubbles`) in CLI layer.
2. Controls:
- Up/down or `j/k` navigate.
- `space` toggle row if toggleable.
- `a` select all toggleable.
- `n` clear all toggleable.
- `enter` confirm.
- `q`/`esc` cancel.
3. Rules:
- `already_downloaded` rows are visible but read-only.
- Default selection includes all toggleable rows.
- Cancel returns interrupted flow (exit `130`) with no state writes.

**Plan-limit behavior**
1. Per-source cap.
2. Limit applied at fetch time for scdl preflight enumeration.
3. `--plan-limit 0` fetches full source list (unlimited).
4. Items beyond the cap are not fetched and not eligible in that run.

**Execution behavior after selection**
1. Apply immediately in same command run (“run selected now”).
2. For selected rows:
- Use managed yt-dlp playlist selectors (`--playlist-items`) tied to selected indices.
- Keep temp state/archive filtering for selected known-gap redownloads only.
3. If no rows selected for a source:
- Emit source finished no-op and skip adapter launch.
4. `--dry-run --plan`:
- Runs preflight + selector + final selection summary.
- No adapter execution and no state/archive writes.

**Stage 2 acceptance criteria**
1. New CLI tests for flag validation and interactive preconditions.
2. Engine unit tests for:
- row classification,
- default selection,
- selected-index to yt-dlp selector mapping,
- known-gap selected redownload filtering,
- empty selection no-op.
3. Integration tests for multi-source sequential selectors and unsupported-source skip warnings.
4. `go test ./...` and smoke sanity for `sync --plan` and normal `sync` parity.

---

### Stage 3 — Future full log normalization follow-up (not in this pass)

**Objective**
Replace regex-heavy compact writer parsing with adapter-specific parser strategies emitting normalized progress events; make compact/human/json consume a shared normalized stream.

**Implementation plan**
1. Parser strategy rollout:
- Implement concrete parsers for `scdl`, `deemix`, `spotdl` under `internal/engine/adapterlog`.
- Parsers emit normalized `TrackEvent`s from subprocess lines.
- Keep parser unit fixtures per adapter log corpus.

2. Event bridge:
- Map `TrackEvent`s to additive public `output.Event` names:
  - `track_started`, `track_progress`, `track_done`, `track_skip`, `track_fail`.
- Preserve existing current events (`source_*`, `sync_*`) for compatibility.
- Keep human-readable `Message` in each new event for backward log readability.

3. Renderer convergence:
- Compact writer consumes structured track events first.
- Existing regex/text parsing remains as fallback behind compatibility guard.
- After one release cycle with parity tests, remove fallback parser paths.

4. Compatibility/deprecation policy:
- Treat old regex message parsing as deprecated internals.
- Do not remove old event names in same release as new events.
- Document JSON event additions and backward compatibility expectations.

**Stage 3 acceptance criteria**
1. Compact writer no longer depends on backend raw log regex for primary track lifecycle.
2. Parser contract tests cover known noisy/degraded upstream outputs for all three adapters.
3. Existing machine consumers of JSON remain functional (additive event evolution only).
4. Regression suite verifies output parity for representative scdl/deemix/spotdl runs.

---

### Cross-stage test matrix

1. Functional:
- `sync` default, `sync --dry-run`, `sync --plan`, `sync --plan --dry-run`.
2. UX:
- interactive mode, cancel path, no-op selection, multi-source sequencing.
3. Compatibility:
- existing compact output tests stay green through Stage 2.
4. Reliability:
- interruption, timeout, dependency missing, partial success, continue-on-error.
5. Validation gates each stage:
- `go test ./...`
- `go vet ./...`
- targeted manual `udl sync` checks on scdl + deemix fixtures.

---

### Assumptions and defaults

1. `sync --plan` v1 scope is `scdl` only.
2. Plan mode always interactive; no non-interactive selector fallback in this pass.
3. `--plan-limit` default is `10`, and `0` is unlimited.
4. Existing output/event contracts are frozen for Stage 1 and Stage 2 except additive warning lines and new flags.

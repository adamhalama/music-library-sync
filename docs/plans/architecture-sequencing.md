### Architecture Sequencing for `sync --plan` vs Refactor

**Summary**
- Keep momentum: do a targeted refactor first, then implement `sync --plan`, then do full adapter log normalization.
- Full normalization is valuable, but blocking `sync --plan` on it is unnecessary.

**Current Architecture Read**
- Core orchestration is concentrated in one large function ([syncer.go](/Users/jaa/dev/utils/update-downloads/internal/engine/syncer.go):67), which makes feature additions expensive and brittle.
- Output normalization is still largely regex + suppression logic in the writer ([compact_writer.go](/Users/jaa/dev/utils/update-downloads/internal/output/compact_writer.go):201), even though a state machine exists.
- Event contracts are coarse-grained (`sync/source` lifecycle only) ([event.go](/Users/jaa/dev/utils/update-downloads/internal/output/event.go):15), so renderers still infer track semantics from text.
- Test baseline is healthy (`go test ./...` passes), so this is a good moment for controlled structural cleanup.

**Decision**
- Do **not** wait for full “adapter log normalization architecture” before `sync --plan`.
- Do a **minimal structural pass first** (behavior-preserving), then ship `sync --plan`.

**Execution Order**
1. **Pre-refactor seam (small, no UX change)**
- Extract adapter execution branches from `Sync()` into per-adapter flow units (SoundCloud/scdl, Spotify/deemix, Spotify/spotdl, scdl-freedl).
- Introduce internal normalized progress event types for track lifecycle (`track_started/progress/done/skip/fail`) but keep existing external `output.Event` unchanged.
- Keep current compact regex parser as fallback to preserve behavior while new structured events are phased in.

2. **Implement `sync --plan`**
- Build on extracted SoundCloud flow and preflight pipeline; avoid touching compact parsing.
- Add interactive planning/select logic and apply selection only for `scdl` first.
- Keep other adapters skipped/warned in plan mode as scoped v1 behavior.

3. **Full log normalization follow-up**
- Move backend-specific parsing from `compact_writer` into adapter parser strategies.
- Emit normalized track events from parser layer.
- Make compact/human/json consume shared normalized events; retire message-regex dependence gradually.

**When `sync --plan` may be unnecessary**
- If your primary usage remains unattended full sync runs and not selective interactive pulls, defer `sync --plan`.
- If your pain today is output fragility/noise drift, prioritize Step 3 earlier.
- Otherwise, `sync --plan` is high user-value and worth doing after Step 1.


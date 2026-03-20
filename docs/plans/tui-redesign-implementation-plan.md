# UDL TUI Redesign Plan

## Summary

Rebuild `udl tui` in place as a shared shell-based Bubble Tea UI, using the v4 HTML concept as the visual reference and prioritizing interactive sync first. The redesign will land in stages, with shell/layout consistency across all workflows early, and full behavior parity added progressively. Interactive sync gets the highest-fidelity implementation; standard sync and the other workflows adopt the same shell first and richer bodies later.

Key implementation defaults:
- `udl tui` is replaced in place as stages land.
- Delivery is shell-first.
- Standard sync uses the same shell with a simpler runtime-focused table/list in the first redesign pass.
- Keyboard-first only in this redesign; no mouse support in v1.
- Any visible control that is not yet functional must render as disabled/dimmed, not as a dead affordance.

## Implementation Changes

### 1. Refactor the TUI into a shared shell architecture

- Split the current monolithic TUI model in `internal/cli/tui.go` into same-package screen models plus one shared shell renderer; keep the Cobra entrypoint unchanged.
- Introduce a shell contract for all workflows:
  - screen identity and title
  - sidebar state
  - topbar badges
  - command summary bar
  - shortcut bar
  - main body renderer
  - footer summary
  - modal/prompt layer
  - `esc`/back eligibility
- The shell owns layout, theme tokens, responsive breakpoints, and modal overlay rendering. Individual screens only provide screen state and body content.
- Make the shell the only place that renders the outer frame, titlebar, sidebar, shortcuts, run banner, activity panel, and footer.
- Keep all workflows in the current `cli` package; do not create a new package boundary for the redesign.

### 2. Define the reusable UI state model

- Add internal shell/view-model types:
  - `tuiShellState`
  - `tuiSidebarSection`, `tuiSidebarItem`
  - `tuiBadge`
  - `tuiShortcut`
  - `tuiBanner`
  - `tuiFooterStat`
  - `tuiModalState`
- Add sync-specific view-model types:
  - `tuiStatusFilter` with `all`, `pending`, `done`, `failed`, `skipped`
  - `tuiTrackRowState` with stable row fields: source id, source label, remote id, title, index, selectable, selected, status, status label, failure detail
  - `tuiActivityEntry` with timestamp, level, message, source id
  - `tuiSyncRunTracker` that consumes `output.Event` and derives:
    - current source
    - current row status
    - aggregate counts
    - current banner state
    - bounded activity log
    - elapsed runtime
- Keep `output.StructuredProgressTracker` as-is for aggregate progress bars; add the row/activity tracker in the CLI layer rather than changing engine contracts first.

### 3. Stage 1: ship the shell and general workflow structure

- Replace the current full-screen text view with the new Carbon-style shell:
  - titlebar and app label
  - left sidebar navigation
  - top badges
  - command summary bar
  - shortcut bar
  - footer stats
- Convert the current main menu into the default shell landing state:
  - sidebar becomes the main workflow navigation
  - main panel shows the selected workflow summary and available actions
  - `interactive sync` remains the default selected item
- Wrap `doctor`, `validate`, and `init` in the new shell immediately:
  - their bodies can remain simple text/panel content initially
  - prompts move into the shared modal renderer
- Introduce one responsive fallback:
  - width `>= 110`: full two-column shell with sidebar
  - width `< 110`: compact single-column shell with sidebar content collapsed into a top navigation strip and reduced command/footer detail
- Do not introduce new behavior in this stage beyond shell navigation and visual structure.

### 4. Stage 2: redesign interactive sync selection view

- Make interactive sync the first fully redesigned workflow body.
- Pre-run body uses the HTML concept structure:
  - source list in sidebar
  - filter list in sidebar
  - topbar badges for run state, dry-run, plan limit, timeout
  - command bar showing `udl sync --plan` context
  - shortcuts bar mirroring active keys
  - main table with rows, cursor highlight, locked rows, selection marker, status pill, remote id
  - activity panel at the bottom, collapsed/expanded with `l`
  - footer summary with selected count, skipped count, progress stub
- Populate the pre-run table from `engine.PlanRow` after plan/preflight selection is requested; preserve current selector semantics.
- Current prompt flows become modals inside the shell:
  - confirm prompt
  - input prompt
  - plan selection modal/table when needed
- Preserve existing keyboard behavior:
  - source move/toggle
  - row move/toggle
  - select all / clear all
  - dry-run, timeout, plan limit controls
  - enter to run
  - cancel via `x` or `ctrl+c`
- In this stage, visual parity takes priority over perfect runtime row transitions. If needed, the initial runtime state may degrade to a partially live table plus activity panel, but the pre-run/selection experience should match the concept closely.

### 5. Stage 3: interactive sync running and done states

- Add persistent row-state updates for interactive sync during the run:
  - selected rows start as pending
  - current row becomes active/downloading
  - completed rows move to done
  - already-downloaded rows stay skipped/locked
  - failures mark the row failed and retain short failure detail
- Derive row updates from existing `output.Event` data first:
  - match by source id plus track index when available
  - fall back to track id/name if index is unavailable
- Use the activity panel as the primary live event surface:
  - bounded log length
  - timestamps from `output.Event.Timestamp`
  - collapse/expand with `l`
- Running-state footer uses live aggregate counts and elapsed time.
- Done-state topbar/footer change to complete or complete-with-errors styling.
- Keep the existing failure detail content available in a shell banner or failure panel; do not drop stdout/stderr tail diagnostics.

### 6. Stage 4: redesign standard sync in the same shell

- Standard sync adopts the same shell, but does not try to force full pre-run plan-table parity.
- First-pass standard sync body:
  - source sidebar and filter/sidebar shell stay the same
  - topbar badges show dry-run, timeout, ask-on-existing, scan-gaps, no-preflight
  - main body uses a simpler runtime-focused list/table:
    - current source
    - current track
    - recent outcomes
    - per-source summary rows if per-track rows are not known up front
  - activity panel and footer remain consistent with interactive sync
- If current events are insufficient for a richer standard-sync table, keep this body intentionally simpler rather than inventing speculative row state.
- Do not change standard sync engine behavior in this redesign stage.

### 7. Stage 5: finish the remaining workflows and documentation

- Rework `init`, `doctor`, and `validate` bodies to feel native to the new shell:
  - `init`: guided modal/panel flow
  - `doctor`: check list with severity styling
  - `validate`: success/failure panel with actionable details
- Update `docs/tui.md` to match the redesigned shell, screen structure, keybindings, and known limitations.
- If later needed, add an optional final stage for additive engine/output event enrichment, but only after the shell and interactive sync redesign are stable.

## Public APIs / Interfaces / Types

- Public CLI surface:
  - No new command or flag is introduced in the redesign plan.
  - `udl tui` remains the single entrypoint.
- Internal interfaces and types to add:
  - shared shell screen contract used by all workflows
  - sync row/activity tracker types in the CLI layer
  - modal state shared by sync and init
- Engine/output contracts:
  - No required breaking changes.
  - Any later event enrichment must be additive only.

## Test Plan

- Expand `internal/cli/tui_test.go` to cover:
  - shell navigation and workflow switching
  - `esc` behavior with and without active modals
  - responsive layout mode switching based on width
  - interactive sync sidebar source toggles and filter selection
  - interactive sync row selection, locked row behavior, selected counts
  - plan limit, timeout, and dry-run badge state changes
  - activity panel collapse/expand
  - running-state transitions from emitted `output.Event` sequences
  - done-state and failed-state banners/footer stats
  - standard sync shell rendering with simpler runtime body
  - modal prompt rendering for sync and init
- Add focused tracker tests for `tuiSyncRunTracker`:
  - event-to-row mapping
  - aggregate counts
  - activity log truncation/bounding
  - elapsed time formatting
- Keep `go test ./...` green throughout.
- Update existing view assertions so they validate the new shell structure rather than the old plain text blocks.

## Assumptions and Defaults

- The HTML concept is treated as the visual target, not a strict 1:1 spec.
- Interactive sync gets the closest fidelity first.
- The redesign prioritizes consistency, readability, and performance over exact visual mimicry.
- Keyboard-first interaction remains the source of truth; clickable HTML-style affordances are represented as styled terminal elements only.
- Narrow-terminal fallback is mandatory; do not attempt to force the full desktop shell into small widths.
- Incomplete future behavior is acceptable only if the related UI element is visibly disabled and not advertised as active.

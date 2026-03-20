# TUI Redesign Feasibility Assessment

## Stack

- **Language**: Go 1.22+
- **TUI framework**: Bubble Tea v1.1.1 (Charmbracelet, Elm model-update-view)
- **Styling**: lipgloss v0.13.0
- **Components**: bubbles v0.20.0 (key bindings, viewport, spinner, etc.)
- **Main TUI file**: `internal/cli/tui.go` (1861 lines)
- **Plan selector component**: `internal/cli/plan_selector.go`

---

## Visual fidelity estimate: ~80%

The structure, color semantics, information layout, and keyboard vocabulary of the v4 prototype map cleanly to what Bubble Tea + lipgloss can produce. The compromises are all cosmetic, not structural.

---

## What maps 1:1

### Two-column layout (sidebar + main)
`lipgloss.JoinHorizontal` takes two strings and places them side by side. The sidebar is a fixed-width styled string; the main panel fills the rest. This is the standard lipgloss pattern and works exactly as designed.

### Colored badges in topbar
`lipgloss.NewStyle().Background(...).Foreground(...).Padding(0, 1).Render("⬤ RUNNING")` produces a filled color block. No rounded corners (terminals don't support `border-radius`), but filled rectangles read the same way in practice — the color + text carries the meaning, not the shape.

### Two-line command bar
Plain styled text rows. Trivial.

### Keyboard shortcut bar
Same — a horizontal row of styled key labels. The existing code already renders these as plain strings; just needs lipgloss wrapping.

### Status pills in the track table
Same trade-off as badges: filled color blocks, not rounded. The contrast between `already_downloaded` (dim background), `pending` (blue), `done` (green), `downloading` (amber), and `failed` (red) reads clearly.

### Track table with fixed columns
`plan_selector.go` already does this manually. The refactor is adding lipgloss column-width management and the outcome indicator column (`✓` / `–` / `↓` / `✕`).

### Activity log panel
The existing code already maintains an event line buffer (12 lines). Converting it to a fixed-height panel using `bubbles/viewport` is straightforward. Timestamps already exist in the event system.

### Pulsing/spinner for the active downloading row
`bubbles/spinner` provides the character cycling. A pulsing RUNNING badge can toggle between two states on a `tea.Tick`. Not as smooth as CSS animation but effective — this is standard terminal UX.

### Outcome indicator column (replaces checkboxes during run)
Unicode chars (`✓`, `–`, `↓`, `✕`) — these just work in any modern terminal.

### Footer stats bar
Simple horizontal row. Already exists in simpler form.

### Progress bar in sidebar / footer
`bubbles/progress` or manual `█░` fill. The existing renderer already does ASCII progress bars.

---

## Compromises vs. the HTML prototype

| HTML feature | Terminal reality |
|---|---|
| Rounded pill corners (`border-radius: 10px`) | Filled rectangle — barely noticeable in practice |
| CSS transitions / smooth state changes | Instant full-frame redraws — no cross-fade |
| Pixel-precise spacing and density | Cell grid: each char = 1 unit |
| Scroll bar chrome | No visible scrollbar, or fake with `│` block indicator |
| `overflow: hidden` with text fade | Hard truncation with `…` |
| Sidebar hover states | Cursor-based navigation only (j/k) |
| `border-left: 2px` accent on active rows | A single `▌` or colored `│` char — 1 cell wide, same visual intent |
| Sub-pixel font rendering | Terminal monospace — all chars same width |

---

## What's already built (reuse directly)

- **`StructuredProgressTracker`** — per-track status state machine. Already knows `already_downloaded` / `done` / `failed` / `pending`. Directly drives the table.
- **Event line buffer** — feeds the activity log. Timestamps are already present in the event data.
- **Source toggling** — space to enable/disable sources, already implemented.
- **Full keyboard vocabulary** — every keybind in the prototype (`j/k`, `space`, `a`, `n`, `d`, `[/]`, `l`, `u`, `t`, `x`, `ctrl+c`, `enter`, `esc`, `q`) already exists in `tui.go`.
- **Cancellation** (`x` / `ctrl+c`) — already wired to the sync goroutine.
- **Channel-based async running** — the sync runs in a background goroutine, emitting `tuiSyncEventMsg`. This is exactly what drives the running → done state transition.
- **Per-track status vocabulary** — `already_downloaded`, `done`, `failed`, `pending`, stage-level progress. All present.
- **Plan selector table** — the existing plan selector already has cursor navigation, toggleable rows, select all / clear all, selection counter.

---

## What needs new work

### Structural: layout refactor
`tui.go` renders everything sequentially (top to bottom strings joined with newlines). The v4 layout requires a **two-column layout** (`sidebar | main`) using `lipgloss.JoinHorizontal`. This means splitting the current monolithic `View()` into:
- `renderSidebar() string`
- `renderTopbar() string`
- `renderCmdbar() string`
- `renderShortcuts() string`
- `renderTable() string`
- `renderActivityLog() string`
- `renderFooter() string`

Then composing them with `JoinVertical` (main column) and `JoinHorizontal` (sidebar + main).

This is the **bulk of the work** — not risky technically, but it's a meaningful structural refactor of the most complex file in the codebase.

### Height budgeting
The current TUI renders sequentially with no explicit height management. V4 needs the table to scroll within a bounded height. This requires:
- Listening to `tea.WindowSizeMsg` to get terminal dimensions
- Computing available height for the table: `termHeight - topbarH - cmdbarH - shortcutsH - activityLogH - footerH`
- Passing that height to a `bubbles/viewport` wrapping the table

### Sidebar filter panel
There is no sidebar at all currently. This is new — but straightforward with lipgloss.

### Plan state → unified table
Currently the plan selector is a **modal overlay** over the sync screen. In v4, the track table IS the primary view for all three states (plan, running, done). The table transitions between:
- Plan state: checkboxes, all interactive
- Running state: outcome indicator column, locked
- Done state: outcome indicator column, locked, with summary footer

This unification simplifies the modal logic but requires thinking through the state machine transitions carefully.

### Summary/cmdbar bar for done state
New component — the two-line command bar and the consolidated footer stat bar.

### Pulsing badge animation
Needs a `tea.Tick`-based state toggle. Simple — 5–10 lines of code.

---

## Honest risk areas

### Terminal width constraints
The HTML prototype renders at ~900px width. At 80 columns, sidebar (≈24 chars) + table number + outcome col + status col + ID col ≈ 55 chars left for track names. That's workable but requires careful truncation. At 120+ columns (typical dev terminal), it's comfortable. The layout needs responsive behavior: either hide the sidebar below a threshold or collapse the ID column.

### lipgloss v0.13 vs v1.x
You're on `v0.13.0`. The `v1.x` release introduced a proper `Layout` engine. `v0.13` `JoinHorizontal`/`JoinVertical` is sufficient for this layout but requires more manual height/width math. Upgrading may simplify things but is not required.

### `tui.go` refactor scope
At 1861 lines, splitting into sub-models carries real regression risk. The existing `tui_test.go` (22KB) provides coverage but focuses on behavior, not layout. A careful incremental approach — extract one component at a time, verify tests pass — is safer than a big-bang rewrite.

### Activity log height + table scroll
Getting the scroll regions to work correctly at various terminal sizes is the trickiest implementation detail. The table needs to know exactly how many rows fit so cursor-following scroll works. `bubbles/viewport` handles this well once the height budget is wired correctly.

---

## Recommended approach

1. **Extract sidebar as a separate sub-model** — easiest win, no behavioral changes.
2. **Wire `tea.WindowSizeMsg`** and compute height budgets throughout.
3. **Convert table rendering** to use a `bubbles/viewport` so it scrolls properly.
4. **Add lipgloss styling** to topbar, cmdbar, shortcuts bar — purely visual, no logic changes.
5. **Unify plan selector into the main table** — remove the modal, make the table stateful across plan/running/done.
6. **Add activity log panel** using the existing event buffer.
7. **Wire pulsing animation** with `tea.Tick`.

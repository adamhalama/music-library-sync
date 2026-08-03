# Reliable, Unified Sync Queue

- **Status:** Done
- **Target:** Sync planning, execution, cancellation, and progress reporting across the Go engine, agent protocol, CLI/TUI, and native macOS app
- **Last updated:** 2026-08-03
- **Implementation tracker:** [IMPLEMENTATION.md](./IMPLEMENTATION.md)
- **Predecessor:** [plans/archive/gui-redesign-plan.md](./plans/archive/gui-redesign-plan.md)

## Purpose

Make an accepted sync plan visually stable, accurately ordered, responsive while
downloads are active, and reliably cancelable. The track-selection table becomes
the run table: it stays on screen from review through completion, shows the actual
execution slot before acceptance, and locks mutation controls once execution
starts.

This initiative also removes the progress-event and filesystem work responsible
for download-time lag without weakening per-track state durability.

## Product Decisions

These decisions are fixed for this initiative:

1. **One table from plan to completion.** Continuing a plan does not navigate to
   a separate run presentation or reorder the visible rows.
2. **Source order stays primary.** Rows remain in the source's natural order.
   The authoritative execution order is shown separately as `run #N`.
3. **Oldest-first is the sync default.** Native GUI, CLI, TUI, agent fallbacks,
   and controllable engine paths use `oldest_first` unless the user explicitly
   chooses `newest_first`.
4. **Queue positions are immediate.** Selection and order changes recompute
   `run #N` synchronously in the plan table. Continue does not add another
   confirmation step.
5. **Accepted queues are immutable.** Once Continue is pressed successfully,
   selection, order, window, and reset controls lock. Search, filters, sorting,
   row navigation, and source navigation remain available.
6. **Multi-source runs auto-focus the work.** The source needing input wins,
   otherwise the actively downloading source wins. Previously planned sources
   remain accessible through the sidebar.
7. **Stop remains confirmed.** The destructive Stop button keeps its warning;
   the confirmed action must cancel reliably and report its outcome.
8. **Standalone Free DL keeps its own default.** The oldest-first change applies
   to sync surfaces, not the separately configured Free DL workflow.

## Target Experience

During review, selected rows show their pending runtime state, plan class, and
execution slot directly:

```text
[x]  1   pending · gap · run #37   JØK3R X DEGA - B
[x]  2   pending · gap · run #36   UÑAS PINTADAS
[x]  3   pending · gap · run #35   HUMAN ERROR - WA
```

For oldest-first, the oldest selected row is marked `run #1` even though it may
appear near the bottom of the source-order table. Deselected and already-owned
rows have no run number. Changing selection or order updates every affected run
number immediately.

After Continue, the same rows remain in the same positions. Checkboxes and plan
controls lock, while status transitions in place through `pending`,
`downloading`, `downloaded`, `skipped`, or `failed`. Deselected rows read
`not-run`; non-toggleable existing rows read `have-it`.

## Architecture

### Canonical ordering

The Go execution manifest remains authoritative. A single engine-level sync
default resolves unspecified order to `oldest_first`; explicit values still win.
The native app uses a small, pure queue projection with the same rule so it can
show execution slots while Go is blocked waiting for the selection response.
Go canonicalizes the submitted selection as it does today, and runtime snapshots
replace projected slots with backend-provided slots.

For selected rows in source order:

- `newest_first` assigns slots `1...N`;
- `oldest_first` assigns slots `N...1`;
- excluded or locked rows receive slot `0` and render no `run #` label.

### Persistent per-source table state

The native sync state retains plan metadata, selection, projected/canonical
execution slots, acceptance state, and runtime status per source. A new plan
prompt creates or rebuilds an editable source table. A successful Continue
freezes it before the prompt disappears. Subsequent snapshots and row updates
merge into that stored table by stable row identity.

The Sync workspace has two top-level modes only:

- configuration before a run exists;
- the unified source-table workspace while a run is active or retained as a
  terminal result.

Plan-capable sources use the table. A source whose adapter exposes no plan uses
the same workspace shell with an explicit source-level status and empty state.

### Cancellation lifecycle

Cancellation is explicit state, not a best-effort RPC hidden behind `try?`.
Pressing confirmed Stop immediately records `cancelRequested` and enters
`canceling`, even if `sync.start` has not returned a run ID. Once the ID exists,
the app sends exactly one ordered cancellation. A late start response may only
transition `.starting` to `.running`; it can never revive `.canceling`.

Pending UI interaction replies remain ordered before `run.cancel`. Cancellation
acknowledgement, request failure, and terminal exit code 130 are represented
separately. Percentage updates are ignored after cancellation begins, while
terminal and failure notifications remain lossless.

### Progress transport

High-frequency percentage traffic is separated from lifecycle events:

- `sync.event` remains lossless and carries lifecycle, track outcome, failure,
  and source snapshot changes;
- `sync.progress` carries the latest compact progress snapshot and optional
  changed row at no more than 10 updates per second per run.

Pending percentage updates are coalesced newest-wins and flushed before terminal
events. The native transport buffers only the latest progress notification but
never drops lifecycle or terminal notifications. This protocol change requires
a version bump; mixed native/backend versions fail clearly at initialization.

Raw adapter progress is no longer duplicated through the agent's stderr. The
subprocess runner still retains bounded stdout/stderr tails, and failure
diagnostics remain persisted and attached to failures.

### Spotify backend I/O

Deemix execution uses one initial media snapshot and one post-invocation snapshot
per track, advancing the baseline rather than scanning both before and after
every track. Spotify state is loaded into a reusable writer once per source.
Each completed track is still atomically persisted immediately, but the state
file is not reread and reparsed for every update.

## Compatibility and Safety

- Config, sync state, and archive formats remain backward-compatible.
- The agent protocol version increases because progress delivery changes.
- Explicit `newest_first` behavior remains supported.
- Completed track state remains committed when a later track is canceled.
- The active partial track is killed and cleaned as today.
- No credential, Rekordbox, playlist-cache, signing, or security-sensitive
  behavior changes.

## Completion Criteria

The initiative is complete when:

- every selected plan row shows its correct `run #N` before Continue;
- the first executed track is the row marked `run #1`;
- Continue leaves the same source-order table on screen and locks only mutation
  controls;
- multi-source focus and sidebar history behave as specified;
- confirmed Stop responds immediately, cannot be lost during startup, and
  reaches canceled terminal state without stale progress obscuring it;
- progress rendering is capped at 10 updates per second and Stop stays responsive
  under a synthetic 1,000-line-per-second adapter stream;
- Spotify/deemix performs at most one media-library walk per track invocation;
- completed-track state survives cancellation exactly as before;
- Go race tests, Swift tests, protocol fixtures, and manual native-app checks all
  pass; and
- [IMPLEMENTATION.md](./IMPLEMENTATION.md) records the final evidence and every
  deviation from this plan.

# Rekordbox and Navidrome Directional Playlist Sync

- **Status:** Planned
- **Target:** Safely mirror one paired playlist in either direction between
  Rekordbox and Navidrome, preserving membership and order
- **Last updated:** 2026-08-07
- **Implementation tracker:** [IMPLEMENTATION.md](./IMPLEMENTATION.md)
- **Predecessor:**
  [plans/archive/navidrome-likes-rekordbox-plan.md](./plans/archive/navidrome-likes-rekordbox-plan.md)

## Purpose

The working pattern is gig-oriented:

1. Curate a Rekordbox playlist such as `favs_august` from the larger favourites
   library.
2. Send that playlist to Navidrome so it is available in the phone client.
3. Add, remove, or reorder tracks on the phone.
4. Bring the Navidrome playlist back into the paired Rekordbox playlist.

This is deliberately an **explicit directional mirror**, not an automatic
merge. Each run names its source, previews the exact replacement, and applies
only after confirmation. The selected source controls both membership and
order for that run.

## Current State

The repository already has most of the safe primitives:

- Rekordbox inspection exposes playlists, ordered content IDs, and library
  paths through the pyrekordbox bridge.
- The existing Rekordbox mirror uses checksummed plans, precondition checks,
  mandatory backups, a transactional rewrite, and post-apply verification.
- Navidrome exposes real library paths and ordered playlist contents through
  the Subsonic API, with credentials kept in Keychain.
- Standalone playlist snapshots and the native app already establish
  cache-first reads, explicit refreshes, cancellable agent runs, and on-screen
  workflow errors.

The missing pieces are a Rekordbox playlist source model, a writable Navidrome
playlist client, a direction-neutral mirror planner, durable pair bindings, and
CLI/agent/native-app surfaces for the complete workflow.

## Product Decisions

1. **Direction is selected for every run.** Supported values are
   `rekordbox-to-navidrome` and `navidrome-to-rekordbox`. There is no automatic
   three-way merge or last-writer-wins rule.
2. **The source controls membership and order.** A successful apply makes the
   destination exactly match the selected source.
3. **Every mutation is plan then apply.** Plans are checksummed, human-readable,
   saveable, and revalidated immediately before the destination changes.
4. **Matching is exact normalized real path only.** No artist/title fallback is
   allowed; a plausible wrong match is worse than a refused sync.
5. **Partial mirrors are forbidden.** Empty sources, missing matches, ambiguous
   paths, and duplicate canonical tracks block apply.
6. **Existing exact-name destinations are adoptable.** The first plan previews
   their complete replacement. Multiple exact-name matches are ambiguous and
   block planning.
7. **Missing destinations may be created.** The source must always exist; the
   plan explicitly identifies a destination creation before apply.
8. **Only normal, writable Navidrome playlists qualify.** UDL-managed smart
   playlists and playlists owned by another account are never overwritten.
9. **Sync remains manual.** Scheduling, background polling, and implicit sync
   after download or scan are deferred.
10. **Existing one-way workflows stay compatible.** Standalone snapshots,
    favourites, and `udl rekordbox playlist-sync` keep their current formats and
    behavior.

## Configuration and State

Add optional paired jobs to `playlists.yaml` without changing the existing
configuration version:

```yaml
version: 1

sync_jobs:
  - id: favs-august
    rekordbox:
      playlist: favs_august
      playlist_id: "" # optional stable selector
    navidrome:
      playlist: favs_august
      playlist_id: "" # optional stable selector
```

- Job IDs are unique and use the existing safe identifier rules.
- Both playlist names are required because either side may need to be created
  as a destination; provider IDs are optional stable selectors.
- A successful apply atomically records the resolved IDs, configuration
  fingerprint, last direction, timestamp, and verified final checksum under
  `state_dir/playlist-sync/jobs/<job-id>.json`.
- Saved bindings survive external playlist renames. A changed job
  configuration invalidates the old binding and forces fresh selector
  resolution and adoption preview.
- Operational state informs selection and status only; it never chooses a
  direction or bypasses live preconditions.

## User Interfaces

### CLI

```text
udl playlist sync list
udl playlist sync plan --job favs-august \
  --direction rekordbox-to-navidrome [--out <file>]
udl playlist sync plan --job favs-august \
  --direction navidrome-to-rekordbox [--out <file>]
udl playlist sync show --plan-file <file>
udl playlist sync apply --plan-file <file> [--force]
```

- `--direction` accepts only the two explicit values above.
- Global `--json`, `--dry-run`, and `--no-input` retain their existing meaning.
- Interactive apply names the source and destination and reports additions,
  removals, moves, unchanged tracks, and final count before confirmation.
- Non-interactive apply requires `--force`; primary results go to stdout and
  progress/errors to stderr.
- Existing exit-code conventions remain, including `5` for a mutation whose
  outcome needs reconciliation and `130` for interruption.

### Agent protocol and native app

Add `playlistSync.inspect`, `playlistSync.plan`, and `playlistSync.apply` run
methods. The native app gets a dedicated **Playlist Sync** workspace with:

- configured jobs and last verified status in the sidebar;
- explicit “Send Rekordbox to phone” and “Bring phone edits to Rekordbox”
  direction choices;
- a row-level add/remove/move/keep/blocked preview;
- inline blockers and disabled-control explanations;
- a confirmation that names the destination, removals, final count, and backup;
- workflow-owned success, failure, cancellation, and partial-state messages.

## Safety and Apply Semantics

Every plan records the job configuration fingerprint, resolved selectors,
ordered source and destination memberships, matched ID/path pairs, destination
creation intent, blockers, preconditions, and checksum.

Immediately before writing, apply must:

1. Verify the plan version and checksum.
2. Reload the job and reject a changed configuration fingerprint.
3. Re-read both playlists and all matched library records.
4. Reject any source or destination change since planning.
5. Confirm that Rekordbox is closed and has no live database sidecars whenever
   its database is read or written.
6. Create the destination-specific mandatory backup.

For Rekordbox destinations, reuse the existing full database-directory backup,
transactional playlist rewrite, and committed-state verification.

For Navidrome destinations, use its own database backup command, replace the
complete ordered song list through the supported Subsonic `createPlaylist`
operation, and read the playlist back for exact ordered verification. Prefer
form-encoded POST when supported so large playlists do not place credentials or
song lists in an oversized URL. Creation-by-name is never blindly retried after
an uncertain network result; reconcile by exact-name lookup and planned
membership instead.

Pair state advances only after exact destination parity is verified. A failure
or cancellation never claims success, and any uncertain write reports the
backup path and reconciliation steps.

Security behavior remains unchanged: the password stays in Keychain, auth
tokens and query strings are redacted, plans/state contain no secrets, and the
managed Navidrome service remains trusted-LAN-only with no port forwarding.

## Scope

### In scope

- Reusable paired playlist jobs, not a hard-coded `favs_august` special case.
- Exact membership and order in both directions.
- Destination creation or explicit adoption of one unique exact-name playlist.
- Checksummed plan/show/apply workflow with live preconditions and backups.
- CLI, agent protocol, and native macOS app support.
- Unit, integration, isolated Rekordbox, fake-Navidrome, and manual phone
  acceptance coverage.

### Out of scope

- Automatic or scheduled synchronization.
- Simultaneous three-way merging or conflict resolution.
- Fuzzy metadata matching, downloading missing tracks, or importing files into
  either library.
- Syncing stars, ratings, play counts, cue points, comments, colors, or other
  playlist metadata.
- Deleting paired playlists or allowing an empty source to clear a destination.
- Writing UDL-managed Navidrome smart playlists.
- Changes to Music.app or Apple Music playlists.

## Exit Criteria

The initiative is delivered when:

- a Rekordbox `favs_august` playlist can create or adopt the paired Navidrome
  playlist with identical ordered real paths;
- a phone-side add, removal, and reorder can be planned and reproduced exactly
  in Rekordbox;
- re-planning either direction after parity produces a no-op;
- empty, missing, ambiguous, duplicate, stale, unowned, and smart-playlist cases
  refuse before writing;
- every write has a verified backup and exact post-apply readback;
- interruption or uncertain network delivery cannot advance pair state or hide
  a potentially changed destination;
- CLI JSON/human output, agent RPC, and the native app describe the same plan
  and outcome; and
- the validation commands and manual round trip are recorded in
  [IMPLEMENTATION.md](./IMPLEMENTATION.md).

## Risks and Mitigations

- **Real-path reporting is disabled or drifts.** Refuse all fuzzy matching and
  point to the managed Navidrome `DefaultReportRealPath` setting.
- **A provider changes after planning.** Ordered source/destination and matched
  path preconditions force regeneration before any write.
- **Navidrome creation succeeds but the response is lost.** Do not blindly
  retry; reconcile unique exact-name membership and return partial status when
  uncertain.
- **A new field invalidates old Rekordbox plans.** Use a separate mirror plan
  type/version and leave the existing `playlistsync.Plan` checksum contract
  untouched.
- **A local binding write fails after the destination succeeds.** Report partial
  success, preserve the verified external result, and make the next plan
  rebuild the binding safely.

## Deferred

- Automatic three-way sync using a last-successful baseline.
- Background/scheduled runs and phone push notifications.
- Per-track conflict resolution or user-selected partial application.
- Playlist folders and batch mirroring of multiple gig playlists.
- Metadata and Rekordbox DJ-data synchronization beyond playlist membership and
  order.

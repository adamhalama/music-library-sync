# Archived Plans

These documents are retained as implementation history. They are not the current source of truth.

Current planning documents:

- [../../PLAN.md](../../PLAN.md) — high-level product and architecture plan
- [../../IMPLEMENTATION.md](../../IMPLEMENTATION.md) — live implementation tracker

Archived work:

- `architecture-sequencing.md` — sequencing for the original `sync --plan` refactor
- `sync-first-plan.md` — SoundCloud `sync --plan` v1
- `three-stage-big-plan.md` — execution seam, planning, and output normalization
- `tui-redesign-implementation-plan.md` — shared-shell Bubble Tea redesign
- `freedl-playlist-rekordbox-plan.md` — completed Free DL, playlist, and Rekordbox integration plan
- `freedl-playlist-rekordbox-implementation.md` — completed implementation record and evidence
- `native-macos-frontend-plan.md` — completed native SwiftUI frontend and agent-protocol plan
- `native-macos-frontend-implementation.md` — completed implementation record and evidence for that frontend
- `gui-redesign-plan.md` — completed native macOS GUI redesign plan
- `gui-redesign-implementation.md` — completed GUI redesign implementation record and evidence
- `reliable-unified-sync-queue-plan.md` — completed sync queue continuity, cancellation, ordering, and performance plan
- `reliable-unified-sync-queue-implementation.md` — completed implementation record and evidence for the unified sync queue
- `navidrome-phone-library-plan.md` — Navidrome + Amperfy phone library plan; code complete
- `navidrome-phone-library-implementation.md` — implementation record and evidence for that initiative
- `navidrome-likes-to-rekordbox-plan.md` — Navidrome likes reaching Rekordbox, and the GUI playlist-error blind spot; code complete
- `navidrome-likes-to-rekordbox-implementation.md` — implementation record and evidence for that initiative

The native macOS frontend, visual-design pass, unified sync queue, the Navidrome
+ Amperfy phone library, and Navidrome likes reaching Rekordbox are all code
complete.

Two successive trackers have been archived with items still open — the phone
library's Amperfy acceptance and documentation, and the likes-to-Rekordbox
Rekordbox apply and un-star check. **None of those are lost.** Each generation
restates its predecessor's open items as Phase 0 of the successor, and they now
live in Phase 0 of the current [../../IMPLEMENTATION.md](../../IMPLEMENTATION.md).
Tick them there, not here.

The current root plan covers `udl triage` — remote metadata triage from the
phone. Its wider context is
[../holy-grail-music-loop.md](../holy-grail-music-loop.md).

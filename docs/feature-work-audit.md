# Branch and feature audit — 2026-10-10

This is a dated audit, not a replacement for the active `PLAN.md` and
`IMPLEMENTATION.md`. PR CI and merge status can change after publication.

## Repository state and PRs

The repository has one registered worktree, checked out on
`chore/freedl-upgrade-09-12`. It was clean, with no stashes. All four local
branches now track matching remote branches; no unpublished commits remain.
No branches were deleted, no history rewritten and no feature PRs merged.

| Branch | Publication and review | Integration status |
| --- | --- | --- |
| `master` | Matches `origin/master`; last merged PR is #23 | Does not contain the native GUI or later Free DL work |
| `feature/swiftui-gui` | Eight commits above master; previously unpublished `afb3840` pushed; [draft #25](https://github.com/adamhalama/music-library-sync/pull/25) targets master | Conflict-free against master, but fresh Linux and macOS CI fail on known foundation regressions fixed in #24 |
| `chore/freedl-upgrade-09-12` | Published; [#24](https://github.com/adamhalama/music-library-sync/pull/24) targets GUI | Two review threads verified resolved; previous head `bc1d4ec` passed all three checks; final approval/integration review remains |
| `feature/genre-picking-app` | Newly published; [draft #26](https://github.com/adamhalama/music-library-sync/pull/26) targets GUI | One unique planning commit, `0f5c794`; inherited Linux CI failures and root-planning conflicts |

Both newly published tips passed `git diff --check` before the push. A clean
worktree and whitespace check do not imply passing runtime CI. Draft PRs make
their validation and planning gaps explicit.

Updating the remote GUI base to `afb3840` removes that already-shared commit
from #24's diff: before this audit's documentation/CI cleanup, #24 contained
29 commits and 68 changed files against its updated base, while the combined
GUI/Free DL work contained 37 commits and 337 changed files against master.

Recommended integration order: review and merge #24 into GUI, validate and
review the resulting #25 against master, then reconcile #26's planning content
with the active initiative. Preserve `plans/holy-grail-music-loop.md` and the
historical archive material without allowing the older root plan to silently
replace the directional playlist-sync plan. Branch deletion follows verified
integration and is not part of this audit.

## Feature status

| Feature | Implemented and evidenced | Remaining work |
| --- | --- | --- |
| Native macOS app | SwiftUI shell and workflow screens; configuration/onboarding; credentials; doctor; sync; cached playlists; Free DL; Rekordbox; Go agent v2; development build/install and ad-hoc signing | Foundation PR review; workflow-specific manual parity checks. The old parity document's full-Xcode automation gap is superseded by #24 CI, but manual checks are not automatically closed |
| Unified sync execution | Lossless lifecycle/terminal events, latest-only progress, cancellation/late-ack handling, ordered source execution and cached Spotify state persistence | Integration review; archived queue tracker is code complete |
| Navidrome/Amperfy phone library | Setup/service controls, date reconciliation, genre allowlist, generated smart playlists, phone connection tracking, favourites and snapshot handoff | External phone/network acceptance retained in archived evidence; this is not the proposed genre-classification workflow |
| Directional Rekordbox/Navidrome playlist mirror | Separate checksummed plans; exact real-path matching; provider IDs in state; both directions; owned-playlist replacement; backups; drift checks; parity verification; CLI, agent, native workspace and basic name/ID pair editor | Active tracker remains in progress: provider-picker enrichment and editor validation, lifecycle/view matrices, combined CLI acceptance, isolated destructive restore, real phone edit round trip and manual GUI close-out |
| SoundCloud Free DL upgrades | Registered plan adapter actually executes; artwork input ordering fixed; supported-gate filtering; isolated batch reports 15/15 supported upgrades and 46/46 audio decodes; reusable gate script and provider helpers | General maintenance commands now derive scope/identity from saved plans and configured jobs; preservation repair remains a Python maintenance step; new batches still need capture, provenance, preview and quality validation |
| Spotify/deemix | Per-track Spotify ISRC enrichment and cache priming prevent false unavailability or incorrect audio; API/public-page fallback limitations documented | Dependency/API availability remains external; integration review |
| Interactive TUI sync | Scan-gaps and ask-on-existing options carried through interactive workflow | CLI UX integration review |
| Curated genre workflow / broader music loop | A proposal and historical plan archival only, saved by #26 | Taxonomy decisions, suggestions/review queue, file-tag write/backfill, scheduling, Rekordbox analysis gate and export evaluation are not implemented |
| macOS packaging/CI | Xcode app/test schemes, universal backend verification, embedded smoke and ad-hoc release policy; #24's prior macOS CI passed 93 Swift tests | App workflow push filter used `main` although default branch is `master`; corrected by this cleanup. Release/install/manual acceptance is separate from a successful CI build |

Root tracker phases 0–4 are recorded Done and phases 5–9 In progress. Several
unchecked test/close-out items coexist with later evidence that those cases
were exercised; reconcile them deliberately. For example, the basic native pair
editor is implemented in `PlaylistSyncJobEditor.swift` with name/optional-ID
fields and guarded configuration saving, although the combined editor/picker
task remains unchecked. `playlist_mirror_test.go` already covers stale-state,
backup-failure and post-write state-failure cases; broader acceptance is still
open. Older `docs/roadmap.md` and
`docs/swiftui-parity.md` contain stale status statements and must not be treated
as authoritative evidence that implemented features are absent.

## Free DL reuse and saved evidence

See [.dev/freedl-0912/README.md](../.dev/freedl-0912/README.md) and the
[userscript guide](../.dev/userscripts/README.md). Code, tests, results, quality
table and portable hashes are tracked and remotely published. Original media,
private action evidence, OAuth information and rollback backups remain outside
Git. Git preserves the tools and public proof; it is not a backup of that media
or private recovery state.

Reusable application fixes live in the Go engine/adapters. The gate userscript
and URL/tab-parameterized browser helpers can be used on other tracks. The
Python maintenance commands now use [`.dev/freedl.py`](../.dev/freedl.py) and
[the general workflow](freedl-maintenance.md). Saved native plan metadata supplies
the exact job/library/buffer/backup/log scope and identities; connection context
retains initial config choices. Containment, backups, transactional restoration,
encoded-audio/artwork/decode checks and complete MP4 tag preservation remain.
The wrapper applies metadata restoration after native promotion, but other
native/app routes do not automatically invoke this Python maintenance layer.
New validation covers two independent jobs through the real Go backend using
synthetic media, plus traversal/conflicting-context/nested-path/migration cases.
All 13 actual legacy plans passed read-only context validation, and legacy status
still reports 15 upgraded, 0 pending and 31 unavailable tracks. No real-library
mutation or browser action ran. Linux and macOS CI now run the maintenance tests.

## CI failures and fresh validation

Fresh #25 Linux checks failed on:

- Darwin-only `setBirthTime` / `birthTime` helpers compiled on Linux.
- Success fixtures exercising macOS-only Navidrome configuration on Linux.
- Test-harness `PASS` output appearing on the agent stdout protocol.

The macOS job also failed on that protocol-stdout regression before the Swift
build stage. Fixes are already published in #24 (`a4a2c03`, `55e8ca6`,
`fd32f29`). #26 inherits the older APFS/platform-fixture failures; its planning
commit does not change runtime code. Its three documentation merge conflicts
are `IMPLEMENTATION.md`, `PLAN.md`, and `plans/archive/README.md`.

Fresh local validation on `bc1d4ec` during this audit passed:

```sh
go test ./...
go vet ./...
python3 .dev/freedl-0912/test_quality.py
python3 .dev/freedl-0912/test_droploud.py
node --check .dev/userscripts/udl-freedl-gate-autopilot.user.js
NODE_PATH=/tmp/udl-userscript-tests/node_modules python3 .dev/freedl-0912/test_soundcloud.py
NODE_PATH=/tmp/udl-userscript-tests/node_modules node .dev/userscripts/udl-freedl-gate-autopilot.test.cjs
```

Results: 11 quality tests, 2 Droploud tests, 9 SoundCloud tests and 16 DOM
scenarios passed. The temporary jsdom installation was incomplete initially;
reinstalling the documented `jsdom@29` outside the checkout resolved those
fixture errors. No live browser/social actions or music-library writes ran.

Before publication, cleanup also ran whitespace checks on both unpublished
branch ranges and simulated the genre merge with `git merge-tree --write-tree
--name-only`; the simulation returned the three documented conflicts without
changing the worktree. No new full-Xcode, race or destructive/phone acceptance
run is claimed here; #24 retains its earlier passing CI evidence.

## Native preservation follow-up

The earlier maintenance-only preservation limitation is resolved in the shared
Go app/TUI/native promotion service and legacy CLI. Complete container metadata is
restored and checked on the staged file before install; failed verification keeps
the library and original backup. Python repair remains for earlier backends and
manual migrations. See `docs/freedl-maintenance.md` for supported container layouts
and direct app-RPC/native-CLI acceptance commands. Earlier audit entries above are
historical descriptions, superseded by this follow-up.

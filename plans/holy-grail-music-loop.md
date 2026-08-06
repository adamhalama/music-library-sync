# The Full Loop: SoundCloud Like → CDJ USB

- **Status:** Exploration / feasibility, not yet scheduled
- **Last updated:** 2026-08-06
- **Relationship to current work:** independent of [../PLAN.md](../PLAN.md)
  (Navidrome likes → Rekordbox), but consumes its output. Do not start before
  that lands.

## The Idea As Stated

> like a track on SoundCloud → it downloads to the Mac → it lands in
> Navidrome/Amperfy so it can be liked and browsed → it gets a genre, assigned
> semi-automatically through a good picker → it imports into Rekordbox with that
> genre → playlists export into a favourites playlist, gated by genre → and the
> USB in my pocket ends up with the new tracks on it, without me being at the
> Mac.

## Summary Of Findings

Seven of the eight legs are either already built or are ordinary work inside
this repo's existing seams. One leg — getting bytes onto a Rekordbox-exported
USB stick that is not physically attached to the Mac — is a hard wall for
reasons that are not about effort. It is treated separately in
[The USB Wall](#the-usb-wall).

The single most valuable *new* thing this repo could build is the **genre
layer**, and it is worth building whether or not the USB leg is ever solved.

## Current State, Leg By Leg

| # | Leg | State | Where it lives / stops |
| --- | --- | --- | --- |
| 1 | Like on SoundCloud | **Done, and it is already the trigger** | `soundcloud-likes` source polls the likes URL |
| 2 | Download to Mac | **Done** | `internal/engine`, `scdl` + `yt-dlp`, state/archive-based dedupe |
| 3 | Appear in Navidrome/Amperfy | **Done** | Navidrome serves `~/Music/downloaded`; `udl navidrome dates reconcile` fixes Date Added |
| 4 | Like it on the phone, get it back | **In flight** | Current `PLAN.md` — `getStarred2` → `navidrome-favorites` snapshot |
| 5 | Assign a genre, well | **Missing — the real gap** | Genre is whatever SoundCloud's uploader typed; nothing normalises or curates it |
| 6 | Import into Rekordbox with that genre | **Partial** | `playlist_sync_helper.py` only rewrites playlists over *existing* collection content; nothing adds tracks |
| 7 | Export to a fav playlist, genre-gated | **Nearly done** | `playlistsync` mirrors any snapshot; genre filtering is a small addition |
| 8 | Reach the USB in your pocket | **Wall** | Nothing exists; see below |

### Trigger and remote download (legs 1–2)

This is already solved and you may not have noticed. `soundcloud-likes` reads
your likes page — so **liking a track on SoundCloud *is* the remote trigger**.
There is no need to build a remote-control channel for the common case; a
launchd timer running `udl sync --source soundcloud-likes` turns "like it on the
train" into "it is on the Mac when you get home", today.

A real remote control channel is still cheap if you want one: `udl agent` is
already a complete NDJSON JSON-RPC surface (56 methods, `internal/agent/server.go`)
covering sync, playlists, Navidrome, and Rekordbox. Exposing it over Tailscale
behind a token is a shim, not an architecture. That is the right way to build
"kick off a sync / retrigger an import from my phone" **when** the automatic
trigger is not enough — not before.

### The genre problem is real and measurable

Sampling 400 files in `~/Music/downloaded`:

```
178 Hard Bounce      21 Techno         2 HARD BOUNCE
 51 SCHRANZ          19 Bounce         2 Schranz
 39 Hard Groove      14 Trance         2 Groove Bounce
 34 240 SOUND         9 Hard Trance    1 Hardbounce / hardbounce / bounce
                      5 <none>         1 Tekno, 1 speedcore, 1 hardstyle, …
```

Three separate defects in one sample:

1. **Case and spelling variants** — `Hard Bounce` / `HARD BOUNCE` / `Hardbounce`
   / `hardbounce` are four genres today. This is not cosmetic: Navidrome's `.nsp`
   genre rule is **case-sensitive** (already documented in
   `internal/navidrome/config.go:563`), which is why `hard_bounce_genres` has to
   be an allowlist of every spelling rather than one genre.
2. **Non-genres in the genre field** — `240 SOUND` is a label, `bladee` is an
   artist. 34 files, ~9% of the sample.
3. **Uncurated granularity** — `Bounce`, `Hard Bounce`, `Hard Groove`,
   `Hardgroove Techno` may or may not be distinctions you want to make. Nobody
   has ever decided.

Every downstream consumer inherits this: the Navidrome smart playlists, any
genre-gated Rekordbox export, and your own browsing in Amperfy. **Fixing genre at
the tag, once, before Rekordbox ever sees the file, fixes all of them at once.**

### Rekordbox import (leg 6) — do not write the collection

The obvious implementation is to extend `playlist_sync_helper.py` to insert
`djmdContent` rows. Recommendation: **don't.**

- pyrekordbox's content-adding API is experimental, and critically it **cannot
  generate ANLZ analysis files** (beat grid, waveform, cue points). A row you
  insert is an unanalysed track. You explicitly care about grid quality
  ("assuming the grid is analysed well"), so an import path that guarantees
  *un*analysed tracks is the wrong path.
- The repo's existing Rekordbox posture is deliberately conservative — full
  directory backup, checksummed plans, fail-closed on ambiguity, refuses to run
  while Rekordbox is open. Adding collection writes puts the riskiest possible
  operation inside the most safety-critical component.

**Use Rekordbox's own watch folder instead.** Point a Rekordbox watched folder
at `~/Music/downloaded`, enable auto-analysis. Then the entire "import with the
correct genre" requirement collapses to *"the file must have the right genre tag
before Rekordbox first sees it"* — which is exactly what the genre layer
produces. Rekordbox does the import and the analysis; UDL never touches
`djmdContent`.

The one thing UDL should add on this leg is a **gate**: read `djmdContent`
(already possible today — `inspect_db` in the bridge) and refuse to include
tracks that are not analysed in an export plan. That converts "assuming the grid
is analysed" from an assumption into a check.

### Genre-gated favourites export (leg 7)

Cheapest leg in the whole plan. `internal/app/rekordbox_playlist_sync.go` already
mirrors an arbitrary snapshot into an arbitrary Rekordbox playlist, and
`inspect_db` already returns collection contents. Filtering the planned content
IDs by genre, and routing per-genre into different target playlists, is a plan-
building change — no new subsystem, no new write path. Note `VerifyPlanChecksum`
re-marshals the plan, so any new plan field must be `omitempty` (AGENTS.md).

## The USB Wall

The stated design is: take the USB attached to the phone, replicate it somewhere
the Rekordbox machine can see as a real device, let Rekordbox export onto it,
then push the result back down to the physical stick.

Three findings collapse it.

**1. There is no remote PC.** Rekordbox is on this Mac —
`~/Library/Pioneer/rekordbox` is populated and `~/Music/rb-library-export` has
nine backup directories from real `playlist-sync` runs. Downloads, Navidrome,
and Rekordbox are all one machine. So "inject a USB into my PC remotely" is not
needed at all; the only thing that is genuinely elsewhere is the stick.

**2. The phone is an iPhone.** Amperfy is iOS-only. This is decisive, because
the entire back-half of the design assumes the phone can act as a sync endpoint
for an attached mass-storage device. On iOS there is no rsync, no Syncthing to
an external volume, no background filesystem access to a USB drive — the Files
app can copy by hand and that is the whole API. USB-over-IP (VirtualHere,
usbip) needs a server on the machine physically holding the device, which an
iPhone cannot run. That leg is not "hard", it is closed.

**3. Even with a perfect file channel, copying files is not exporting.** A
CDJ-readable USB is not a folder of audio. It is `CONTENTS/` plus
`PIONEER/rekordbox/export.pdb` — a binary database plus per-track ANLZ files.
Adding a track means *rewriting the PDB*, and no available tooling writes it
(`pyrekordbox` and Deep Symmetry's `crate-digger` read the export format;
neither writes it). So even a rooted Android with rsync would produce a USB the
CDJ ignores. Only Rekordbox itself can produce a valid export.

Points 2 and 3 are independent — either one alone kills the design.

### What is actually available instead

Roughly in order of how much I would recommend them:

1. **Accept the last mile, make it one command.** Everything up to "Rekordbox
   holds the right tracks, correctly tagged, correctly analysed, in the right
   playlists" becomes fully automatic and remotely triggerable. Plugging the
   stick into the Mac and hitting export becomes a 60-second act instead of an
   evening of manual tagging and playlist work. This captures ~90% of the value
   for ~0% of the risk, and it is the only option that is entirely within this
   repo.

2. **Buy the last mile — rekordbox Cloud Library Sync + rekordbox for iOS.**
   Pioneer's own answer to exactly this problem: the Mac library syncs to the
   cloud, the iOS app pulls it, and the iOS app can export to a USB drive
   attached to the iPhone. If it works as advertised it *is* the holy grail,
   including the "add a track while I'm out" case. Requires a paid plan.
   **Verify before relying on it** — I have not confirmed that iOS export-to-USB
   is available on the plan tier you would need, and Pioneer's feature matrix
   moves.

3. **Buy the last mile differently — CloudDirectPlay.** Deletes the USB from the
   loop entirely: CDJ-3000s pull from your cloud library over the venue LAN. Only
   applicable if you are playing on CDJ-3000s with internet, so it solves the
   problem for some gigs and none of the others.

4. **The interesting hack: a USB-gadget device instead of a stick.** A Pi Zero
   (or similar) in `g_mass_storage` mode presents a backing image file as a
   genuine USB mass-storage device to the CDJ, while you push updates to that
   image over WiFi/LTE. This is the only real version of "inject a virtual USB",
   and it does work in principle. Caveats are serious: FAT32 cannot be mounted
   read-write on both ends at once, so you can only update it while the CDJ does
   not have it mounted; power and boot timing at a venue; CDJs are picky about
   devices. And it still does not solve point 3 — Rekordbox must still generate
   the export, so this is a transport for an export the Mac produced, not a way
   to export remotely. Out of scope for this repo; a fun separate project.

### Recommendation

Build the first option. Treat 2 as an evaluation task you do in an afternoon
with a trial, because if it works it retires the entire question and you should
know that before writing code against it.

## Proposed Plan

Phases are ordered by value-per-risk. Each is independently shippable and each
leaves the system better even if the next is never built.

### Phase 0 — Verify the two things that change the plan

Before any code.

- Confirm whether rekordbox Cloud Library Sync + iOS export-to-USB does what
  option 2 claims, on a trial. If yes, Phase 4 changes shape and the USB
  question closes.
- Confirm Rekordbox's watch-folder + auto-analysis behaviour on
  `~/Music/downloaded`: does a file with a genre tag arrive with that genre, and
  does re-tagging an already-imported file update Rekordbox's genre or not?
  **The answer to the second half determines whether genre assignment must
  happen before first import or can happen at any time**, which is the single
  biggest constraint on Phase 1's UX.

### Phase 1 — The genre layer

The core of the initiative. New package, `internal/genre`.

- **A canonical taxonomy**, in config: your genres, each with alias spellings
  (`hardbounce`, `HARD BOUNCE`, `Hardbounce` → `Hard Bounce`) and an optional
  parent. This is a decision you make once, not something derived.
- **A classifier that suggests, never decides.** Signals in order of cheapness:
  SoundCloud's genre and tags (already captured —
  `internal/engine/soundcloud_freedl_metadata.go` carries `Genre`); the existing
  tag if it maps to a known alias; nearest-neighbour on artist/label against the
  already-classified library. Each suggestion carries a confidence and a reason.
  Leave an LLM or an audio classifier (Essentia) as a later pluggable signal —
  the taxonomy and the review UI are what make those useful, not the reverse.
- **A review queue.** Unclassified and low-confidence tracks accumulate; you
  confirm or override. Keyboard-driven, one key per top genre, because this is
  a task you will do in batches of thirty.
- **Write the decision to the file tag** via ffmpeg, reusing the tagging path
  that already exists in the Free DL flow. Tag is the source of truth — that is
  what makes Navidrome and Rekordbox both agree for free.
- A **backfill** mode for the existing 1,574 files, which is where the `240
  SOUND` / `bladee` / four-spellings-of-Hard-Bounce cleanup happens.

Surface it in the native app first (there is already a Features/ screen per
workflow, and `agent` extends cleanly with `genre.*` methods). A CLI equivalent
follows for scripting. Once this exists, `hard_bounce_genres` shrinks from a
list of spellings to a list of genres, and `derive-genres` becomes a check
rather than a workaround.

### Phase 2 — Make the pipeline unattended

- launchd timer for `udl sync`, then `udl navidrome dates reconcile`, then
  enqueue new arrivals into the Phase 1 review queue.
- A notification when the queue is non-empty — the only manual step left in the
  loop should come and find you.

### Phase 3 — Close the Rekordbox half

- Rekordbox watch folder on `~/Music/downloaded`, auto-analysis on. No UDL
  collection writes.
- **Analysis gate** in `playlistsync`: read `djmdContent` analysis state via the
  existing bridge and fail a plan closed on unanalysed tracks, in the same style
  as the existing missing/ambiguous/duplicate handling.
- **Genre filter** in plan building: gate a snapshot by genre, optionally
  routing per-genre to different target playlists. `omitempty` on any new plan
  field.

### Phase 4 — Remote control, only if still wanted

By this point the automatic trigger covers most of it. If the gaps are real:
expose `udl agent` over Tailscale behind a token, with a small web UI for the
two things that genuinely need a human — approving a genre, and kicking a sync.
A web UI rather than native, so it works from the phone. Do not build this
speculatively; build it against a gap you actually hit.

## Explicitly Not Doing

- Writing `djmdContent` rows / importing into the Rekordbox collection. Phase 3
  explains why.
- Writing `export.pdb`. No tooling writes it and reverse-engineering it for a
  one-user use case is not a good trade.
- Any attempt to make the iPhone a filesystem sync endpoint for an attached USB
  drive.
- Bidirectional genre sync between Rekordbox and the file tags. The tag is the
  source of truth; edits made inside Rekordbox are not propagated back.

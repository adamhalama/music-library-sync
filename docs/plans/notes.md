# Notes — Free DL upgrade 2026-09-12

## 2026-09-12 — scoping

Scratch config (nothing touches the real `udl.yaml` / `freedl.yaml`):
`~/dev/music-down/statefiles/freedl/upgrade-09-12/` holds `config.yaml`,
`state/`, `buffer/`, `backups/`, `logs/`.

**Correction to my first approach.** I wrote a throwaway Go probe under
`experiments/` before checking whether the repo already had one. It does:
`freedl.Service.BuildCapturePlanProgress` (`internal/freedl/progressive.go`)
enumerates the source, probes each track's free-DL status, indexes local media
quality and emits the whole table — which is exactly what I had rebuilt.
Deleted the throwaway; driving `freedl.plan.start` over `udl agent` instead.

**Enumeration order.** `StreamSoundCloudTracks` returns likes newest-first.
Index 1 is `[FREE DL] KØDA - FUMANDO MARIJUANA` (the *last* file written today,
15:22:11); the batch was downloaded oldest-like-first. So today's 46 files map
to roughly the newest ~48 likes, not exactly 46: at limit 46 the tail was
`Emmanuel Messina - Pianolator` and two likes in the window
(`PREMIERE: FANK – Divine [MLKL048]`, `Parole`) have no file on disk at all.
Plan limit must be >46 to reach `Nom Nom [Free DL] - Birthday Special`.

**The real constraint (first probe pass, newest 46 likes):**

| free-DL status | count |
| --- | --- |
| `available` (hypeddit) | 4 |
| `unsupported_host` | 37 |
| `no_free_dl` | 5 |

Only 4 tracks are capturable by the flow as it stands. The 37 unsupported hosts
break down as:

- **Bandcamp, 24 tracks.** These are SoundCloud `purchase_url` values behind a
  *Buy* button, not a free download. Genuinely not upgradable for free.
  (I first guessed the neighbouring `purchase_title` field could separate "Buy"
  from "Free Download" and make the classification more honest. Checked it
  against all 33 non-gate rows later — see "Native downloads and purchase_title"
  below — and it cannot: it is free-text and usually null.)
- **Free-DL gate services the flow does not know: `gaterush.me` (6),
  `droploud.com` (2), `found.ee` (1), `mypresskit.info` (1).** These are the
  same shape as Hypeddit — a gate page that yields a file. `isHypedditPurchaseURL`
  is the only allowlist (`soundcloud_freedl_browser.go`), so they are skipped.
  This is where the upgradable-track count is actually being lost.

## Bug 1 — Free DL capture could never run (`SupportsPlan` missed `scdl-freedl`)

First `freedl.capture.start` returned success while downloading nothing:

```
[upgrade-09-12] --plan does not support adapter.kind=scdl-freedl for source type soundcloud; skipping source
sync finished: attempted=0 succeeded=0 failed=0 skipped=1
```

`freedl.capture.start` (`internal/agent/methods_freedl.go`) runs the capture as a
sync with `Plan: true`, because plan mode is what carries the row selection into
the run. `Syncer.Run` gates plan mode on `planProviderForSource`, which returns
nil unless `engine.SupportsPlan(source)` — and `SupportsPlan`
(`internal/engine/download_order.go`) listed only `scdl` and `deemix`.

The inconsistency is internal to the engine: `NewSyncer` registers
`planRegistry.Register("scdl-freedl", NewSCDLPlanProvider())` and
`SCDLPlanProvider.buildWithTracks` explicitly accepts
`adapter.kind == "scdl-freedl"`. Only the capability predicate disagreed, and it
fails *silently* — the source is skipped with a warning and the run still exits 0.

Fixed by adding `scdl-freedl` to `SupportsPlan`. Checked the other callers first
(`tui_sync_shell.go`, `tui_sync_model.go`, `methods_startup.go`): all of them ask
"can this source be planned", so widening is correct for each. Added
`TestSupportsPlanCoversEveryRegisteredPlanAdapter`, which asserts the predicate
against the registry itself so the two cannot drift apart again.

`go test ./internal/engine ./internal/cli ./internal/agent ./internal/freedl` passes.

## Bug 2 — artwork embedding always failed (ffmpeg input/output option order)

Every captured track logged:

```
metadata tagging warning: metadata written without artwork: exit status 234:
Option map (set input stream mapping) cannot be applied to input url .udl-artwork-*.jpg
-- you are trying to apply an input option to an output file or vice versa.
```

`runSoundCloudMetadataFFmpeg` built `-i INPUT -map 0 -codec copy -i ARTWORK -map 1:0 …`.
ffmpeg reads options positionally, so `-map 0 -codec copy` — sitting between the
two `-i` flags — are parsed as *input* options for the artwork file and rejected.
Tags were still written by the no-artwork fallback path, which is why this
degraded quietly to "every free-DL file has metadata but no cover" instead of
failing the run.

Fix: declare both inputs first, then the output options
(`-i IN -i ART -map 0 -map 1:0 -codec copy -c:v mjpeg -disposition:v:0 attached_pic …`).

Verified against real ffmpeg (`ffmpeg version` in PATH, AAC test file + JPEG):

| arg order | exit | result |
| --- | --- | --- |
| old | 234 | reproduces the error above verbatim |
| new | 0 | `index=1 codec_name=mjpeg disposition:attached_pic=1` |

Extracted the argument construction into `soundCloudMetadataFFmpegArgs` so the
ordering is testable without invoking ffmpeg, and added
`soundcloud_freedl_metadata_args_test.go` asserting that no output option ever
precedes the last `-i`, plus that the no-artwork path stays free of
artwork-only flags.

## Change 3 — free-DL gate allowlist widened beyond Hypeddit

The batch's real bottleneck. `isHypedditPurchaseURL` was the only gate predicate,
so every other gate service classified as `unsupported_host` and was never even
attempted. Checked each host in the batch by hand before touching the allowlist:

| host | in batch | page says | verdict |
| --- | --- | --- | --- |
| `hypeddit.com` | 5 | gate + unlock steps | already supported |
| `gaterush.me` | 7 | "Download X by Y", `<button class="download">` + JS unlock steps | **added** |
| `droploud.com` | 2 | `<title>… \| Free Download \| Droploud>`, "FREE DOWNLOAD" | **added** |
| `mypresskit.info` | 1 | `/gate/…` path, "Free download" | **added** |
| `found.ee` | 1 | smart link, resolves to Spotify/Apple Music | left unsupported |
| `*.bandcamp.com` | 24 | store album/track page | left unsupported |

A SoundCloud `purchase_url` backs both the `FREE DL` and the `Buy` button and
the page exposes no machine-readable hint which, so support has to stay
host-driven. Excluding stores is not laziness: opening a Bandcamp album can
never produce a free file, so classifying it as a gate would just burn a
15-minute browser wait per track.

Implementation: `freeDownloadGateHosts` + `isSupportedFreeDownloadGateURL` in
`soundcloud_freedl_browser.go`, with `matchesFreeDownloadGateHost` keeping the
existing http/https scheme guard (the URL is handed to the OS URL handler, so
`javascript:`/`file:` must never pass) and requiring an exact host or a real
`.host` suffix so `gaterush.me.evil.example` cannot match. `isHypedditPurchaseURL`
is kept as a thin wrapper so its existing tests still pin Hypeddit behaviour.
Probe and flow now call the general predicate; the `hypeddit gate detected` /
`hypeddit-timeout` strings became host-labelled / `free-dl-gate-timeout`, and
the `skipped_hypeddit_timeout` detail became `skipped_gate_timeout`.

Docs updated: `readme.md` (three lines) and `docs/tui.md`. `go test ./...` green.

## Run 1 — Hypeddit-only capture (plan `20260912-154029`)

5 selected, **2 captured**, 3 timed out at the gate.

| track | gate | result |
| --- | --- | --- |
| RED ROOM | hypeddit | captured, 64 MB WAV |
| FRAI X JCB - FEEL GOOD | hypeddit | captured, 88 MB WAV |
| Emmanuel Messina - Pianolator | hypeddit | `browser-wait-timeout` |
| PIKETU | hypeddit | `browser-wait-timeout` |
| Dex Fury - Oriental Bounce | hypeddit | `browser-wait-timeout` |

Both Hypeddit pages carry `gate_type=sc` (a SoundCloud fan-gate), yet two
completed unattended and three did not — consistent with Helium's signed-in
SoundCloud session already satisfying the follow/like step for some artists and
not others. The three timeouts are recorded in
`logs/20260912-154029/upgrade-09-12.freedl-stuck.jsonl`, which is what that
ledger exists for.

I could not drive the browser myself: `screencapture` fails with "could not
create image from display" and Apple Events to Helium hang, so this process has
neither Screen Recording nor Automation permission. Completing a fan-gate also
means following/liking on the user's SoundCloud account, which is not mine to do
unprompted. Stopped short of scripting Gaterush's `/gate-step` endpoint for the
same reason — the browser handoff is the designed mechanism.

Promotion applied, both in place, originals backed up to
`backups/20260912-154029/`:

| track | before | after |
| --- | --- | --- |
| RED ROOM.m4a | 163 kbps / 4.57 MB | **275 kbps / 7.71 MB** |
| FRAI X JCB - FEEL GOOD.m4a | 163 kbps / 6.75 MB | **273 kbps / 11.3 MB** |

Match scores 100 and 96; both `encode-aac` from a lossless WAV under
`target_format: auto`, so the `.m4a` paths are preserved.

## Run 2 — widened allowlist (plan `20260912-160042`)

Re-planning after the allowlist fix moved the in-window (rows 1-48) counts from
`available:5 / unsupported_host:39 / no_free_dl:5` to:

| status | count |
| --- | --- |
| `available` | **15** |
| `unsupported_host` | 28 (all Bandcamp bar `found.ee`) |
| `no_free_dl` | 5 |

13 queued for capture (15 available minus the 2 already promoted). Download order
is `oldest_first`, so the run opens with `Nom Nom [Free DL] - Birthday Special` —
the track named as the batch boundary.

The generalized log line reads `gaterush.me gate detected for …` rather than the
old hard-coded `hypeddit gate detected`.

## Native downloads and `purchase_title` — two dead ends, checked

Before concluding that 28 in-window tracks are genuinely not upgradable, I
pulled the SoundCloud hydration payload for every `no_free_dl` and every
store/smart-link row (33 tracks).

**Native SoundCloud downloads: nothing to gain.** SoundCloud has a "Download
file" button of its own (`downloadable` / `has_downloads_left`) which the parser
ignores entirely — it only ever reads `purchase_url`. That looked like a missed,
gate-free upgrade path, especially for row 38 whose title literally says
`(FREE DL)`. It is not: row 38 is the only `downloadable: true` track in the
batch and its `has_downloads_left` is `false` (the artist's free-download quota
is spent). All five `no_free_dl` rows are correctly classified. Supporting
native downloads is still a reasonable future feature, but it would not have
upgraded a single track here.

**`purchase_title` cannot separate Buy from Free DL.** My earlier note claimed
it could. It cannot — it is artist-authored free text and null for 26 of 33
rows. Observed values: `Bandcamp`, `Support us`, `Support`, `Download`,
`Buy Vinyl & Digital`, `BUY HERE`, `Buy here`. A `Download` label sits on a
Bandcamp album (row 12) while `BUY HERE` sits on a `found.ee` smart link (row
23), so the label does not even agree with the destination. This confirms the
host allowlist is the right mechanism rather than a workaround.

## Fallback for gates completed out-of-band

The capture waits on `~/Downloads` only while it is sitting on a given track. If
a gate is clicked *after* the run moved on, the file lands in `~/Downloads` and
nothing picks it up.

No new code needed for that case — `promote-freedl` works on any directory pair:

```sh
bin/udl promote-freedl \
  --free-dl-dir ~/Downloads \
  --library-dir ~/Music/downloaded/sc-likes-today-09-12 \
  --target-format auto            # add --apply once the preview looks right
```

It matches on embedded metadata first (title/artist/source URL) and falls back to
the filename stem, skips anything below `--min-match-score 72` or inside the
`--ambiguity-gap`, and only replaces when the candidate's effective bitrate
actually beats the library file. Previewing first is the default.

## Verified: promotion does not lose artwork

Worth checking, because the captured free-DL files are bare WAVs with no cover
and they replace library files that do have one — a naive implementation would
strip artwork off the whole batch.

It does not. `runFFmpeg` in `internal/freedl/service.go` takes audio from the
free DL but metadata and cover from the *library* file
(`-map 0:a:0 -map_metadata 1 … -map 1:v? -c:v copy`, the video map skipped only
for `.wav` outputs). Confirmed on the two promoted files: cover survives as a
500x500 mjpeg stream, identical to the backup of the original.

This also makes the one remaining capture-time warning harmless:

```
metadata tagging warning: metadata written without artwork:
[wav @ …] wav muxer does not support any stream of type video
```

That is WAV genuinely being unable to carry cover art, and the no-artwork
fallback doing the right thing — not the ffmpeg ordering bug, which is gone from
run 2's log. Artwork for these tracks comes from the library file at promotion
time regardless. Noise, not a defect; left alone.

Also note the promotion path already ordered its ffmpeg args correctly
(both `-i` before any `-map`); only the capture-time tagging call was wrong.

## Run 2 result and where the task stands

13 gates opened, **1 captured** (`Emmanuel Messina - Pianolator`, 39 MB WAV —
the same track that timed out in run 1, so gates do succeed when clicked),
12 `free-dl-gate-timeout`, all recorded in
`logs/20260912-160042/upgrade-09-12.freedl-stuck.jsonl`.

Promoted in place, match score 100, originals backed up:

| track | before | after |
| --- | --- | --- |
| Emmanuel Messina - Pianolator [UNDRGRNDSPORTS].m4a | 163 kbps | **273 kbps** |

Note the captured filename was `Emmanuel - Pianolator [UNDRGRNDSPORTS].wav`
against a library file named `Emmanuel Messina - Pianolator …` — it still scored
100 because `buildPromotionAssignments` links through the capture state's remote
ID rather than relying on the filename.

### Batch totals (rows 1-48 = today's 46 files)

| outcome | count |
| --- | --- |
| upgraded to 273-275 kbps AAC from lossless | **3** |
| capturable, waiting on a gate click | 12 |
| not upgradable (24 Bandcamp, 1 found.ee, 5 no free DL, 3 other stores) | 33 |

The 12 remaining are a human step by design, not a defect: each is a gate page
whose unlock action (following/liking on SoundCloud, an email form) belongs to
the account owner. Two Hypeddit gates completed unattended early on because that
session already satisfied the follow requirement; the rest did not. This process
has neither Screen Recording nor Automation permission, so it cannot drive
Helium, and completing a fan-gate on someone's account is not an action to take
unprompted.

`.dev/freedl-0912/status.py` prints the live breakdown with the gate URL for each
pending track. After clicking any of them, `promote-freedl --free-dl-dir
~/Downloads` picks the files up without another capture run.

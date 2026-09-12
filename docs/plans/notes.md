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
  *Buy* button, not a free download. Genuinely not upgradable for free — the
  parser in `soundcloud_freedl_metadata.go` reads `purchase_url` without ever
  looking at the accompanying purchase *title*, so "Buy" and "Free Download"
  are indistinguishable downstream. Worth noting as a real gap: these should
  classify as "paid link", not "unsupported host".
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

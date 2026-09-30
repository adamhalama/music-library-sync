# September 12 Free DL upgrade results

Snapshot: 2026-09-30T10:16:35.738602+00:00. This report records the current checkpoint; one supported gate remains in progress.

The isolated library is `/Users/jaa/Music/downloaded/sc-likes-today-09-12` (46 tracks). Upgrades, backups and preservation repairs in this batch target that copy. The normal `~/Music/downloaded/sc-likes` library was not a promotion target.

| Outcome | Count |
|---|---:|
| Confirmed upgrades | 14 |
| Supported gates pending | 1 |
| No supported Free DL route | 31 |
| Full audio decodes passing | 46/46 |
| Tags, artwork and creation times preserved | 46/46 |

## Verified upgrades

Rates below measure the audio stream, excluding artwork and container overhead. The downloaded WAV originals are lossless; the installed AAC files are lossy encodes made from those originals. FUNKY TOWN retains the artist’s MP3 audio without another lossy encode.

| Track | Downloaded original | Before | Installed audio |
|---|---|---|---|
| CHULO | WAV, 16-bit | AAC 160.0 kbps | AAC 277.3 kbps |
| Emmanuel Messina - Pianolator [UNDRGRNDSPORTS] | WAV, 16-bit | AAC 160.0 kbps | AAC 270.0 kbps |
| FRAI X JCB  - FEEL GOOD | WAV, 24-bit | AAC 160.0 kbps | AAC 270.3 kbps |
| FUNKY TOWN (TOWLIE FLIP) | MP3 (artist file) | AAC 160.0 kbps | MP3 267.2 kbps |
| KØDA  - POP IT LIKE [UNDRGRNDSPORTS] | WAV, 16-bit | AAC 160.0 kbps | AAC 257.5 kbps |
| Nom Nom [Free DL] - Birthday Special | WAV, 16-bit | AAC 160.0 kbps | AAC 272.6 kbps |
| PIKETU | WAV, 24-bit | AAC 160.0 kbps | AAC 259.5 kbps |
| Paolo Doldo x DJ HOTMAIL - Sonic Boom (FREE DOWNLOAD ) | WAV, 24-bit | AAC 160.0 kbps | AAC 265.8 kbps |
| RED ROOM | WAV, 24-bit | AAC 160.0 kbps | AAC 272.0 kbps |
| WHO WANT SMOKE | WAV, 16-bit | AAC 160.0 kbps | AAC 276.9 kbps |
| ZiNØ x Shizeero - YOKAI | WAV, 24-bit | AAC 160.0 kbps | AAC 269.3 kbps |
| [FREE DL] KØDA  - FUMANDO MARIJUANA | WAV, 16-bit | AAC 160.0 kbps | AAC 267.1 kbps |
| [FREE DL] SMVGGLERS - SEX | WAV, 24-bit | AAC 160.0 kbps | AAC 274.4 kbps |
| chopstick420 - PRIME | WAV, 24-bit | AAC 160.0 kbps | AAC 278.3 kbps |

## Proof and preservation

Every upgraded row has a successful promotion or migration ledger and a changed encoded-audio hash. The before baseline uses the original backup referenced by the earliest successful ledger, rather than an already-upgraded metadata-repair copy. All 46 installed files passed a full decode. Their tags, embedded artwork and macOS creation times match their audited baselines. FUNKY TOWN intentionally changes extension from `.m4a` to `.mp3`; other paths retain their existing extension.

New browser intakes verify the exact saved remote ID, title, gate URL and isolated target, then verify download size/hash, complete decode and duration before staging by remote ID. Fresh single-track promotion runs preview one selected row and keep rollback backups. Downloaded originals and intake evidence remain under `~/dev/music-down/statefiles/freedl/upgrade-09-12/buffer` and `logs`.

Local evidence:

- [Full 46-track quality table](/Users/jaa/dev/music-down/statefiles/freedl/upgrade-09-12/quality-audit.md)
- [Machine-readable audit](/Users/jaa/dev/music-down/statefiles/freedl/upgrade-09-12/quality-audit.json)
- [Capture and promotion ledgers](/Users/jaa/dev/music-down/statefiles/freedl/upgrade-09-12/logs)
- [Original-file backups](/Users/jaa/dev/music-down/statefiles/freedl/upgrade-09-12/backups)

## Remaining work

Dex Fury — Oriental Bounce is pending at Hypeddit: its genuine SoundCloud actions are complete, but its hidden tab repeatedly times out before Spotify Connect. Future-song opt-out must be checked again after the last reload. CHULO and ZiNØ x Shizeero — YOKAI have completed their Droploud gates and verified promotions. Of the other 31 unupgraded tracks, 27 have an unsupported purchase/download host and four advertise no Free DL route in the saved capture plan. Their current files remain in the isolated library.

Refresh this checkpoint from the regenerated audit after additional promotions; do not reuse an earlier capture run for newly downloaded tracks.

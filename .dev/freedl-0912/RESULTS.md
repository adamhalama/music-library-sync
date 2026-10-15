# September 12 Free DL upgrade results

Final audit: 2026-09-30T13:39:11.723009+00:00. All 15 supported Free DL candidates have verified upgrades.

The isolated library is `/Users/jaa/Music/downloaded/sc-likes-today-09-12` (46 tracks). Upgrades, backups and preservation repairs in this batch target that copy. The normal `~/Music/downloaded/sc-likes` library was not a promotion target.

| Outcome | Count |
|---|---:|
| Confirmed upgrades | 15 |
| Supported gates pending | 0 |
| No supported Free DL route | 31 |
| Full audio decodes passing | 46/46 |
| Tags, artwork and creation times preserved | 46/46 |

## Verified upgrades

Rates below measure the audio stream, excluding artwork and container overhead. The downloaded WAV originals are lossless; the installed AAC files are lossy encodes made from those originals. FUNKY TOWN retains the artist’s MP3 audio without another lossy encode.

| Track | Downloaded original | Before | Installed audio |
|---|---|---|---|
| CHULO | WAV, 16-bit | AAC 160.0 kbps | AAC 277.3 kbps |
| Dex Fury - Oriental Bounce (Free DL) | WAV, 24-bit | AAC 160.0 kbps | AAC 284.9 kbps |
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

Portable proof for reviewers:

- [Full 46-track quality table](QUALITY.md)
- [Hashes and measured stream evidence](quality-evidence.json)

Local source evidence:

- [Full 46-track quality table](/Users/jaa/dev/music-down/statefiles/freedl/upgrade-09-12/quality-audit.md)
- [Machine-readable audit](/Users/jaa/dev/music-down/statefiles/freedl/upgrade-09-12/quality-audit.json)
- [Capture and promotion ledgers](/Users/jaa/dev/music-down/statefiles/freedl/upgrade-09-12/logs)
- [Original-file backups](/Users/jaa/dev/music-down/statefiles/freedl/upgrade-09-12/backups)

## Gate exception and unchanged files

Dex Fury — Oriental Bounce completed its native Hypeddit flow and verified promotion. Its Instagram link `dexfury_music` is unavailable: the exact profile returned Instagram’s unavailable-page message after reload, and the artist’s current public SoundCloud bio still advertises that same username. No Instagram follow was performed or claimed. The gate’s ordinary link click and Next action nevertheless allowed the flow to continue. Required SoundCloud actions and the Spotify OAuth callback were verified; optional future Spotify additions were disabled (`futureOptOut=0`). Public-link and unavailable-page evidence are retained in the scratch `automation` directory.

The other 31 tracks advertise no supported Free DL route in the saved capture plan: 27 use an unsupported purchase/download host, and four have no Free DL route. Their current files remain in the isolated library. These are outside the 15 completed supported candidates.

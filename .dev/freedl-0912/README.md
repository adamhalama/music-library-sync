# September 12 isolated Free DL batch

These scripts only modify `~/Music/downloaded/sc-likes-today-09-12` and use
`~/dev/music-down/statefiles/freedl/upgrade-09-12` for originals, backups and evidence.

- `python3 .dev/freedl-0912/audit.py`: probe every actual media file, fully decode audio,
  compare audio bitrate, tags and artwork hashes with its earliest backup, and write
  `quality-audit.json` plus `quality-audit.md` in scratch. Bitrate is audio-only;
  container size including artwork is not used as a quality measurement.
- `preserve.py restore-tags BACKUP TARGET`: restore the entire original MP4 tag set
  including unknown iTunes freeform fields. Verify compressed audio and artwork hashes,
  all ffprobe-visible original tags, and decoding before replacement. Backup first.
- `preserve.py install-mp3 SOURCE EXISTING_AAC`: explicitly migrate a verified artist
  original to `.mp3`, stream-copying its encoded audio without lossy transcoding. Copy
  existing metadata/artwork, verify duration (within two seconds), compressed audio
  hash and decoding, back up the AAC, then install MP3 and remove old AAC. The caller
  must establish source identity/provenance; duration alone is not proof of identity.
- `status.py` incorporates these logged migrations so extension changes retain the
  original track identity and do not disappear from the batch count.

Requirements: Python 3, ffmpeg/ffprobe. MP4 tag preservation also uses Mutagen:

```sh
python3 -m pip install --target ~/dev/music-down/statefiles/freedl/upgrade-09-12/pythonlib mutagen==1.48.1
```

FFmpeg `-map_metadata` drops the original MP4 `WWWAUDIOFILE` freeform atom; ExifTool
`-TagsFromFile -all:all` also failed to preserve it. Mutagen transfers the entire MP4
`ilst` metadata including `purl`, freeform tags and cover artwork without re-encoding.
This is a batch maintenance dependency, not an added application dependency.

Every mutation writes `logs/<timestamp>-<action>/preservation-result.json` with
before/after probes, file hashes, encoded audio hash, backup and source information.
Filesystem creation time is retained on macOS, including when the extension changes.
The 2026-09-29 metadata repair also restored the four previously upgraded tracks'
creation times from their earliest backups.

The audit counts an upgrade only when a successful promotion/migration ledger entry
exists **and** the encoded audio differs from its backup. A metadata-only change
cannot inflate that count. `changed_audio` is reported separately, and the table
checks creation-time preservation explicitly.

Mutations first persist `transaction.json` with backup/target paths. A successfully
written `preservation-result.json` is the commit marker, and records the installed
file's actual creation time. An exception during installation or final evidence
writing restores the backup and removes the migrated output. A process/machine
crash can leave a prepared transaction without a commit marker; inspect its paths
and backup before resuming. Run isolated regression tests with:

```sh
python3 .dev/freedl-0912/test_quality.py
```

`promote.py CAPTURE_RUN --apply` automatically repairs each successfully replaced
row using its native backup/library paths, including successes in partially failed
runs. It checks Mutagen availability before applying, reports missing backups or
repair failures with a nonzero exit, and always closes its agent. Native promotion
hashes describe the pre-repair output; the separate preservation ledger records
the final metadata-restored file. Tag restoration uses the backup's creation time,
so it also corrects native promotion timestamp drift.

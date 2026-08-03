# Rekordbox playlist-sync recovery

Every non-dry-run playlist-sync apply creates a complete timestamped copy of
the configured Rekordbox database directory before the bridge is allowed to
write. The apply result reports the exact `backup_path`; retain that value with
the run diagnostics.

## Restore procedure

Never restore while Rekordbox or a process whose name contains `rekordbox` is
running.

1. Quit Rekordbox and confirm the UDL process guard reports no matching
   process.
2. Record the configured database directory and the apply result's exact
   `backup_path`. Do not infer or glob either path.
3. Move the current database directory to a separately named quarantine
   location on the same volume. Do not delete it.
4. Copy the complete backup directory to the original database-directory
   path, preserving metadata. On macOS, use `ditto <exact-backup-path>
   <exact-database-path>`.
5. Run read-only Rekordbox inspection from UDL before opening Rekordbox.
6. Open Rekordbox and verify the affected playlists and a representative set
   of tracks.
7. Keep both the quarantine directory and backup until the restored library
   has been used successfully. If verification fails, quit Rekordbox before
   changing either directory again.

## Release validation

Use an isolated database copy, never the live library:

1. Copy a fixture/test database directory and configure both `db_dir` and
   `backup_dir` under a temporary validation root.
2. Generate and retain a checksummed plan.
3. Apply it and record the reported backup path.
4. Verify the intended playlist mutation.
5. Follow the restore procedure above.
6. Compare the restored directory with the pre-apply fixture and record the
   result in the active `IMPLEMENTATION.md` tracker.

The validation is incomplete until both the apply and restore have succeeded
on the isolated copy.

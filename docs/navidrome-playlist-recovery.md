# Navidrome paired-playlist recovery

Every non-dry-run paired-playlist apply creates a verified Navidrome database
backup before replacing a Navidrome destination. UDL prints and returns that
exact backup path. A no-op or dry run creates no backup.

If apply returns partial/uncertain status, do not repeat the saved plan. The
mutation request may have reached Navidrome even when its response did not.

1. Record the reported backup path and error.
2. Run `udl playlist sync plan` again in the same direction. The new plan reads
   live membership and order and safely reveals whether the intended write
   landed.
3. If live state is correct, apply the fresh no-op plan to record verified pair
   state. If it is not correct, review and apply the fresh diff.
4. Restore the database backup only when the destination must be rolled back.
   Stop Navidrome first, preserve the current database for investigation,
   replace it with the exact reported backup using the service's existing
   backup/restore procedure, restart the service, and generate a fresh plan.

Never restore a backup while Navidrome is writing its database, and never use a
backup from a different apply merely because its timestamp looks close. UDL's
pair state advances only after exact readback verification, so a missing or old
state file is not evidence that the provider was unchanged.

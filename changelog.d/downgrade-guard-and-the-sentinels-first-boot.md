### Fixed

- **Starting an older Ovumcy against a database a newer one wrote is refused instead of passing
  unnoticed.** Migrations are forward-only, and the runner only ever looked for migrations it had
  yet to apply — so a rolled-back image found nothing to do, said nothing, and served requests
  against columns and conventions it does not know about, writing rows the next upgrade then had to
  reconcile. A start whose ledger records migrations this binary does not carry now ends before any
  statement runs, naming those versions, the newest one this binary knows, and the two ways out
  (start the newer release again, or restore a backup taken from before the upgrade). The check
  lives in the binary being started, so it covers every downgrade from this release onward; going
  down to a release that predates it stays silent, and the runbook says so.

- **The first start after upgrading no longer adopts a legacy calendar feed as its baseline.** Feeds
  armed before the keyed verifier MAC landed (pre-migration-032 rows) verify through bcrypt, which
  does not depend on `SECRET_KEY`, and nothing in such a row records which secret minted it. The
  boot sentinel treated an absent stored key epoch as "new installation", recorded the current epoch
  beside those rows and left them armed — so the first poll backfilled a MAC under today's key, and
  a subscribe URL that a `SECRET_KEY` rotation in that same maintenance window was meant to kill
  came back to life. That branch now runs the same disarm as a detected rotation before it records
  anything: the legacy rows are cleared and their owners generate a fresh subscribe URL from
  Settings, while a genuinely new installation reaches the branch with nothing armed and is
  unaffected. The startup log names the count when it happens.

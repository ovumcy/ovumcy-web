none

Internal only: `scripts/backuprestoredoc` extended to pin the two erasure
passages finding DB-4 named — `docs/self-hosted.md`'s Post-Restore
Verification step 8 and `docs/gdpr.md`'s "Backup Restore and Erasure" section —
which until now were guarded only by the calendar-feed cross-reference beside
them. Deleting or hollowing out either passage previously reddened nothing.

`TestPostRestoreVerificationPointsAtTheErasureNote` follows the existing
calendar-feed test's shape (`TestPostRestoreVerificationPointsAtTheCalendarFeedNote`):
both ends of the checklist's cross-reference to `gdpr.md#backup-restore-and-erasure`
are checked, and on top of that each passage's load-bearing claims are pinned
as short, distinctive fragments rather than a whole-paragraph quote — the
restore-resurrects-the-erasure claim, the no-tombstone claim explaining why
that is invisible from inside a restore, and the re-apply-manually remedy, on
both the runbook and the gdpr.md side. `runbookSectionText` is now backed by a
new `documentSectionText(t, docPath, heading)` helper so the same
section-extraction idiom works against `docs/gdpr.md`, not only
`docs/self-hosted.md`.

Proven red on defect: deleting the `self-hosted.md` step reddens on the
missing link and all three runbook-side claims; deleting the `gdpr.md`
section reddens on the missing heading; keeping the `gdpr.md` heading but
replacing its body with generic prose (a hollow-out, not a deletion) reddens
on all three gdpr.md-side claims while the heading/link checks stay green.
Restored afterward; `git diff` against the doc files is empty.

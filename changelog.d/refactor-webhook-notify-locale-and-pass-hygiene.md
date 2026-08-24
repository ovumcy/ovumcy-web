### Fixed

- **Webhook reminders and the calendar feed now speak the owner's language.** Both surfaces that
  carry predictions off the instance resolved the mandatory "estimates, not medical advice or a
  method of contraception" disclaimer at the server default, and the webhook headline and sentence
  were English literals with no catalogue entry at all — so an owner on one of the five non-default
  locales received a notification written in two languages, neither of them necessarily theirs. The
  notify pass and the `.ics` feed now resolve every localized field at the owner's chosen interface
  language, and the reminder copy lives in all six locale catalogues.

### Internal

- **One URL-hostname redaction helper, not two.** The settings display carried its own byte-identical
  copy of the "hostname and nothing else" rule that the notify pass and the CLI print through; a
  hardening of what is safe to show now reaches every surface at once.
- **The notify pass decides once per owner.** Its idempotency counter was derived by re-running the
  whole reminder decision with the watermarks cleared, which rebuilt the owner's cycle statistics
  from their entire logged history a second time; one traversal now yields both the due set and the
  count.
- **A locale sweep for the day-field label functions.** Every value of every day-field enum is fed
  through its translation-key function and required to resolve in all six catalogues, closing the
  gap the existing map-based sweep could not cover: those keys come from `switch` statements, so a
  typo reached every language as a raw key on screen.

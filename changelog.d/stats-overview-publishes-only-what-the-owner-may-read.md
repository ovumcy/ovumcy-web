### Changed

- **Breaking (API shape): `GET /api/v1/stats/overview` now publishes an explicit payload, and it
  withholds every prediction the app itself withholds.** The endpoint used to serialize the internal
  statistics object as it stood, so the ovulation date, the fertile window and the next-period
  estimate went out over JSON even in the states where Ovumcy has decided it must not show them — an
  unpredictable-cycle account, an active pregnancy pause, a cycle running more than a week past its
  own reference length, or an account with no completed cycle yet, where the fertility figures come
  from the onboarding cycle-length setting rather than from anything recorded. The pages and the
  calendar feed already withheld them; the JSON view did not, and a suppression one surface stands
  in front of is not a suppression.

  What changes for a consumer of that endpoint:

  - Dates are calendar days (`YYYY-MM-DD`), no longer timestamps with a time and an offset. A date
    that is withheld or unknown is `null` rather than a zero date.
  - A new `suppression` object says whether predictions are withheld (`predictions`), whether the
    fertility half specifically is (`fertility`), and why (`reasons`: `unpredictable_cycle`,
    `pregnancy_pause`, `cycle_overdue`, `awaiting_first_cycle`). A withheld date is a decision the
    payload names, not a gap to guess at.
  - `disclaimer` carries the medical-safety line in the account's language, with `disclaimer_key`
    beside it for clients that would rather branch on a stable key than on translated text.
  - The field set is now fixed and documented in `docs/openapi.yaml`, which lists every field
    instead of declaring the payload open-ended. A field added to the internal object no longer
    reaches the API on its own.

  Recorded history — the observed cycle lengths, the last period start, the current cycle day — is
  published in every state, exactly as before: it is fact rather than projection, and nothing
  suppresses it.

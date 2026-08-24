### Fixed

- **A malformed mood in a form-encoded day save is refused instead of clearing the recorded value.** `mood=abc` used to be substituted with zero — "no mood recorded" — so a malformed update erased a mood the owner had saved and still answered 200. It now answers 400, exactly as the same value in a JSON body always did. An absent or empty field still means "no mood", which is what an unchecked radio posts.
- **A calendar page failure no longer reports that the stats load failed.** The calendar error mapper named only one of its two sentinels, so a stats-load failure and any future failure both inherited the message of the other one. Both sentinels are named now, and an error the mapper does not know answers with its own key.

### Internal

- **A cycle-start mark whose implantation policy could not be resolved says so in the audit stream.** The caution stays suppressed — the safe direction for a prediction claim — but the mark's existing `health.cycle_start_mark` line now carries `cycle_start_policy="unresolved"`, so an operator can tell a caution that was not warranted from one that could not be computed.
- **A flash cookie that cannot be written reports it.** The encode and sealed-write failures were both discarded, so a user could be redirected after an error with no explanation and no operator signal. Each now emits one always-on diagnostic naming the reason, never the payload (`docs/security/logging.md`).
- **Four test guards tightened.** The calendar mapper has an exhaustiveness barrier derived from the producer's own sentinels; the template walker sees chained calls (`{{ (dict "k" .V).k }}`) and compares its walk against the embed rather than a hand-maintained floor; the recovery-code regeneration regression compares the owner's whole settings row instead of two sentinel columns; and the settings form helper closes the response body it opens.

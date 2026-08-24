### Fixed

- **The two-factor settings page no longer shows a raw translation key.** An account with TOTP
  enabled saw the literal text `settings.2fa.status_enabled_hint` under the "Two-factor
  authentication is enabled" heading, in all six languages, because no catalogue defined that key
  while the page rendered it. The hint is now translated in English, German, Spanish, French,
  Italian and Russian, and reads: signing in requires a code from the authenticator app in addition
  to the password.
- **A read-only day summary no longer shows a raw translation key in place of the symptom
  placeholder.** The same defect class: `dashboard.owner_only_details` was rendered by the read-only
  log summary and defined by no catalogue. It is now translated in all six languages.
- **The English stale-baseline notice on the statistics page reads as a sentence.** It said "Phase
  shown as estimate due stale baseline."; it now says "Phase is shown as an estimate because the
  baseline is out of date." English only — the other five languages already carried a full sentence.

### Internal

- **`dashboard.fertile_window` was declared twice in every locale file; the first declaration is
  removed.** `json.Unmarshal` into a map keeps the last member of a duplicated name, so the
  surviving declaration is the one the loader was already using and nothing renders differently.
- **Three locale guards.** The suite now fails when a shipped template passes `t` or `tn` a literal
  key the English catalogue cannot answer (a blank value counts as unanswerable, matching the
  lookup the templates go through); when any embedded locale file declares the same key twice, read
  as a JSON token stream rather than unmarshalled into a map, which cannot see a duplicate at all;
  and when the seven validator sentences shared by the onboarding step-2 form and the cycle settings
  card fall out of step, or lose one half, in any locale.

### Internal

- **Transport-layer duplication swept out of `internal/api`, with three sweeps to
  keep it out.** A tagless switch that classifies domain sentinels may no longer
  carry a default arm identical to one of its own cases (16 sites folded, from
  `mapDayDeleteError` — whose entire switch had one case byte-identical to its
  default — to the auth, symptom, export, recovery and password mappers); a call
  site that wants its own fallback for a missing translation now asks the new
  two-value `lookupMessage` instead of comparing `translateMessage`'s result to
  the key it just passed in (19 sites, six of which spelled the key a second
  time as a literal); and the shared test helpers reach the app secret and the
  auth-cookie name through their constants rather than through a second copy of
  the value. Four duplicated or vacuous test cases were removed or re-pointed:
  the privacy-route test now builds its app with the shared recipe instead of a
  divergent one, the partial-template test asserts the one define `base.html`
  actually declares, the onboarding picker test derives its 24 expected strings
  and its locale list from the i18n catalogue, and the settings-symptoms HTMX
  test now reads the mutated row back out of the rerendered section. No rendered
  string, status code or error key changes.
</content>

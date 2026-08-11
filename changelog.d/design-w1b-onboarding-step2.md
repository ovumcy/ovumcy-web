### Changed

- **Onboarding step 2 stops asking for an age bracket, and the mode question is now skippable in
  one visible step.** The step collected two answers it described as optional; only one of them
  frames the product. The age question leaves onboarding entirely — it stays available under
  Settings → Cycle, and onboarding no longer writes that column at all, so a bracket chosen in
  settings can never be overwritten by finishing the flow. The mode question ("Avoid pregnancy",
  "Trying to conceive", "Track my health") stays on the screen, keeps "Track my health"
  preselected as the neutral default, and gains a plain "Skip this question" button next to the
  choices instead of a footnote. Skipping submits no answer and completes onboarding on the
  neutral default. The three-line note explaining that the prediction algorithm is identical no
  longer appears during onboarding; the dashboard and stats pages still carry it where the mode is
  displayed.
- **The current mode can be changed from the dashboard.** The "Current mode" panel now offers the
  two modes the account is not in as one-click chips, so switching no longer means a trip into
  settings. A chip saves only the mode — every other cycle setting is left exactly as it stands —
  and the page re-renders in the new framing. The mode is never changed for the owner: nothing
  logged on a day, a positive pregnancy test included, rewrites it.

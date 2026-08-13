### Fixed

- **The mood- and symptom-by-phase cards on Insights draw their phase icon
  again.** Both cards passed the phase icon's *name* to the page instead of the
  icon, so each phase heading read "drop Menstrual" or "sprout Follicular", with
  the English name showing through in every language. They now render the same
  sprite icon as the rest of the page, and a template guard fails the build if
  any surface reaches for a phase icon without drawing it.

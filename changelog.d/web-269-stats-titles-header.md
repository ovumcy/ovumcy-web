### Fixed

- **Stats progress meters are named for screen readers, and the two-factor pages carry the app name
  in their title.** The cycles-progress bar and each phase mood bar on Statistics now point at their
  visible label. The two-factor settings and sign-in pages read "Ovumcy | …" like every other page.
  The security headers no longer list `ambient-light-sensor`, a feature browsers have dropped, which
  logged a console warning on every page.

none

CI reliability change with no user-visible effect: the browser lanes no longer
install system packages through apt. Every real Playwright dependency is
already present on the runner image, and the only packages the step added were
fonts for scripts this application does not ship.

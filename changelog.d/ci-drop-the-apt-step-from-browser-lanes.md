none

CI reliability change with no user-visible effect: the two Chromium-only
browser lanes no longer install system packages through apt. Every real
Playwright dependency for Chromium is already present on the runner image, and
the only packages the step added were fonts for scripts this application does
not ship. The cross-browser lane keeps its apt steps: Firefox and WebKit need
real system libraries there, not fonts.

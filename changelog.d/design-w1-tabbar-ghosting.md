### Fixed

- **The mobile tab bar no longer shows page content through itself, and the footer link below it is
  reachable again.** The floating bar painted a translucent background (`alpha 0.96` in light,
  `0.95` in dark) with no backdrop filter, so text scrolled visibly through it on dashboard,
  calendar and insights. It is opaque in both themes now. The bottom clearance the layout intended
  was also inert: the footer's `padding-bottom` lost to the utility class on the same element, which
  left the privacy link 34px inside the bar at the bottom of the page on a 390x844 viewport.

### Fixed

- **The desktop navigation marks Settings as the current section again.** After the
  duplicate gear icon was removed, the account chip was left as the only desktop
  route to settings but did not take over the active highlight, so on `/settings`
  the desktop navigation marked nothing while Today, Calendar and Insights each
  marked their own page. The chip now carries the same active treatment and
  `aria-current="page"`; the bottom tab bar stays the single marking on a phone.

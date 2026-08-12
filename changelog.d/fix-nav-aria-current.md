### Fixed

- **Every navigation entry says which page you are on.** Today, Calendar, Insights and the
  bottom bar's settings icon marked the current section with a fill and a weight only, so a
  screen reader read the entries out with nothing to distinguish the page already open. The
  desktop section links and the mobile tab bar now carry `aria-current="page"` alongside that
  fill, exactly one entry per navigation.

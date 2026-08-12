### Changed

- **The interface now has a design system instead of per-component decisions.** Semantic
  surface/ink/border/accent tokens carry both themes, type sizes follow one scale with a real
  display size, radii collapse to a card radius, a control radius and the pill, shadows to three
  elevation levels, and every transition runs on one of three durations with one shared easing.
  Sizes and corner radii shift by a pixel or two in places; a selected but disabled chip in the
  dark theme no longer paints a light-theme cream fill under light text.

### Internal

- **The clearance rule under the mobile tab bar that never applied is gone.** It declared
  `padding-bottom` on the element that also carries the `py-8` utility, and the equal-specificity
  tie has always resolved to the utility. The footer clearance is what meets the bar at the
  document bottom; the neighbouring scroll-padding and scroll-margin rules keep working.

### Fixed

- **The dashboard cycle ribbon's predicted-flow hatch and start-window gradient are now
  readable in both themes.** They previously reused the calendar's warm accent colour, which sat
  at nearly the same lightness as the phase fill they are drawn over (measured 1.00-1.02:1 for the
  hatch, 1.20-1.22:1 for the gradient's dark-theme 'beyond' case — both well under the WCAG 3:1
  floor for a meaningful non-text graphic). Both overlays now use a neutral stripe calibrated
  against what they actually paint over (4.61:1 / 3.57:1 for the hatch on a phase fill; 5.77:1 /
  7.50:1 for the gradient on the dark 'beyond' track).

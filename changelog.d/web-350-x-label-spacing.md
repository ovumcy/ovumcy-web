### Fixed

- **Stats chart date labels no longer overlap mid-axis on a narrow screen.** The chart checked only
  the last two labels against each other, so a long label in the middle of the axis could print over
  its neighbours at phone widths. Every neighbouring pair is now measured, and the labels thin out
  until none of them touch.

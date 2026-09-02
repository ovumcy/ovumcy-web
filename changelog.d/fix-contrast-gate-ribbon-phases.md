### Internal

- **The cycle-ribbon contrast gate now measures all four phases, and sees `background-image`.**
  `theme-dark-mode.spec.ts` checked only the `menstrual` phase, and `measureGraphicContrast` read
  only `background-color` — the predicted-flow hatch and the start-window gradient are both painted
  as `background-image` on top of the phase fill, so a cell carrying either flag was measured as if
  it did not. The independently recomputed ratios were already correct (light theme: menstrual
  3.39:1 and ovulation 3.61:1 clear the 3:1 floor by design; follicular 1.71:1 and luteal 2.32:1 sit
  under it deliberately, per the sanctioned exception documented beside the phase colours in
  `input.css`; dark theme clears the floor on all four) — the gap was in the tool, not the picture.
  A new `'background'` mode on `measureGraphicContrast` resolves the graphic's own
  `background-color` and `background-image` stops together; the spec now measures a clean (no
  overlay) cell of each phase and keeps the same pass/exempt split the design already carries.

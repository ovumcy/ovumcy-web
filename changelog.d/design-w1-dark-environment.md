### Added

- **The theme gains a "System" option that follows the browser.** Settings offers it beside Light
  and Dark. It is resolved through `prefers-color-scheme` at apply time and keeps following it
  live, so a system that switches to dark at sunset reaches a page that is already open, not only
  the next one. `html[data-theme]` keeps carrying the resolved `light`/`dark`, so every rule
  written for those two still applies, and an owner who already saved Light or Dark keeps it.

### Fixed

- **The dashboard cycle ring is visible in the dark theme again.** Its track — the rest of the
  cycle the coloured phase segments are drawn on — measured **1.34:1** against the card behind it,
  where WCAG 1.4.11 puts a meaningful graphic at 3:1, so the ring dissolved into the card in the
  dark. It measures 3.38:1 at the card's light end and 3.95:1 at its dark end now, still far below
  the phase segments (5.4:1–7.2:1) so it stays the backdrop rather than data.
- **The dark cycle card no longer carries a pale smudge in its top-right corner.** Two decorative
  corner glows kept their warm light-theme paper tints on the dark card, lifting it by up to
  **3.27:1** and 1.71:1 — a decoration painted as loudly as a meaningful graphic, which is why the
  brighter one read as a render artifact. The dark theme drops both; the light theme keeps them at
  the 1.09:1–1.13:1 they were drawn for.

### Internal

- **The cold-start theme claim is now measured rather than asserted in a comment.** A browser
  check opens a page with a dark theme already stored and compares the moment `html[data-theme]`
  is written against the browser's own first-paint entry; a structural check pins the bootstrap
  script ahead of the stylesheet and free of `defer`/`async`. Both pass as they stand — no flash
  was found — and stay as the guard against a reordering that would introduce one.

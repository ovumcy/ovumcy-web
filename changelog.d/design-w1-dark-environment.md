### Added

- **The theme gains a "System" option that follows the browser.** Settings offers it beside Light
  and Dark. It is resolved through `prefers-color-scheme` at apply time and keeps following it
  live, so a system that switches to dark at sunset reaches a page that is already open, not only
  the next one. `html[data-theme]` keeps carrying the resolved `light`/`dark`, so every rule
  written for those two still applies, and an owner who already saved Light or Dark keeps it.

### Fixed

- **The dashboard cycle ring was made visible in the dark theme again — since superseded.** Its
  track — the rest of the cycle the coloured phase segments were drawn on — measured **1.34:1**
  against the card behind it, where WCAG 1.4.11 puts a meaningful graphic at 3:1, so the ring
  dissolved into the card in the dark; this pass raised it to 3.38:1/3.95:1. Wave 4 later replaced
  the ring itself with a cycle ribbon — one cell per day carrying the phase, the fertile window and
  entry marks along the band — which carries its own contrast: only the period and ovulation cells,
  the two a reader looks for, are held to the 3:1 floor. The recessive follicular/luteal pair is
  deliberately exempt against the light card, where it sits under the floor; on the dark card it
  happens to clear it anyway. The ribbon as a whole is redundant to the day marks and status text
  around it, which is why the floor is scoped to those two cells at all. This ring's numbers
  describe a track this release no longer contains.
- **The dark cycle card no longer carried a pale smudge in its top-right corner from the
  light-theme glow tints.** Two decorative corner glows kept their warm light-theme paper tints on
  the dark card, lifting it by up to **3.27:1** and 1.71:1 — a decoration painted as loudly as a
  meaningful graphic, which is why the brighter one read as a render artifact. This pass dropped
  both from the dark theme; a later pass gave dark its own tinted pair instead, so the corner is no
  longer bare there either — only the light-era light tint this fix removed stays gone. The light
  theme keeps its original pair at the 1.09:1–1.13:1 they were drawn for.

### Internal

- **The cold-start theme claim is now measured rather than asserted in a comment.** A browser
  check opens a page with a dark theme already stored and compares the moment `html[data-theme]`
  is written against the browser's own first-paint entry; a structural check pins the bootstrap
  script ahead of the stylesheet and free of `defer`/`async`. Both pass as they stand — no flash
  was found — and stay as the guard against a reordering that would introduce one.

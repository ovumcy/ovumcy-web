### Fixed

- **The dashboard cycle ribbon is no longer invisible to a screen reader.** It carried
  `aria-hidden="true"` on the premise that every fact it draws is stated in the status line above
  it, but two per-day facts — which days fall in the predicted start window and which were actually
  logged — exist only in the ribbon, with no textual equivalent anywhere else on the page. Each day
  cell now carries its own accessible name (`role="img"` plus a computed `aria-label`: cycle day,
  phase, and start-window/logged when they apply) instead of the ribbon being hidden outright. Also
  dropped `data-projected`, a ribbon-day attribute nothing ever read.

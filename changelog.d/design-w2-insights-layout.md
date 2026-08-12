### Changed

- **On Insights, a card is as tall as what it holds, and section titles share one scale.** The
  "Top symptoms in your last cycle" card was stretched to the height of the BBT chart beside it, so
  a single "no data yet" line sat in 435px of empty card — the card now ends where its content ends
  (measured in the browser: same row, same top edge, its own height). The page also carried three
  competing title treatments: the small uppercase stat label, the section heading, and the form
  field label pressed into service as a heading. Section titles are now one treatment — "Recent
  cycle context" becomes a real subheading under "Recent cycle factors" — the uppercase label is
  left to the stat cards it belongs to, and the labels inside pattern, phase and cycle cards read
  as emphasized text rather than as a fourth heading level. No wording changed, and the page keeps
  its single `h1` with valid heading nesting.

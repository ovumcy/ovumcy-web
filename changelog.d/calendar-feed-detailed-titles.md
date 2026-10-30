### Added

- **The calendar feed can name what each event is, if you ask it to.** Every `.ics` event used to
  carry the same neutral title, `Ovumcy: reminder (estimate)`, so a subscribed calendar could not
  tell an estimated period from an estimated ovulation day (#1159). Generating or rotating the feed
  link now offers a checkbox, off by default, that switches the titles to `Period (estimate)` and
  `Ovulation (estimate)` in your interface language. The choice belongs to that link: rotating
  asks again, pre-filled with the current choice.

  Leaving the box unticked keeps the feed byte-for-byte as it was. Ticked, only the titles
  change: the same estimated days are published, every title keeps the estimate qualifier, each
  event still carries the medical-safety disclaimer, and every case where the app withholds
  predictions still empties the feed. The settings card warns that your calendar provider, and
  anyone you share that calendar with, will see the titles, and the egress ledger names the mode
  your current link sends. Upgrading applies migration 043, which adds the opt-in column with every
  existing feed left neutral.

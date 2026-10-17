### Fixed

- **Settings deep links now land with the section title in view.** Opening `/settings#settings-cycle`
  (the dashboard's "update cycle data" button), the privacy page's ledger link, the account chip or
  any pill of the section index scrolled the card's top edge to the top of the viewport — under
  the sticky header and, from 640px up, the sticky section index, about 220px of chrome on a
  desktop. Every settings card now carries a `scroll-margin-top` that clears the header on a phone
  and the header plus the index on wider screens. The index wraps to as many rows as the locale and
  width need (three in Russian on a tablet) and the header wraps with text zoom, so both heights
  are measured and kept current by the settings script; without JavaScript a default applies.
  The index's sticky offset and the cards' clearance now read one shared value, so the two cannot
  drift apart.

### Security

- **A calendar feed event's identifier no longer names the cycle phase.** Each `.ics` event's
  `UID` read `period-…`, `ovulation-…` or `…-window-…` followed by the date, which calendar apps
  show in event details and exports even though the title stays neutral. The `UID` is now an
  opaque value keyed per owner from `SECRET_KEY`, still stable from one poll to the next. After
  upgrading, a subscribed calendar app replaces every Ovumcy event once: the old events disappear
  and new ones appear, and some apps briefly show both until their next refresh. Alarms or notes
  added to an Ovumcy event in the calendar app do not carry over. Rotating `SECRET_KEY` renames
  the events the same way, and it already requires re-subscribing to the feed.

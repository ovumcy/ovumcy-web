### Fixed

- **The long-period warning is now counted over calendar days in time zones
  whose clock jump lands on midnight.** The backward walk that measures how many
  consecutive days the current period has run stepped its cursor inside the
  request time zone. In zones west of UTC where daylight saving starts at 00:00
  (America/Santiago, America/Havana) that step skipped the transition date
  entirely: a continuous period spanning it counted one day short, so a nine-day
  period stayed one day below the threshold and the "period running long" notice
  never appeared. When the transition date carried no period at all, the walk
  jumped that gap instead of stopping there and merged two separate periods, so
  the notice was anchored to — and acknowledged against — the previous period's
  start date. The walk now steps over calendar days, visiting each exactly once.
  Zones without such a transition, including UTC, are unaffected.

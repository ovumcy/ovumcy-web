### Added

- **The baseline `docker-compose.yml` now lists the five settings it was omitting.**
  `RATE_LIMIT_CALENDAR_FEED_MAX` (`20`), `RATE_LIMIT_CALENDAR_FEED_WINDOW` (`1m`),
  `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` (`false`), `REMINDER_SCHEDULER_ENABLED` (`false`) and
  `REMINDER_SCHEDULER_HOUR` (`9`) are read at boot and documented as supported knobs, but reached
  the container only through `.env`, unlike every other knob the file spells out with its default.
  Each new entry carries that same loader default, so a stack started from the baseline behaves
  exactly as before.

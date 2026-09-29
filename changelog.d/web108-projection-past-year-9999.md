### Fixed

- **A projection that would fall after 9999-12-31 is reported as absent.** A cycle logged in late 9999
  projected its next period start, ovulation day and fertile window into year 10000, which no date
  field can spell: `GET /api/v1/stats/overview` could not answer it as a `format: date`, and the
  calendar feed wrote a nine-digit DTSTART. Such a date is now `null` in the API, exactly like a
  withheld projection, with `suppression` unchanged; a fertile window whose last day crosses the year
  is `null` as a whole. The dashboard, the webhook reminders, the calendar grid and the calendar
  feed leave the same dates out: the grid withholds such a window with its pre-fertile lead-in and
  marks nothing on the year-10000 days that close its last week, and the feed also drops an event
  on 9999-12-31, whose end date would fall in year 10000. Accepted day input is unchanged.
- **The 2FA enrollment docs name the status a wrong code gets.** `docs/openapi.yaml` listed `400`
  for `PUT /api/v1/users/current/2fa`; the server answers a wrong, missing or malformed code with
  `401` `totp invalid code`, and keeps `400` for a missing password. The spec now says so.

### Fixed

- **Breaking (API shape): a date outside 1900-01-01 through 9999-12-30 is now answered `400`
  `invalid date` instead of being accepted.** `GET /api/v1/days/0001-01-01` used to answer `200` with
  an empty `date`, and `9999-12-31` built a read range ending in year 10000 that cannot be encoded or
  ordered in the database. The bound applies to every date input: the `{date}` path of the day routes
  (`GET`, `HEAD`, `PUT`, `DELETE`, `cycle-start`), the `from` and `to` bounds of `GET /api/v1/days`
  and of the exports, and the dates in an import file, where such a row is counted as rejected and the
  rest of the file is imported. A client that reads, writes or deletes a day dated before 1900 has to
  stop asking for it; a whole-account export without `from`/`to` still includes every stored day.

### Changed

- **A request refused by the CSRF check now answers under its own error key, `csrf_refused`.**
  Until now it carried the shared `forbidden` key, the same one the role guards use, so a client
  could not tell a stale token from an account that may not do something. The status (403) and the
  `error_detail.category` (`forbidden`) are unchanged, so a client that branches on those is
  unaffected; a client that matched the `forbidden` key to detect a CSRF refusal must match
  `csrf_refused` instead. The HTMX and page-form refusals carry the new stable key as
  `data-flash-key="common.error.csrf_refused"`, with copy in all six languages that tells the owner
  to try again first and to reload the page only if the retry is refused too: the page refreshes its
  token after the refusal, but a form sent without JavaScript does not, and its retry from a page
  left open is refused again. `docs/openapi.yaml` names the new key on every operation that
  documents the CSRF refusal.
- **After a CSRF refusal the page re-reads its token on every htmx control, not only the day
  writes**, so the "try again" the notice offers can succeed. A 403 whose body names no key, such as
  a proxy's own error page, is treated the same way and shows the refusal copy rather than "check
  your connection". The calendar editor's delete and cycle-start controls, which have no status
  area, now say so in a toast instead of staying silent.

### Fixed

- **The calendar's account-changed notices are announced to screen readers.** They were inserted as
  new nodes already carrying a live role, which is not announced; the text is now written into a
  live region the calendar page renders up front. The region is emptied at once and filled in a
  later task, so a notice repeating the previous one is announced again.
- **A refused calendar grid refresh has its own message** ("This calendar is out of date: ...")
  instead of reusing "This day can't be opened", in all six languages.

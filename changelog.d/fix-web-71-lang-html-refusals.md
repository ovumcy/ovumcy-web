### Fixed

- **The language-switch form's refusals stay readable.** `POST /lang` used to answer a blank
  language and a rejected CSRF token with a raw JSON envelope painted into the browser window when
  submitted as a plain page navigation. Both now answer that caller with the same localized error
  message the form's other refusals (rate limit, failed save) already show; JSON and HTMX callers
  are unaffected.

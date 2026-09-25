### Fixed

- **The language-switch form's refusals stay readable.** `POST /lang` used to answer a blank
  language, a rejected CSRF token, a server fault or an expired request budget with a raw JSON
  envelope painted into the browser window when submitted as a plain page navigation. Every
  refusal on that route now answers that caller with the same localized error message the form's
  rate limit and failed save already showed, followed by a link back to the page the form was
  submitted from. Status codes and error keys are unchanged. A caller that asks for JSON — through
  `Accept: application/json` or a `Content-Type: application/json` body — still gets the JSON
  envelope, and HTMX requests still get the fragment; a caller that sends `Accept: */*` or no
  `Accept` header at all now gets `text/html` instead of JSON.

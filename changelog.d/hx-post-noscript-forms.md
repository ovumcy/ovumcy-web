### Fixed

- **Onboarding and the cycle-start control work without JavaScript and no longer put their fields
  in the address bar.** Both onboarding steps and the manual cycle-start form on the dashboard and
  the calendar day panel declared no method or action, so a browser without JavaScript submitted
  them as a GET to the current page: the CSRF token, the timezone, the last period date and the
  cycle lengths landed in the query string, and nothing was saved. Each form now posts to its own
  endpoint, and the browser is redirected with 303: to the next onboarding step, to the dashboard
  after onboarding, and back to the dashboard or the calendar day after a cycle start is marked.
  Without JavaScript, onboarding shows a plain date field in place of the date picker, and
  replacing an existing cycle start or marking one after a short gap asks for confirmation with a
  checkbox in place of the dialog. A refused submission from a browser without JavaScript shows the
  localized error with a link back to the form, under the same status code, instead of the JSON
  error body. API clients get the same responses as before.

### Fixed

- **Onboarding and the cycle-start control no longer put their fields in the address bar without
  JavaScript.** Both onboarding steps and the manual cycle-start form on the dashboard and the
  calendar day panel declared no method or action, so a browser without JavaScript submitted them
  as a GET to the current page: the CSRF token, the timezone, the last period date and the cycle
  lengths landed in the query string, and nothing was saved. Each form now posts to its own
  endpoint, and the browser is redirected with 303: to the next onboarding step, to the dashboard
  after onboarding, and back to the dashboard or the calendar day after a cycle start is marked.

### Fixed

- **A refused erasure or egress form shows a page with a way back, without JavaScript.** When the
  delete-account, clear-data, step-up, webhook or calendar-feed forms on the settings page are
  submitted without JavaScript and refused before they are handled (an expired CSRF token or a lost
  session, for instance), the browser showed the raw JSON error. It now shows the localized message
  and a link back to the settings page. API clients and htmx get the same responses as before.

### Fixed

- **A refused account or two-factor form shows a page with a way back, without JavaScript.** When the
  profile, password, recovery-code, SSO link or unlink form on the settings page, or the two-factor
  enrol or disable form, is submitted without JavaScript and refused before it is handled (an expired
  CSRF token, for instance), the browser showed the raw JSON error. It now shows the localized message
  and a link back to the page the form was on. A wrong second-factor code on the enrol form, and a
  wrong password on the disable form, read the same way. API clients and htmx get the same responses
  as before.

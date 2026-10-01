### Fixed

- **A refused day form shows a page with a way back, without JavaScript.** When the day editor, the
  dashboard day form, the usage-goal switch or the cycle settings form is submitted without
  JavaScript and refused before it is handled (an expired CSRF token, for instance), the browser
  showed the raw JSON error. It now shows the localized message and a link back to the page the form
  was on. A day entry whose values are invalid answers 422 with the same page instead of a bare
  error. API clients and htmx get the same responses as before.

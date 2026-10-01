### Fixed

- **A refused day form shows a page with a way back, without JavaScript.** When the day editor, the
  dashboard day form, the usage-goal switch or the cycle settings form is submitted without
  JavaScript and refused before it is handled (an expired CSRF token, for instance), the browser
  showed the raw JSON error. It now shows the localized message and a link back to the page the form
  was on. A day entry whose values are invalid answers 422 with the same page instead of a bare
  error. A day save or delete, or a cycle-settings save, that the server fails says so in the
  person's language instead of showing an internal message. API clients get the same responses as
  before, and htmx keeps its status and fragment.

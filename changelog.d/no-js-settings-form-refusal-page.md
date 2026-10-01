### Fixed

- **A refused settings form shows an error page with a way back, not raw JSON, in a browser
  without JavaScript.** When the reminders, interface, tracking or symptom forms were refused
  before their handler ran (an expired page token, or too many requests on a symptom form), a
  browser without JavaScript painted the JSON error body as the page. It now shows the localized
  error under the same status code, with a link back to Settings. Too many requests on the
  reminders, interface or tracking form still returns to Settings with the message, as before. API
  clients get the same responses as before.

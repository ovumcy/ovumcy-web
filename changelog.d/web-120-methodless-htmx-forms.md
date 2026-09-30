### Fixed

- **Account deletion and the day forms no longer put their fields in the address bar without
  JavaScript.** The account-deletion form, the calendar day editor's save and delete forms, and the
  dashboard's day form declared no method or action, so a browser without JavaScript submitted
  them as a GET to the current page: the deletion password and the day's entries landed in the
  query string, and nothing was saved or deleted. Each form now posts to its own endpoint with a
  hidden `_method` field and a CSRF token. The deletion password is read only from the form body.
  A browser submitting one of these forms without JavaScript is redirected with 303: to the login
  page after deletion, and to the calendar day or the dashboard after a day is saved or deleted.
  Clients that send or accept JSON, and htmx requests, get the same responses as before.

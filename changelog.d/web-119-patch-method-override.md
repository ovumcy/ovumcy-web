### Fixed

- **The settings forms and the dashboard usage-goal switch save without JavaScript.** The profile,
  interface, tracking, cycle, reminders and symptom-edit forms, and the dashboard usage-goal quick
  switch, are sent by htmx as PATCH, but a browser without JavaScript posted them to URLs that only
  accept PATCH and got 405 Method Not Allowed, so nothing was saved. Each form now names its real
  method in a hidden field, and the browser is redirected with 303 back to the settings page or
  the dashboard. API clients and htmx get the same responses as before.

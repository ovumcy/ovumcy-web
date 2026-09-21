### Security

The HTML fragments the server builds by hand for HTMX requests (error and
success status messages on the settings, day, import and two-factor forms, and
the "page not found" fragment) are now sent as `text/html; charset=utf-8`.
They used to go out under the framework's default `text/plain`, so a browser
that opened one of those responses directly showed the markup as text. All such
responses now pass through one helper that sets the type, and a test fails if a
handler sends a hand-built body any other way.

### Fixed

- **`GET /forgot-password` no longer renders the reset-request form for a signed-in owner.** It now redirects (`303`) to the same landing page `/login` uses; a guest still gets the form, and nothing about the session or the URL changes.

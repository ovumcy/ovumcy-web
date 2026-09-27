### Fixed

- **An expired or rotated CSRF token on the sign-in, registration, forgot/reset-password, or 2FA
  challenge form no longer paints the raw JSON error response into the browser.** A plain (no
  HTMX, no JavaScript) form submission refused at the CSRF layer, or by any transport-level
  rejection reached before the form's own handler runs, now answers with the same readable status
  page the language switcher already uses, with a stable key next to the localized message and a
  link back to the actual page the form was submitted from (never "/"). A caller asking for JSON,
  or submitting through HTMX, keeps its existing answer unchanged. Logout's own answer is unchanged
  and is being decided separately.

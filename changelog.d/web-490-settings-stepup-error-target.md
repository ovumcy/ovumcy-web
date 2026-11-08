### Changed

The SSO refusals of the Settings step-ups (set a local password, link an
identity, clear data, delete account) and of the identity unlink now land back
on Settings with the refusal shown there. They reused the sign-in form's `sso
temporarily unavailable` and `sso authentication failed` specs, aimed at a form
a Settings route does not have: a browser submit got the generic refusal page
instead of the Settings banner, and an HTMX submit a non-2xx fragment. JSON
callers keep the same status and key; `error_detail.target` on these routes now
reads `settings_form`.

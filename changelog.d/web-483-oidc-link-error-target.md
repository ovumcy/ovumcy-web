### Fixed

A refused OIDC identity link confirm now lands back on Settings with the
refusal shown there. The refusals the link shares with SSO sign-in (`sso
sign-in unavailable`, `sso temporarily unavailable`, `sso authentication
failed`) were aimed at the sign-in form, which a Settings route does not have,
so a plain form submit got the bare JSON error body and an HTMX submit no
status fragment. JSON callers keep the same status and key; `error_detail.target`
on this route now reads `settings_form`.

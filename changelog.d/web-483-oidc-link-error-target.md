### Changed

A refused OIDC identity link confirm now lands back on Settings with the
refusal shown there, like the link's other refusals already did. The refusals
the link shares with SSO sign-in (`sso sign-in unavailable`, `sso temporarily
unavailable`, `sso authentication failed`) were aimed at the sign-in form,
which a Settings route does not have. A browser submit got the generic refusal
page instead of the Settings banner, and an HTMX submit a non-2xx fragment.
JSON callers keep the same status and key; `error_detail.target` on this route
now reads `settings_form`.

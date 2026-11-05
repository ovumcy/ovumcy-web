none

Tests only: the login mode (local, hybrid, oidc_only) crossed with the registration mode (open,
closed) is pinned at the HTTP layer, six cells, for the register page, the register endpoint, the
sign-up link on /login and the OIDC callback of a never-seen identity under auto-provisioning. The
test app mounts no CSRF middleware and no rate limiters, so the cells say nothing about either.

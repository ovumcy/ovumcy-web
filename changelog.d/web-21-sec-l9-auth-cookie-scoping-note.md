none

Docs-only: SEC-L9 (WEB-21) design note, no code change. Adds
`docs/security/auth-cookie-scoping.md`, documenting that `ovumcy_auth` carries
no `__Host-` prefix, its `sealedCookieSpec` has no `Domain` attribute and
`Secure` tracks `COOKIE_SECURE` alone, and that fasthttp resolves a duplicate
`Cookie:` header entry first-wins — with what each gap does and does not
enable, given the cookie's payload is AEAD-sealed and version-checked.

none

Removed the public OIDC link-confirm route (`/auth/oidc/link-confirm`) and everything that
existed only for it. The route has been unreachable since issue #701 — the callback never mints
the pending-link cookie its handler reads — and its forced-reset branch minted a reset token
bound to the pre-link session version that the link's own bump would immediately retire (WEB-77).
No operator-visible behavior changes: the callback's fail-closed handoff for
`ErrOIDCLinkRequiresConfirmation` (redirect to `/login`, no cookie minted) is unchanged, only
moved out of the deleted handler and into the callback itself. Linking a new identity still has
its two live entry points: the authenticated Settings step-up and the operator CLI.

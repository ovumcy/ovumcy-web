### Added

- **Link an additional OIDC identity from Settings, or via the operator CLI.** Attaching a new
  sign-in identity to an existing account is now authorised by a live authenticated session plus a
  fresh provider re-authentication (Settings → *Connected sign-in identity*), or by the operator
  running `ovumcy link-oidc-identity <email>|--id <id> --issuer <issuer> --subject <subject>` for an
  account with no working sign-in at all.

### Security

- The public `/auth/oidc/link-confirm` page can no longer complete an identity link under any
  configuration. Linking an OIDC identity is a permanent, password-change-weight binding, and an
  unauthenticated page cannot verify a factor "now" the way a live session or an operator can.

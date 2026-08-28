### Internal

- **`docs/self-hosted.md` miscounted the OIDC cookies in its `431` arithmetic.** The section said
  "the two OIDC cookies only apply on the callback path"; the app defines four —
  `ovumcy_oidc_auth` and `ovumcy_oidc_stepup` on `/auth/oidc/callback`, `ovumcy_oidc_link_pending`
  on `/auth/oidc/link-confirm`, `ovumcy_oidc_logout_bridge` on `/auth/oidc/logout`. The exclusion
  argument the section rests on gets stronger, not weaker: each one is `Path`-scoped to the single
  endpoint that consumes it, so none reaches an ordinary page request.
- **Both operator-facing measurements now carry their date and the release they were taken on.**
  The SQLite write-concurrency table was a drill of 2026-07-28 on the then-current release image
  (v1.9.2); the cookie byte counts were measured from the running app on 2026-07-25 against the
  same release. Neither said so, so a reader could not tell which build produced them or whether
  they had gone stale — and nothing in the repository re-derives either.

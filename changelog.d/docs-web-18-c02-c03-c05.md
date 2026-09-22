### Internal

- **`docs/openapi.yaml` now states the real recovery-code and password-reset-redeem responses.**
  `POST /api/v1/users/current/recovery-code` published only "recovery code surface rendered" and told
  JSON clients to follow a redirect that never happens for them — the handler answers a JSON caller
  directly with `{ok, next_step, next_path}`, never a `303`. The spec now declares that body and the
  browser-only `303`/`HX-Redirect` separately. `POST /api/v1/password-resets/redeem` claimed its
  success redirect lands on `/login`; it lands on `/recovery-code`, where the freshly minted code
  waits — fixed, with a guard test driving the real browser-surface request. The same operation's
  `403` named only an operator-set `must_change_password` as the reason a forced-from-OIDC reset
  token survives the local-auth-disabled gate; an enrolled-but-unverifiable TOTP secret (a
  `SECRET_KEY` rotation) reaches the identical escape hatch and is now named too, with a guard test
  proving that account's recovery path completes end to end.

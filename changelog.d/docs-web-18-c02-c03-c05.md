### Fixed

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
  (local public auth switched off before redeem, against a fixture account that is genuinely
  TOTP-enabled) proving that account's recovery path completes end to end. The `403` also gained
  the reason it was missing outright — a successful password replacement whose session-issuance
  step then refuses the account's role (`web sign-in unavailable`) — since that was already
  reachable and undocumented, and now states what it leaves behind: password and recovery code
  both already replaced, the rotated code never shown on this path, and no separate sign-in to
  fall back on, because the same role check gates every web session. Two smaller trues alongside:
  `NextStepResponse`'s note no longer reads as a list of producers — `POST /api/v1/password-resets`
  answers `recovery_code` too, through its own schema and without a `next_path` — and both
  recovery-code operations now name the third success shape, the htmx one, which lands on `200`
  with `HX-Redirect` and a plain-text body instead of the declared JSON, under a guard test.

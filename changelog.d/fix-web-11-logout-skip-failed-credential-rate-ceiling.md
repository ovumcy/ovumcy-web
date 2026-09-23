### Changed

- **Breaking: the login, registration and password-reset rate limits are held to 30 requests per
  minute.** Each `RATE_LIMIT_{LOGIN,REGISTER,FORGOT_PASSWORD}_MAX` / `*_WINDOW` pair must now satisfy
  `MAX × 1 minute ≤ 30 × WINDOW`. The count ceiling of 100 alone still let 100 over a one-second
  window through: about 6000 bcrypt compares a minute from one address, where the ceiling now allows
  about half a compare a second. A pair above the rate is logged at boot, naming the allowed rate
  and an allowed pair (`100` with `200s`), and both halves fall back to their defaults (8 per 15
  minutes; 8 per hour for password reset). An operator who widened a credential budget by
  shortening its window gets the defaults from this release. Set a pair within the rate instead.
  The per-account login and recovery budgets read the same validated pairs. The one-second window
  floor is unchanged, and the e2e harness now runs at 100 per 200 s. Regressions:
  `TestCredentialRateLimitPairsHaveARateCeiling`,
  `TestCredentialRateLimitRefusalLeavesTheOtherPairsAlone`,
  `TestCredentialMaxCeilingIsReadOnlyThroughTheRateCheck`.

### Security

- **Refused logout requests no longer spend the per-IP logout budget.** The per-IP row on
  `DELETE /api/v1/sessions/current` refuses before the handler runs. Until now it counted every
  request, so anyone behind the same address (a household NAT) could spend it with sixty
  unauthenticated `DELETE`s. Every other owner's API sign-out was then refused for the rest of the
  window, with the session still alive. The row now counts only answers below 400. Successful
  sign-outs still spend it, and each account's own logout budget still bounds them per owner.
  Concurrent refused requests can still keep the row exhausted until the window ends: the limiter
  counts a request while it is in flight, and one it answers `429` itself keeps its count.
  Regression: `TestLogoutEdgeBudgetIsNotSpentByRefusedRequests`.
- **An API path spelled in another case is refused as an API request.** The router matches paths
  case-insensitively, but the session gate decided between a JSON refusal and the sign-in redirect
  on the raw path, so `DELETE /API/v1/sessions/current` without a session was answered `303` —
  below 400, so the per-IP logout row still counted it. A route behind the session gate reached
  with an uppercase letter in its `/api/` segment (`/API/…`, `/Api/…`) and no JSON `Accept` header
  is now refused with a `401`/`403` JSON answer instead of a redirect to `/login` or `/onboarding`;
  other spellings (`/api/V1/…`, a trailing slash) were already refused as JSON. One gap remains: the
  onboarding exemption still compares the raw path, so a session that has not finished onboarding
  gets `403` `onboarding_required` on `DELETE /API/v1/sessions/current` and `/API/v1/onboarding/…`,
  where the lowercase spelling succeeds. Regressions:
  `TestLogoutEdgeBudgetIsNotSpentByCaseVariantRefusals`,
  `TestAuthRequiredRefusesACaseVariantAPIPathAsAnAPIRequest`.

### Fixed

- **A path spelled in another case, or with a trailing slash, gets the same answer as the lowercase
  spelling.** The router matches paths case-insensitively and ignores a trailing slash, but several
  answers were still chosen on the raw path. `POST /LANG` or `POST /lang/` past the language-switch
  budget answered JSON where `/lang` renders a page fragment. A form error or rate-limit refusal on
  `/API/v1/sessions`, `/api/v1/sessions/`, `/AUTH/OIDC/start` or an `/API/v1/users/current…` form
  answered the JSON envelope where the lowercase spelling redirects back to the form with a flash
  message. `GET /API/…` on an unknown path rendered the HTML 404 page instead of the JSON one. A
  session that has not finished onboarding got `403` `onboarding_required` on
  `DELETE /API/v1/sessions/current` and `/api/v1/sessions/current/`, where the lowercase sign-out
  succeeds. The `scope` field of a rate-limit security event filed those spellings under `api`
  instead of `auth` or `settings`. Each of these now follows the lowercase spelling. Regressions:
  `TestEveryRawRequestPathReadIsRoutingNormalized`,
  `TestRateLimitRefusalsAnswerEveryRoutableSpellingLikeTheLowercaseOne`,
  `TestRespondAuthErrorRedirectsEveryRoutableSpellingOfAnAuthForm`,
  `TestRespondSettingsErrorRedirectsEveryRoutableSpellingOfTheSettingsForms`,
  `TestNotFoundAnswersEveryRoutableSpellingOfAnAPIPathAsJSON`,
  `TestAuthRequiredLetsEveryRoutableSpellingOfSignOutPastTheOnboardingGate`,
  `TestRateLimitScopeClassifiesEveryRoutableSpelling`.

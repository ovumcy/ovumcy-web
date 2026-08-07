### Internal

- **The 2FA login-redirect e2e test runs on WebKit again.** Its wait now binds to the click's own
  `POST /api/v1/sessions` and asserts the `303 → /auth/2fa` the server answers, instead of polling
  only for the landed URL. That race was the whole reason the test carried a permanent WebKit skip,
  so the skip is gone and every browser project covers the challenge redirect.

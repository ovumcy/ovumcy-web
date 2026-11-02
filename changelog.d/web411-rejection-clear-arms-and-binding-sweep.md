### Internal

- **A refused session's cookie retraction is tested on every refusal arm, and the account-binding
  sweep resolves the tests it relies on.** The expired-token, sealed-decode and invalid-claims
  refusals now each prove the session-only handoffs leave with the session while the reset-password
  and TOTP-pending cookies stay. The binding sweep type-checks the package with its tests and
  refuses a route whose named cross-account test is not a declared `Test` function, and the identity
  link step-up is exempt for the reason that holds: it demands the current password.

none

Tests only: the clear-data session-revocation regression now mints a second,
genuinely separate login for the same owner and requires it to be bounced to
/login after the wipe, drives the reissued cookie through /dashboard so the
inline refresh is proven to work rather than proven non-empty, and requires the
acting device's pre-wipe cookie to be refused too. It previously replayed the
acting session's own cookie and asserted only that a value came back, so it
stayed green against a handler that reissued a dead cookie. No product code.

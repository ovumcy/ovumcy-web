none

Tests only: the local-password step-up error paths now read the account after a
refusal — local auth still off, `AuthSessionVersion` untouched — instead of
trusting a 303 to `/settings` that a completed enrollment produces just as well.
No product code.

### Security

- **A burst of wrong passwords or codes can no longer outrun an account's attempt limit.** The
  per-account budgets (sign-in, recovery, the 2FA challenge, settings re-authentication and the 2FA
  enrollment code) read the count, ran the password or code check, and only then booked the
  failure. Requests that arrived while a check was running — a full bcrypt for sign-in — all read the
  same count, so a burst from several addresses could try far more guesses than the limit. The
  attempt is now reserved in the same step that checks the limit, before the check runs, and a
  correct password or code gives its slot back. Brute-force protection is tighter under load: the
  limit now bounds the guesses that run, so a real sign-in that arrives in the middle of a burst
  against the same account can be refused until the burst's attempts age out. A storage error
  during recovery-code redemption, or while the 2FA challenge reads the account, now keeps its
  attempt, as it already did at sign-in; the challenge answers such a fault as an internal error
  and leaves the pending sign-in in place. A 2FA code
  that is not six digits is refused before the attempt budget is consulted, so it neither draws an
  attempt nor, once the budget is spent, clears the pending sign-in.

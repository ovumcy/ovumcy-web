### Security

- **A burst of wrong passwords or codes can no longer outrun an account's attempt limit.** The
  per-account budgets (sign-in, recovery, the 2FA challenge, settings re-authentication and the 2FA
  enrollment code) read the count, ran the password or code check, and only then booked the
  failure. Requests that arrived while a check was running — a full bcrypt for sign-in — all read the
  same count, so a burst from several addresses could try far more guesses than the limit. The
  attempt is now reserved in the same step that checks the limit, before the check runs, and a
  correct password or code gives its slot back. Brute-force protection is tighter under load: the
  limit now bounds the guesses that run, so a real sign-in that arrives in the middle of a burst
  against the same account can be refused until the burst's attempts age out.

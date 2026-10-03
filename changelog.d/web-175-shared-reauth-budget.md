### Security

- **Turning two-factor sign-in off now shares the settings re-authentication budget.** The
  password confirmation of `DELETE /api/v1/users/current/2fa` used to draw a budget of its own
  (5 failures / 15 minutes) beside the one every other password-gated settings action draws, so a
  signed-in session could make twice as many password guesses against one account by spreading
  them over both. Both now draw one per-account budget of 5 failures / 15 minutes: wrong passwords
  on any of these endpoints refuse the others with `429`, the correct password included, until the
  window passes. The sign-in budget is separate and unchanged, and the response each
  endpoint gives is unchanged.

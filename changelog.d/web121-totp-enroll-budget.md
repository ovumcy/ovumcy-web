### Security

- **A wrong 2FA enrollment code now draws its own per-account attempt budget.** Confirming
  two-factor sign-in checked the password against the settings re-authentication budget, which
  counts only a wrong password, so the code itself was not counted. Each wrong code now counts
  against a budget of 5 per 15 minutes per account; once it is spent the confirmation answers
  `429`, even for the correct code, until the window passes, and a successful enrollment clears
  it. A missing or wrong-length code is not counted.

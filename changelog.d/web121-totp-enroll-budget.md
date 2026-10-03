### Security

- **Breaking (API shape): a wrong 2FA enrollment code now draws its own per-account attempt
  budget, and `PUT /api/v1/users/current/2fa` can answer `429`.** A client that confirms
  enrollment has to handle that status and wait out the window. Confirming
  two-factor sign-in checked the password against the settings re-authentication budget, which
  counts only a wrong password, so the code itself was not counted. Each wrong code now counts
  against a budget of 5 per 15 minutes per account; once it is spent the confirmation answers
  `429`, even for the correct code, until the window passes, and a successful enrollment clears
  it. A missing or wrong-length code is not counted.

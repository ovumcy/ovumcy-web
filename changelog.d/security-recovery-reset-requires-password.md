### Security

- **Password recovery now requires your password as well as your recovery code.** A recovery code
  on its own used to be enough to reset the password on `/forgot-password`, receive a signed-in
  session, and from there switch two-factor authentication off — so a single captured secret was a
  full account takeover, on every release that has ever shipped TOTP. The recovery code is now what
  it was always meant to be: a stand-in for the **second** factor, never for the password. The
  recovery form asks for both, and a submission with a wrong or missing password is refused exactly
  like a wrong recovery code — same answer, same rate-limit cost, no account existence revealed.
- **The cost, stated plainly:** if you have forgotten your password and hold only a recovery code,
  you can no longer recover the account yourself. Your fallback is the operator CLI,
  `ovumcy reset-password <email>`, which needs shell or container access to the instance. The path
  this route was written for still works unchanged: an owner whose TOTP stopped working after a
  `SECRET_KEY` rotation knows their password and only lost the second factor, so password +
  recovery code gets them back in and lets them re-enrol TOTP.

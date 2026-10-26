### Fixed

- The "lost your authenticator" link on the two-factor challenge now leads to the recovery flow
  that works — email, then the recovery code beside the current password, which signs in without
  the authenticator and rotates both — instead of the one-time reveal page, which sent the visitor
  back to sign-in with no message. That flow is retitled "Sign in with your recovery code" on the
  page, in the sign-in link and in the tab title, since it never recovered a forgotten password: the
  current password is one of its operands.
- Auth page errors are announced: the error notice on the sign-in, registration, two-factor,
  recovery and reset pages is a `role="alert"` region, and the fields the error is about carry
  `aria-invalid` and are described by it. An enumeration-safe refusal marks every operand it
  compared, never one of them.
- The recovery-code reveal after registration guards against leaving the page before the code is
  acknowledged: a reload or navigation away asks for confirmation first, and the copy says where a
  lost code is regenerated (Settings → Account, behind the password).

### Fixed

- An owner who has not finished onboarding can sign out: `POST /logout` is no longer redirected
  back to `/onboarding` with the session alive, and the onboarding page carries its own sign-out
  control. The sign-out confirmation now says that it also ends the account's sessions on every
  other device, which it always did.

### Fixed

- **Switching the language from the public switcher while signed in now sticks.** The switcher on
  the login and onboarding pages wrote only the browser's language cookie, so for a signed-in owner
  the choice was silently reverted the next time a session was issued — the cookie is re-written
  from the account at every sign-in. A switch made with a session present is now saved on the
  account as well, through the same save `Settings → Interface` uses, including its refusal of a
  language this build does not ship. A visitor with no session is unaffected: the switch stays a
  cookie, nothing is stored, and the answer is the same either way.

### Security

- **Signing out now also clears the remembered interface language.** The `ovumcy_lang` cookie
  outlived a sign-out by up to a year, so on a shared or borrowed browser the login page still
  disclosed that Ovumcy is used on this device, and in which language, before anyone signed in.
  Ending a session on purpose — sign-out, or deleting the account — now retracts it along with the
  session cookies. A session the server merely refuses (an expired or invalid cookie) deliberately
  leaves it alone, so a visitor whose session lapses mid-use keeps reading the login page in their
  own language.

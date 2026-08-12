### Changed

- **The interface language is now part of the account, not just the browser.**
  A language chosen in `Settings` is stored on the account as well as in the
  `ovumcy_lang` cookie, and every sign-in — password, second factor, SSO,
  post-registration pickup — re-issues the cookie from it. Signing in from a new
  browser, another device, or after clearing cookies now shows the language the
  owner picked instead of falling back to the browser's `Accept-Language` or the
  operator's `DEFAULT_LANGUAGE`. An account that never chose a language keeps
  the previous behaviour exactly, and the stored language survives
  `Settings → Clear data`, which erases health records rather than the language
  the interface is read in.

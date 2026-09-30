### Security

- **A correct current password no longer clears the re-authentication attempt count when the
  password change it confirmed is then refused.** Changing the password in Settings draws on the
  same attempt budget as the other password-confirmed Settings actions, and a correct current
  password cleared it as soon as it was checked, before the new password was saved. The count is
  now cleared only once the new password has been saved, so a change refused on the way to
  storage leaves the earlier failed attempts counted. Responses are unchanged.

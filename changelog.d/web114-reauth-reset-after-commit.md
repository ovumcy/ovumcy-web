### Security

- **A correct Settings password no longer clears the re-authentication attempt count when the
  change it confirmed is then refused.** Clearing data, deleting the account, regenerating the
  recovery code, turning on two-factor sign-in and unlinking an OIDC identity each ask for the
  current password against one shared attempt budget. A correct password cleared the wrong
  guesses counted before it as soon as it was checked, even when the change itself then failed
  (a sign-out everywhere landing mid-request, a storage error, a recovery code that could not be
  delivered). The count is now cleared only once the change has been saved, so a refused change
  leaves the earlier failed attempts counted. Responses are unchanged. The clear-data password
  pre-check clears the count once the password is confirmed, and the start of linking an OIDC
  identity once it has redirected to the provider; neither saves anything itself.

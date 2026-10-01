### Security

- **A correct 2FA code no longer clears the attempt count when the sign-in it earned fails.** The
  2FA challenge forgave the client's earlier wrong codes as soon as the code checked out, before
  the session was issued and before an SSO sign-in's provider-logout state was moved onto it. When
  either step failed, the sign-in answered with an error and the count was already gone. The count
  is now cleared only once the session has been issued; a sign-in that fails after the code check
  keeps the wrong codes counted before it.

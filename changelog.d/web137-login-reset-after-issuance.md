### Security

- **A correct password no longer clears the attempt count when the sign-in it earned fails.**
  Password sign-in forgave the client's earlier wrong passwords as soon as the password checked
  out, before the session, 2FA or forced password-reset step was issued. When that step failed,
  the sign-in answered with an error and the count was already gone. The count is now cleared only
  once the sign-in has moved on to its next step; a sign-in that fails after the password check
  keeps the wrong passwords counted before it.

### Fixed

SSO sign-in works with identity providers that send the ID token's
`email_verified` as the string `"true"` or `"false"` rather than a JSON
boolean, as AWS Cognito does; such a sign-in used to fail outright. Only those
two exact strings are accepted. Any other value still fails the sign-in, and
so does `null`, which used to be read as unverified; an absent claim still
means the email is unverified.

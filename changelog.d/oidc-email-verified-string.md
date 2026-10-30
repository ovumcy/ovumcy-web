### Fixed

SSO sign-in works with identity providers that send the ID token's
`email_verified` as the string `"true"` or `"false"` rather than a JSON
boolean, as AWS Cognito does; such a sign-in used to fail outright. Only those
two exact strings are accepted. An absent or `null` claim still means the email
is unverified, and any other value still fails the sign-in.

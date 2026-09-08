### Security

- **The SSO client refuses an issuer or endpoint URL that names no host.** `https://:8443` parses
  as a valid absolute URL with an empty host, which the network stack resolves to the machine
  itself — so a misconfigured issuer, or a discovery document built around one, would have posted
  the client secret and authorization code to whatever listens locally on that port. Configuration
  validation and every origin pin now require a named host. A discovery document whose logout or
  key-set metadata does not decode is also refused outright, instead of being trusted for whatever
  fields happened to decode before the error.

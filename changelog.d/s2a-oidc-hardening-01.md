### Security

- **The SSO client refuses an issuer or endpoint URL that names no host.** `https://:8443` parses
  as a valid absolute URL with an empty host, which the network stack resolves to the machine
  itself — so a misconfigured issuer, or a discovery document built around one, would have posted
  the client secret and authorization code to whatever listens locally on that port. Configuration
  validation and every origin pin now require a named host. The logout-endpoint sanitizer also runs
  on every discovery document now, including one whose metadata only partly decodes; before, a
  decode error skipped it.

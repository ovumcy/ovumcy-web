### Security

- **The SSO client refuses an issuer or endpoint URL that does not name a remote host.** `https://:8443`,
  `https://0.0.0.0:8443` and `https://[::]:8443` all parse as valid absolute URLs that the network
  stack resolves to the machine itself — so a misconfigured issuer, or a discovery document built
  around one, would have posted the client secret and authorization code to whatever listens locally
  on that port. Configuration validation, every origin pin and the stored provider-logout state now
  require a named host; a self-hosted issuer on `127.0.0.1` stays supported.
- **A discovery document that only partly decodes no longer keeps an unpinned logout endpoint.**
  JSON decoding keeps a field it has already read when a later occurrence of the same key fails, so
  a document naming `end_session_endpoint` twice — once as an off-origin URL, once as a number —
  used to leave that URL in place while the error skipped the sanitizer. The sanitizer now runs on
  whatever decoded, and a logout endpoint it rejects degrades to local sign-out as an absent one does.

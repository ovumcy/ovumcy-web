### Security

- **The SSO client refuses an issuer or endpoint URL that does not name a remote host.** `https://:8443`,
  `https://0.0.0.0:8443` and `https://[::]:8443` all parse as valid absolute URLs that the network
  stack resolves to the machine itself — so a misconfigured issuer, or a discovery document built
  around one, would have posted the client secret and authorization code to whatever listens locally
  on that port. Configuration validation, every origin pin, the discovery `authorization_endpoint`
  the browser is sent to, and the stored provider-logout state now require a named host — the whole
  `0.0.0.0/8` block, a zone identifier and the IPv4-mapped spelling included, as is any numeric host
  Go does not parse as a canonical address (`0`, `0.1`, `00.0.0.0`, `0x0`, `192.168.001.010`), which
  a platform resolver may still read as one. A self-hosted issuer on `127.0.0.1` or a LAN address
  written in plain dotted-quad form stays supported.
- **Upgrade note:** an instance whose `OIDC_ISSUER_URL`, `OIDC_REDIRECT_URL` or
  `OIDC_POST_LOGOUT_REDIRECT_URL` names such a host — `https://0.0.0.0:8443` included — now fails
  configuration validation at startup (`… must name a remote host`) instead of starting. Replace
  the host with the real hostname or address the provider and browsers reach.
- **A provider discovery document without an `authorization_endpoint` now leaves SSO unavailable.**
  An empty authorize URL used to compose a relative redirect that sent the browser back into Ovumcy
  carrying the sign-in `state` and `nonce` instead of failing the sign-in.
- **A discovery document that only partly decodes no longer keeps an unpinned logout endpoint.**
  JSON decoding keeps a field it has already read when a later occurrence of the same key fails, so
  a document naming `end_session_endpoint` twice — once as an off-origin URL, once as a number —
  used to leave that URL in place while the error skipped the sanitizer. The sanitizer now runs on
  whatever decoded, and a logout endpoint it rejects degrades to local sign-out as an absent one does.

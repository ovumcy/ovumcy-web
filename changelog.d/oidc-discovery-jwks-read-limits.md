### Security

- **Every response Ovumcy reads from the OIDC provider is now size-bounded.** The discovery document
  and the signing-key set (JWKS) were read whole, with no size limit, so a compromised or
  misbehaving identity provider — or anything answering on its address — could make the server
  buffer an arbitrarily large response in memory. The token response was capped at 1 MiB, but by
  cutting it short silently, and a form-encoded token response cut short still reads as a complete
  one. Every OIDC request — discovery, JWKS and code exchange — now shares one limit of 512 KiB per
  response body and 64 KiB of response headers. A response over either limit fails the sign-in with
  an error instead of being truncated or buffered, so it behaves like an unreachable provider: the
  SSO attempt returns to `/login` with the generic error. Real discovery documents and key sets are
  a few KiB to a few tens of KiB, so no working provider is affected.

### Security

- **A request too large to read no longer receives a cookie.** When a request's headers overflow the
  server's read buffer, Ovumcy answers `431` without having read the request at all, yet that answer
  still carried a fresh anti-forgery cookie, minted for a request that never existed. The `431`
  answer now sets nothing.

### Changed

- **Fiber 3.6.1.** The web framework moves from 3.5.0 to 3.6.1 (fasthttp 1.76.0). This release
  normalizes a request path before routing it — `/x/../days` now reaches `/days` — and the
  onboarding gate already decides on the path the router dispatches on, so an account still
  onboarding stays held at onboarding whichever spelling it sends.

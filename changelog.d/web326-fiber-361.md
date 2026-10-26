### Security

- **A request the server cannot read no longer receives a cookie.** When a request's headers
  overflow the server's read buffer (`431`), are malformed (`400`) or time out (`408`), Ovumcy
  answers without having read the request at all, yet that answer still carried a fresh
  anti-forgery cookie, minted for a request that never existed — and it replaced the browser's own,
  so a form already open in another tab went stale. Those answers now set nothing.

### Changed

- **Fiber 3.6.1.** The web framework moves from 3.5.0 to 3.6.1 (fasthttp 1.76.0). This release
  normalizes a request path before routing it — `/x/../days` now reaches `/days` — and the
  onboarding gate already decides on the path the router dispatches on, so an account still
  onboarding stays held at onboarding whichever spelling it sends.

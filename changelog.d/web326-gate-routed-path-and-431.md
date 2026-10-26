### Security

- **The onboarding gate decides on the path the router dispatches on.** For an account that has not
  finished onboarding, the gate exempted the onboarding page and API by the prefix of the raw request
  path, so `/api/v1/onboarding/../days/<date>` read as an onboarding request. That was safe only
  while the router took the `..` segment literally and matched no route; a router release that
  removes dot segments before matching dispatches it to the days route. The gate now reads the
  routed path, and refuses any request whose path carries a `.` or `..` segment, an empty segment,
  or was rewritten by the router on the way — such a path is never produced by the app itself.
- **The request read buffer is pinned at 4 KB.** A request head (start line plus every header,
  cookies included) larger than that keeps answering `431` with the `request_headers_too_large`
  envelope, instead of depending on the framework default, which a newer Fiber release raises.

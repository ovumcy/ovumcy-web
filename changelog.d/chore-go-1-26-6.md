### Security

- **Go toolchain bumped to 1.26.6.** Clears six standard-library advisories that govulncheck
  reports as reachable from this code: quadratic complexity in `net/url` `resolvePath`
  (GO-2026-6218), JavaScript regexp context tracking in `html/template` (GO-2026-6091), unbounded
  post-handshake messages in `crypto/tls` (GO-2026-6090), missing recursion-depth guards in
  `encoding/xml` (GO-2026-6088) and `encoding/asn1` (GO-2026-5972), and ASCII-only Punycode labels
  in `golang.org/x/net/idna` (GO-2026-5026). Four of the six are reached only through outbound
  paths an operator configures — webhook delivery and the OIDC CA file. The runtime image's
  builder stage moves to `golang:1.26.6-alpine3.24` (the `alpine3.22` variant of this patch
  release was not published, as with the previous bump); the final runtime stage is unchanged.

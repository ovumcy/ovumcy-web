### Security

- **Go toolchain bumped to 1.27.2 and `golang.org/x/net` to v0.60.0.** The module's `go` line
  (`go.mod`) and the runtime image's builder stage (`golang:1.27.2-alpine3.24` in the
  `Dockerfile`) both move, which clears the advisories govulncheck and Trivy reported as reachable
  on 1.27.1: HTTP/2 connection handling in `net/http/internal/http2` and `golang.org/x/net`
  (GO-2026-6617 / CVE-2026-78669), a crafted-request denial of service in `net/http`
  (CVE-2026-78667) and a `crypto/tls` denial of service (CVE-2026-97031). The server's listener
  and the outbound OIDC transport both reach the HTTP/2 path. The final runtime stage is unchanged.

### Internal

- **`source-map-js` lifted to 1.2.2 in the lockfile**, closing CVE-2026-93749 (HIGH) and turning
  the required `trivy-fs` check green again. The package is a transitive dev-only dependency of
  the CSS build and of `jsdom`, not part of any shipped artifact.

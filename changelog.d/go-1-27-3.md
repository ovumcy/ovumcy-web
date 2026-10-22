### Security

- **Go toolchain bumped to 1.27.3.** The module's `go` line (`go.mod`) and the runtime image's
  builder stage (`golang:1.27.3-alpine3.24` in the `Dockerfile`) both move, which clears
  CVE-2026-111943 (HIGH) in `crypto/x509`: certificate chain verification could accept an
  untrusted intermediate, opening the outbound TLS connections — the OIDC provider's discovery,
  token and JWKS endpoints — to a man-in-the-middle. The final runtime stage is unchanged.

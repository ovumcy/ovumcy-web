package security

import (
	"fmt"
	"io"
	"net/http"
)

// Every response the OIDC HTTP client reads is bounded, because the reads
// themselves happen inside the libraries: go-oidc reads the discovery document
// (NewProvider) and the JWKS (RemoteKeySet.updateKeys) with an unbounded
// io.ReadAll, and oauth2 reads the token response through a 1 MiB LimitReader
// that truncates silently — a form-encoded token response cut at 1 MiB still
// parses. So the bound lives in the client every one of those fetches shares.
//
// A real discovery document is a few KiB and a JWKS with several RSA keys and
// x5c chains tens of KiB, so 512 KiB leaves an order of magnitude of headroom
// while staying below oauth2's own 1 MiB cut, which this cap must reach first
// for the token response to fail instead of truncating.
const oidcResponseBodyLimit int64 = 512 << 10

// Real IdP response headers are well under 8 KiB; 64 KiB tolerates a large
// cookie set while replacing the transport's 10 MiB default.
const oidcResponseHeaderLimit int64 = 64 << 10

var errOIDCResponseTooLarge = fmt.Errorf("oidc http response body exceeds %d bytes", oidcResponseBodyLimit)

// oidcBoundedBodyTransport caps every response body the OIDC client returns.
type oidcBoundedBodyTransport struct {
	base  http.RoundTripper
	limit int64
}

func (transport *oidcBoundedBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := transport.base.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	resp.Body = &oidcBoundedBody{body: resp.Body, remaining: transport.limit}
	return resp, nil
}

// oidcBoundedBody yields at most `remaining` bytes and then fails: it reads one
// byte past the cap, and that byte turns into errOIDCResponseTooLarge rather
// than into an EOF, so an oversized body is an error to its reader and never a
// truncated document that parses. Memory stays bounded by the caller's buffer.
type oidcBoundedBody struct {
	body      io.ReadCloser
	remaining int64
	exceeded  bool
}

func (body *oidcBoundedBody) Read(p []byte) (int, error) {
	if body.exceeded {
		return 0, errOIDCResponseTooLarge
	}
	if int64(len(p)) > body.remaining+1 {
		p = p[:body.remaining+1]
	}
	n, err := body.body.Read(p)
	if int64(n) > body.remaining {
		body.exceeded = true
		n = int(body.remaining)
		body.remaining = 0
		return n, errOIDCResponseTooLarge
	}
	body.remaining -= int64(n)
	return n, err
}

func (body *oidcBoundedBody) Close() error {
	return body.body.Close()
}

package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Webhook outbound delivery (issue #124, slice 3). This file owns the SINGLE
// egress point for owner-configured webhook reminders. It is security-critical:
// the URL it POSTs to is fully owner-controlled (self-hosted ntfy/Gotify/Apprise
// commonly live on the LAN), so the request envelope is hardened rather than the
// destination blocked.
//
// Envelope hardening (all mandatory, none configurable away):
//
//   - Hard total timeout via context.WithTimeout plus a matching dialer timeout
//     and client Timeout, so a slow or hung endpoint cannot stall the notify
//     pass.
//   - DisableKeepAlives: each delivery is a one-shot connection; we never pool to
//     an owner-controlled host.
//   - ZERO redirects: CheckRedirect always returns an error, so a 3xx response
//     cannot steer the request (or the JSON body) to a second, unvalidated origin
//     after the scheme/host check passed (SSRF-via-redirect).
//   - Response body capped by io.LimitReader: we only need the status code, so we
//     read at most a few KB and discard the rest — a hostile endpoint cannot make
//     us buffer an unbounded body.
//   - Scheme allowlist http/https ONLY, re-checked here even though slice-1
//     save-time validation already enforced it (defence in depth: the decrypt
//     path or a direct DB edit could in principle yield another scheme).
//
// No-secret-in-transport/logs invariant: the webhook URL may embed an ntfy bearer
// token in its userinfo or query, so this file logs the HOSTNAME ONLY (never the
// full URL, path, query, or userinfo) and never logs the request/response body.

const (
	// webhookDeliveryTimeout is the hard total budget for one delivery: DNS +
	// connect + TLS + request + reading the (capped) response. It bounds how long
	// a single unresponsive owner endpoint can hold up the notify pass.
	webhookDeliveryTimeout = 10 * time.Second
	// webhookResponseReadLimit caps how many bytes of the response body we read.
	// We only need the status code; anything beyond this is drained-and-ignored so
	// a hostile endpoint cannot make us buffer an unbounded body.
	webhookResponseReadLimit = 8 * 1024
	// webhookUserAgent identifies our POSTs without revealing anything sensitive.
	webhookUserAgent = "ovumcy-webhook/1"
)

// ErrWebhookDeliveryURLScheme is returned when a decrypted URL does not use
// http/https at delivery time. It never embeds the URL so the value cannot leak
// into a log or error chain.
var ErrWebhookDeliveryURLScheme = errors.New("webhook delivery url scheme not allowed")

// errWebhookRedirect is returned by the delivery client's CheckRedirect to
// refuse ALL redirects. It is intentionally generic and carries no location.
var errWebhookRedirect = errors.New("webhook delivery refuses redirects")

// errWebhookPrivateAddress is returned by the guarded dialer when the
// WEBHOOK_BLOCK_PRIVATE_ADDRESSES gate is on and a target resolves to (or is) a
// private/loopback/link-local/unspecified address. It surfaces from
// http.Client.Do wrapped in *url.Error; Deliver classifies it via errors.Is so
// the log reason stays stable. It carries no host or IP so it cannot leak one.
var errWebhookPrivateAddress = errors.New("webhook delivery refuses private address")

// ipResolver is the hostname-resolution seam. net.DefaultResolver satisfies it
// in production; tests inject a fake so no case depends on live DNS.
type ipResolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// WebhookPayload is the transport-free notification body. It carries only what a
// reminder needs — a title, a message, and the MANDATORY medical-safety
// disclaimer — and never any secret (no webhook URL, token, SECRET_KEY, or
// recovery code) and no health specifics beyond the reminder type, estimated
// date, and lead days already summarized into Title/Message.
type WebhookPayload struct {
	// Title is a short reminder headline (e.g. "Period reminder").
	Title string `json:"title"`
	// Message is the human-readable reminder line (type + estimated date + lead).
	Message string `json:"message"`
	// Disclaimer is the medical-safety qualifier, MANDATORY in every payload. It
	// is the owner-localized "estimates, not medical advice or a method of
	// contraception" string (i18n key dashboard.prediction_disclaimer).
	Disclaimer string `json:"disclaimer"`
	// Type is the machine-readable reminder kind (DueReminderType*), so a webhook
	// consumer can route on it without parsing Message.
	Type string `json:"type"`
	// EventDate is the estimated event's calendar day in RFC3339-less YYYY-MM-DD
	// form (owner-local). Minimized health specific: the date only, no cycle math.
	EventDate string `json:"event_date"`
	// LeadDays echoes the lead window that surfaced this reminder.
	LeadDays int `json:"lead_days"`
}

// WebhookDeliverer is the narrow delivery seam the notify service depends on. It
// is an interface so tests can substitute a capturing/failing stub and the
// notify service never reaches for a real socket.
type WebhookDeliverer interface {
	// Deliver POSTs payload as JSON to decryptedURL and reports success. Success
	// is a 2xx response; every other outcome (non-2xx, timeout, refused redirect,
	// bad scheme, transport error) is a failure. It must never log the URL beyond
	// its hostname, and never the body.
	Deliver(ctx context.Context, decryptedURL string, payload WebhookPayload) error
}

// blockPrivateAddresses, when true, denies delivery to private/loopback/
// link-local targets — both IP literals (fast pre-check in Deliver) and
// hostnames (resolve-and-check in the guarded dialer, which validates every
// resolved record and pins the dialed IP to close DNS-rebinding). It is OFF BY
// DEFAULT because self-hosted ntfy/Gotify legitimately live on the LAN; the gate
// exists so an operator can opt in via WEBHOOK_BLOCK_PRIVATE_ADDRESSES.
type webhookDeliveryClient struct {
	httpClient            *http.Client
	blockPrivateAddresses bool
}

// NewWebhookDeliverer builds the hardened outbound client. blockPrivateAddresses
// wires the off-by-default private-address gate (see WEBHOOK_BLOCK_PRIVATE_ADDRESSES);
// callers pass false unless the operator opted in.
func NewWebhookDeliverer(blockPrivateAddresses bool) WebhookDeliverer {
	return newWebhookDelivererWithResolver(blockPrivateAddresses, net.DefaultResolver)
}

// newWebhookDelivererWithResolver is the seam-injecting constructor: it lets a
// test supply a fake ipResolver so the resolve-and-check dialer can be exercised
// without live DNS. Production goes through NewWebhookDeliverer with
// net.DefaultResolver.
func newWebhookDelivererWithResolver(blockPrivateAddresses bool, resolver ipResolver) *webhookDeliveryClient {
	return &webhookDeliveryClient{
		httpClient:            newWebhookHTTPClient(blockPrivateAddresses, resolver),
		blockPrivateAddresses: blockPrivateAddresses,
	}
}

// newWebhookHTTPClient constructs the hardened *http.Client used for every
// delivery: bounded timeouts, no keep-alives, and zero redirects. Only when
// blockPrivateAddresses is on is the resolve-and-check DialContext installed; the
// default (gate-off) path keeps the plain stock dialer, byte-for-byte unchanged.
func newWebhookHTTPClient(blockPrivateAddresses bool, resolver ipResolver) *http.Client {
	baseDialer := &net.Dialer{Timeout: webhookDeliveryTimeout}
	dialContext := baseDialer.DialContext
	if blockPrivateAddresses {
		dialContext = guardedDialContext(baseDialer, resolver)
	}
	return &http.Client{
		Timeout: webhookDeliveryTimeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext:       dialContext,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errWebhookRedirect
		},
	}
}

// guardedDialContext is the resolve-and-check DialContext used ONLY when the
// private-address gate is on. It resolves the target host once (or uses the IP
// literal directly) and refuses the whole target with errWebhookPrivateAddress if
// ANY resolved A/AAAA record is private/loopback/link-local/unspecified. It then
// dials one of the validated IPs directly (never a second, independent
// resolution), so a rebinding resolver cannot return a public answer to the check
// and a private one to the dial. The request keeps its original Host header and
// TLS SNI because DialContext only fixes the TCP endpoint, not the request URL.
func guardedDialContext(baseDialer *net.Dialer, resolver ipResolver) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		var ips []net.IP
		if literal := net.ParseIP(host); literal != nil {
			ips = []net.IP{literal}
		} else {
			resolved, err := resolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, addr := range resolved {
				ips = append(ips, addr.IP)
			}
		}
		if len(ips) == 0 {
			return nil, &net.DNSError{Err: "no addresses", Name: host, IsNotFound: true}
		}

		// Reject the whole target if any record is private: a mixed public/private
		// answer is exactly the rebinding shape we must refuse.
		for _, ip := range ips {
			if isPrivateIP(ip) {
				return nil, errWebhookPrivateAddress
			}
		}

		// Dial a validated IP directly, trying each until one connects.
		var lastErr error
		for _, ip := range ips {
			conn, err := baseDialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

// Deliver POSTs the payload as JSON to decryptedURL under the hardened envelope.
// It returns nil only on a 2xx response. On any failure it logs a stable reason
// key plus the destination HOST and (when available) the status code — never the
// URL, query, userinfo, or response body — and returns an error that likewise
// carries no secret.
func (client *webhookDeliveryClient) Deliver(ctx context.Context, decryptedURL string, payload WebhookPayload) error {
	parsed, err := url.Parse(strings.TrimSpace(decryptedURL))
	if err != nil {
		// Do not wrap: url.Parse's error embeds the raw URL. Log host-less.
		log.Printf("webhook delivery skipped: reason=url_parse_failed")
		return ErrWebhookDeliveryURLScheme
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		log.Printf("webhook delivery skipped: reason=scheme_not_allowed host=%s", parsed.Hostname())
		return ErrWebhookDeliveryURLScheme
	}

	if client.blockPrivateAddresses && isPrivateHost(parsed.Hostname()) {
		log.Printf("webhook delivery skipped: reason=private_address_blocked host=%s", parsed.Hostname())
		return fmt.Errorf("webhook delivery to private address refused")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		// codecov:ignore -- unreachable: WebhookPayload is all JSON-safe scalar
		// fields, so json.Marshal cannot fail here. Kept as a fail-safe so a future
		// unmarshalable field never delivers a malformed body.
		log.Printf("webhook delivery skipped: reason=payload_marshal_failed host=%s", parsed.Hostname())
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	requestCtx, cancel := context.WithTimeout(ctx, webhookDeliveryTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, parsed.String(), bytes.NewReader(body))
	if err != nil {
		// codecov:ignore -- unreachable in practice: method is constant and the URL
		// already parsed above, so NewRequestWithContext does not fail here.
		log.Printf("webhook delivery skipped: reason=build_request_failed host=%s", parsed.Hostname())
		return fmt.Errorf("build webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", webhookUserAgent)

	response, err := client.httpClient.Do(request)
	if err != nil {
		// A refused redirect or a private-address block surfaces here wrapped in
		// *url.Error; classify it so the reason key is stable without logging the
		// (secret-bearing) location or the resolved IP.
		reason := "transport_error"
		switch {
		case isRedirectRefusal(err):
			reason = "redirect_refused"
		case errors.Is(err, errWebhookPrivateAddress):
			reason = "private_address_blocked"
		}
		log.Printf("webhook delivery failed: reason=%s host=%s", reason, parsed.Hostname())
		// Return a HOST-ONLY error: the transport error (*url.Error) embeds the full
		// request URL — which may carry an ntfy token in its userinfo/query — in its
		// message, so it must never be wrapped (%w) into a returned error a future
		// caller might log. Reason + host is all a caller needs.
		return fmt.Errorf("webhook delivery to host %q failed: %s", parsed.Hostname(), reason)
	}
	defer func() {
		// Drain (bounded) and close so the connection can be reused/released; the
		// LimitReader guarantees we never read an unbounded hostile body.
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, webhookResponseReadLimit))
		_ = response.Body.Close()
	}()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		log.Printf("webhook delivery failed: reason=non_2xx host=%s status=%d", parsed.Hostname(), response.StatusCode)
		return fmt.Errorf("webhook delivery to host %q returned status %d", parsed.Hostname(), response.StatusCode)
	}
	return nil
}

// isRedirectRefusal reports whether err is our zero-redirect refusal, which the
// stdlib returns wrapped in *url.Error from client.Do.
func isRedirectRefusal(err error) bool {
	return errors.Is(err, errWebhookRedirect)
}

// isPrivateHost reports whether host is a loopback / private / link-local
// address LITERAL. It is the fast pre-check at Deliver time: an IP-literal
// target is rejected before any request. A hostname (non-literal) returns false
// here and is instead resolved-and-checked inside the guarded dialer
// (guardedDialContext), which validates every resolved record and pins the
// dialed IP — so the private-address gate is enforced for hostnames too, without
// a second independent resolution.
func isPrivateHost(host string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	return isPrivateIP(ip)
}

// isPrivateIP is the core address classifier shared by the literal pre-check and
// the guarded dialer: loopback, RFC1918/ULA private, link-local (unicast and
// multicast), and the unspecified address all count as private.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

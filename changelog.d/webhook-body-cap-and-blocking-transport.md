none

Test-only hardening: the webhook response-body cap was only proven by a
"large body still succeeds" test, which cannot tell a capped read from an
uncapped one. `TestWebhookDeliveryResponseBodyReadIsCappedAtTheConstant` now
wraps the response body in a counting reader and asserts the deliverer reads
EXACTLY `webhookResponseReadLimit` bytes from a server that serves far more,
regardless of what the network or the OS buffered underneath. Separately, the
one blocking-transport regression tied its proof to a short client-side
`http.Client.Timeout`, which cannot distinguish "the caller's context was
honoured" from "the client just gave up on its own clock".
`TestWebhookDeliveryReleasedByCallerContextCancellationNotClientTimeout` adds
a transport double that blocks until ITS OWN request context is done, with
the client timeout set to an hour (far past the test's few-second bound), and
proves via a channel handshake — no sleeps — that cancelling only the
caller's context releases the call promptly and that the transport itself
observed `context.Canceled`. Both invariants are identical for the default
JSON envelope and the `?format=ntfy` path, so one test each covers both.

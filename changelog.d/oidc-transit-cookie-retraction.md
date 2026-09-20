### Security

- **A sealed cookie the server refuses is retracted in the same response, by the reader that
  refused it.** The OIDC sign-in state, step-up, step-up continuation and pending-link cookies used
  to stay in the browser after the server had refused to read them — an unopenable envelope, a
  plaintext that is not the payload, a payload past its own expiry — and kept being sent until
  they expired on their own; the pending-link cookie, which carries the target account, the issuer,
  the subject and the asserted email, was cleared by each of its callers instead, so the next
  caller added would have carried the leak back. Each reader now clears the value in the response
  that refused it, and the callers no longer repeat it. A value the server still honours is left in
  place, so a stray or cross-site request to the callback path cannot cancel a sign-in, a step-up
  or a link confirmation in progress. The invariant is now pinned for every sealed cookie the
  transport layer declares, from a set derived from the source rather than a list.

### Security

- **The rate limiters no longer let a client choose its own bucket behind a proxy that appends a
  port.** With `PROXY_HEADER=X-Forwarded-For`, a trusted proxy that writes the client as
  `address:port` had that entry dropped before the limiters read the chain, so they keyed on the
  client-written entry to its left and a new value per request bypassed the per-address limits and
  the per-client sign-in attempt budget. The limiters now read every `X-Forwarded-For` line
  themselves, accept `address:port` and `[address]:port`, and fall back to the direct peer when an
  entry does not parse instead of stepping past it.

- **Cloud metadata stays unreachable through a local-use NAT64 prefix.** With
  `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` off, a webhook target in `64:ff9b:1::/48` was delivered to,
  so a network with a translator on that prefix could route a reminder to `169.254.169.254`. The
  whole prefix is now refused in that mode, as it already was with the setting on.

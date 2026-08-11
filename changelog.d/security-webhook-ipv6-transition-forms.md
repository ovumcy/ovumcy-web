### Security

- **The webhook private-address block now recognises every IPv6 form that hides an IPv4
  destination inside it** (GHSA-hg2x-v5cc-m384). With `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=true`
  the guard decoded only RFC 6052 NAT64 (`64:ff9b::/96`), so the same internal target written
  as 6to4 (`2002:7f00:1::` is `127.0.0.1`), IPv4-compatible (`::10.0.0.1`), Teredo or SIIT
  IPv4-translated passed the check. All five forms are now decoded and classified by the IPv4
  they wrap — a wrapped **public** address still routes to the public internet and stays
  allowed, unchanged. Four ranges the guard never covered are refused too: RFC 1122
  `0.0.0.0/8` beyond the unspecified address alone, deprecated IPv6 site-local `fec0::/10`
  (outside Go's `fc00::/7`), and the RFC 8215 local-use NAT64 block `64:ff9b:1::/48`.
  Terminal classification runs before decoding, so `::1` and `::` — which sit inside the
  IPv4-compatible prefix — are never re-read as public addresses.

  The gate remains **off by default**: a self-hosted instance delivering to ntfy or Gotify on
  the same LAN is unaffected, and no configuration changes. Reported by tonghuaroot.

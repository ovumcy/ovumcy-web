### Security

- **The webhook private-address block now refuses every address the IANA special-purpose
  registries record as not globally reachable.** With `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=true` the
  gate knew only the ranges that had been reported to it, so a reminder aimed at RFC 2544
  benchmarking space (`198.18.0.0/15`), IETF protocol assignments (`192.0.0.0/24`), the reserved
  `240.0.0.0/4` including the broadcast address, multicast, or documentation space (`192.0.2.0/24`,
  `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`, `3fff::/20`) was delivered as if it were the
  public internet — the first three of those route on real networks. All of them are refused now,
  along with the IPv6 discard-only, benchmarking, ORCHIDv2, drone-remote-ID and SRv6-SID blocks. The
  special-purpose prefixes the registries do record as globally reachable — AS112, AMT and the
  `2001:1::1-3` anycast addresses — stay deliverable, as does the rest of the public internet; the
  one deliberate exception is the PCP/TURN anycast pair `192.0.0.9`/`192.0.0.10`, refused together
  with the `192.0.0.0/24` block that contains them. An
  operator who had the gate on and a webhook endpoint inside one of those ranges will see that
  endpoint refused from this release; the gate remains off by default, so nothing changes for
  anyone who has not opted in.
- The webhook delivery dial (DNS plus TCP connect) is now bounded by the 5-second per-phase budget
  rather than the full 10-second envelope, so a hung connect can no longer consume the whole budget
  and leave the TLS handshake and the response-header wait with nothing. The total is unchanged.

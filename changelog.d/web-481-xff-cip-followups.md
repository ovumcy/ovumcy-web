### Security

- **A forwarded hop with port `0` or a zero-padded port no longer counts as an address.** The
  rate-limit key reads such an `X-Forwarded-For` entry as unparsable and falls back to the
  direct peer, as it already did for an empty or non-numeric port. The security notes now name
  the one NAT64 case the webhook metadata guard does not decode: an operator-chosen
  network-specific prefix.

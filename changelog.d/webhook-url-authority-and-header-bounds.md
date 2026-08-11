### Changed

- **A webhook URL must now name a host and, if it carries a port, an in-range one.** `http://:8080/`
  parses with a non-empty authority but an empty hostname, so it passed the old "has a host" check —
  and Go's dialer reads an empty host as the unspecified address, i.e. the machine running Ovumcy. A
  port outside `1..65535` was likewise accepted and left for the transport to complain about. Both
  are now refused when the endpoint is saved **and** again before delivery, because save-time
  validation never revisits a URL already stored. An endpoint of this shape saved earlier will start
  failing with the usual invalid-URL message; re-save it with a hostname.

### Security

- **The webhook response envelope now bounds response headers, not just the body.** The body has
  always been capped at 8 KiB, but headers are read before that cap applies and took Go's 10 MiB
  default — three orders of magnitude past anything a webhook acknowledgement needs, read from an
  owner-controlled endpoint. Headers are now capped at 16 KiB, and the TLS handshake and the wait for
  response headers each get their own 5 s bound so one slow phase cannot spend the whole delivery
  budget. The total per-delivery timeout is unchanged.

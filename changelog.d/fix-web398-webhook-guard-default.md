### Security

- **Open registration now blocks private-address webhook egress by default, and cloud metadata is
  refused in every mode.** `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` defaulted to `false`, so on an
  instance with `REGISTRATION_MODE=open` anyone who signed up could make the notify pass POST to
  loopback, RFC 1918 or `169.254.169.254`, and read the 2xx back from settings as a liveness and
  port oracle for the operator's network. Unset, the guard now follows the registration mode: on
  under `open`, off under `closed`, where household owners target their LAN. An explicit value
  still wins. Whatever it says, `169.254.0.0/16` and `fd00:ec2::254` are refused, literal or
  resolved, through the same resolve-and-pin dialer. The server boot and `ovumcy notify` resolve
  the gate through one function.

  **Upgrading to 2.0.0:** an open-registration instance whose owners deliver to LAN ntfy, Gotify
  or Home Assistant stops reaching those targets until you set
  `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=false` (the startup warning then names the exposure) or close
  registration. A closed instance with more than one owner should set it to `true`. Webhook
  delivery ignores `HTTPS_PROXY`; delivering through an egress-only proxy is unsupported and now
  documented as such.

### Changed

- **Docs: the trusted address for a proxy outside the stack.** The self-hosting
  guide and `.env.example` now say which address `TRUSTED_PROXIES` must hold
  when the reverse proxy runs outside the app's compose stack: the address its
  requests arrive from inside the container (for a proxy on the Docker host,
  the bridge gateway), not the host's LAN IP. Trusting the wrong address made
  every sign-in fail with "Forbidden" and `reason="invalid referer"`.

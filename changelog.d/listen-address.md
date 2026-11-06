### Added

- **`LISTEN_ADDRESS` sets the one IP address the server listens on.** Unset, the server listens on
  every interface as before; set to an address such as `127.0.0.1` or `::1`, it listens there alone,
  so a binary run directly stays on loopback without a host firewall. A value that is not an IP
  address, such as a hostname or an address with a port, stops the start and names the variable.
  `ovumcy healthcheck` and `ovumcy readycheck` probe the configured address, and the startup line
  names it. The example compose stacks do not pass it, since their proxy and port publish reach the
  app on the container's own interface.

### Changed

- **The server listens on IPv6 as well as IPv4.** It listened on every IPv4 interface only, so an
  IPv6 client could not reach it. It now listens on every IPv4 and IPv6 interface, and the startup
  line reads `Ovumcy listening on :8080` instead of `Ovumcy listening on http://0.0.0.0:8080`.

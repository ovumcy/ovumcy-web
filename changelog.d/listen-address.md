### Added

- **`LISTEN_ADDRESS` sets the IP address the server listens on.** Unset, the server listens on every
  IPv4 interface exactly as before, so an upgrade opens nothing new. Set to `127.0.0.1` or `::1`, it
  listens there alone, so a binary run directly stays on loopback without a host firewall. Set to
  `::`, it listens on every IPv4 and IPv6 interface, which is how to accept IPv6 clients; make sure the
  host firewall filters IPv6 before setting it. `0.0.0.0` stays IPv4-only. A value that is not an IP
  address, such as a hostname or an address with a port, stops the start and names the variable.
  `ovumcy healthcheck` and `ovumcy readycheck` probe the configured address (a wildcard bind on IPv4
  loopback), and the startup line names it, as `Ovumcy listening on http://[::]:8080` for `::`. The
  example compose stacks do not pass it, since their proxy and port publish reach the app on the
  container's own interface.

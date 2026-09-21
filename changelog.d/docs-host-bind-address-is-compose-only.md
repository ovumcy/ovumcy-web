### Security

- **The documentation no longer implies that `HOST_BIND_ADDRESS` limits where the server listens.**
  Only the base `docker-compose.yml` reads it, as the host side of the port publish. The binary
  ignores it and listens on `PORT` on every interface, both inside the container and when started
  directly or with `go run`, so `HOST_BIND_ADDRESS=127.0.0.1` in `.env` did not keep a direct run off
  the network. The README, the self-hosting guide, `.env.example` and the security invariants now say
  this. They tell a direct run to block the port with a host firewall, and a plain `docker run` to
  publish it as `-p 127.0.0.1:8080:8080`. Runtime behaviour is unchanged.

### Changed

- **`.env.example` documents `LISTEN_ADDRESS`.** The variable appears there commented out, with the
  values it takes, so a non-docker install can copy it from the example. It stays commented out
  because compose passes `.env` into the container, where a loopback value breaks the port publish.

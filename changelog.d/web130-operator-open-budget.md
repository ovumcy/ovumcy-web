### Fixed

- **An account subcommand no longer hangs on a database that stops answering its schema check.** `ovumcy users`, `reset-password`, `link-oidc-identity`, `webhook` and `notify` now give that check the same five-minute storage budget the server's boot passes have, and refuse instead of waiting forever.

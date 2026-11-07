### Changed

- **Startup logs what the migration runner did.** The server prints one line at boot: either
  `migrations: applied N (FROM -> TO)` or `migrations: none pending, schema version V`, so an upgrade
  can be confirmed from the log. It carries no connection detail.

### Fixed

- **The restore walkthrough warns about the shipped compose names.** The named-volume restore section
  now says the compose file fixes the container name `ovumcy` and the volumes `ovumcy_data` and
  `ovumcy_fence`, so a restore into a scratch project must override them, and gives an example.

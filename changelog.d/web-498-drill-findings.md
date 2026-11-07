### Changed

- **Startup logs the migration result.** After migrations apply, one info line states how many
  migrations are recorded and the resulting schema version. It carries no connection detail.

### Fixed

- **The restore walkthrough warns about the shipped compose names.** The named-volume restore section
  now says the compose file fixes the container name `ovumcy` and the volumes `ovumcy_data` and
  `ovumcy_fence`, so a restore into a scratch project must override them, and gives an example.

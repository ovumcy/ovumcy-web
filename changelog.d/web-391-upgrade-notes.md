### Changed

- **Upgrading to 2.0.0 from v1.9.x — read before the first start.** The full list is in
  [docs/self-hosted.md → Upgrading to 2.0.0](docs/self-hosted.md#upgrading-to-200).
  - Every calendar feed armed under v1.9.2 or earlier is disarmed on the first start: those
    tokens predate the keyed verifier and nothing records which `SECRET_KEY` minted them.
    Owners generate a new subscribe URL from `Settings → Calendar feed`.
  - Replace the compose file with this release's. Without the `ovumcy_fence` volume the start
    is refused; `CALENDAR_FEED_FENCE_OPTOUT=true` boots anyway, disarming every feed on every
    start and leaving a restored backup free to bring back sessions and webhook endpoints.
  - A `.env` v1.9.2 accepted can stop the start: `COOKIE_SECURE`, `HSTS_ENABLED`,
    `TRUST_PROXY_ENABLED`, `WEBHOOK_BLOCK_PRIVATE_ADDRESSES`, `AUDIT_LOG_ENABLED` and
    `OIDC_ENABLED` refuse an unreadable value instead of using the default, `TRUSTED_PROXIES`
    entries must be valid and canonical, and `CALENDAR_FEED_FENCE_PATH` must be absolute.
  - `RATE_LIMIT_*` values outside the new ranges are logged and replaced by the defaults.
  - Downgrading to v1.9.x has new caveats (stale auto-fill marks, the feed's last-polled date):
    [Downgrade Caveats](docs/self-hosted.md#downgrade-caveats).

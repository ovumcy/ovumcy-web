### Security

- **The settings page no longer shows any part of a webhook endpoint's path.** The destination
  line showed the first path segment in clear, and for a Telegram bot URL
  (`https://api.telegram.org/bot<id>:<token>/sendMessage`) that segment is the bot token. The line
  now shows the host and, when it is not the default, the port — nothing else.
- **A removed webhook endpoint can no longer be re-armed by a settings save that was already on
  its way.** A save that keeps the stored endpoint read it, then wrote it back; a removal landing
  between the two was undone. Every webhook-settings save is now pinned to the settings it was
  decided from, and one that finds them changed writes nothing and asks the owner to check the
  current settings and try again (HTTP 409 for API clients).
- **A webhook URL field holding only spaces no longer deletes the stored endpoint.** It is now
  read as the blank "keep the stored endpoint" field.

### Changed

- **`ovumcy webhook set` reads `OVUMCY_WEBHOOK_URL` only when `--url-env` is given.** An exported
  variable used to replace the stored endpoint on any edit, even one that only flipped a toggle.
  Scripts that set the endpoint through the variable must add `--url-env`. `--url-env`,
  `--url-stdin` and `--clear-url` are mutually exclusive.
- **`ovumcy webhook set --clear-url` now also turns delivery off** unless `--enabled=true` is
  given (which is refused, as a webhook with no endpoint). Clearing used to fail while delivery
  was on, so withdrawing an endpoint took two commands.

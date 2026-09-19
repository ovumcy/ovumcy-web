### Added

- **Webhook reminders can be delivered in ntfy's native shape.** A webhook URL ending in
  `?format=ntfy` (or carrying `format=ntfy` among its other query parameters) now receives the
  reminder title in `X-Title`, an emoji tag per reminder kind in `X-Tags`, and a `text/plain` body of
  the message followed by the disclaimer, instead of the JSON envelope an ntfy topic would show as a
  raw blob. The disclaimer is sent on every notification in this format too. Other query parameters
  such as `?auth=` pass through unchanged, and a title carrying control characters drops the header
  rather than failing the delivery. URLs without `format=ntfy` keep the JSON envelope byte for byte;
  timeouts, the no-redirect rule, the response caps and host-only logging are identical in both
  formats.

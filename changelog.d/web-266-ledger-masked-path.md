### Security

- **The settings egress ledger now tells two webhook endpoints on one host apart.** The destination
  line shows `host[:port]` plus a masked path: the first path segment in clear, then `/…/`, then
  the last four characters (for example `hooks.slack.com/services/…/abcd`). A first segment that
  looks like a credential — it contains `:`, or is 20 or more letters, digits, `_` or `-` — is
  masked too, so a Telegram bot URL shows `api.telegram.org/…/sage` and never the bot token. A
  single-segment path is never shown whole. The query, fragment and userinfo are never shown.

### Fixed

- **Three settings/privacy strings no longer fall back to English in `de`, `es`, `fr`, `it`, `ru`.**
  `privacy.third_parties.ledger_link`, `settings.success.webhook_removed`, and
  `settings.error.webhook_url_unreadable` shipped with the English source text copied verbatim into
  all five non-English catalogues; they now carry a native translation matching each locale's
  existing webhook/endpoint terminology.

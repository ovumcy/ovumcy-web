### Fixed

- **The API reference now lists the language switch's `403` and `500`.** `docs/openapi.yaml`
  now lists the `403` the CSRF check gives `POST /lang` for a missing, mismatched or expired
  token or a cross-site request, with the shared `forbidden` key, and the `500` a signed-in owner
  gets when the chosen language cannot be stored on the account, with the key `failed to update
  interface settings` and no language cookie set. Each one says which callers get the JSON
  envelope and which get an HTML fragment instead. The server's behaviour is unchanged.

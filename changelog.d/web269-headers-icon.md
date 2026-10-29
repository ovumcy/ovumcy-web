### Fixed

- **Three small server nits.** Every 404 answer now carries `X-Robots-Tag: noindex`; the retired `interest-cohort` token is gone from `Permissions-Policy` (browsers logged an "unrecognized feature" warning for it); a custom symptom icon of a bare `?` is refused, since `?` is the glyph the interface paints for an icon it cannot show.

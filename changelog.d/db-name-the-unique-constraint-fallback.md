none

Dead-code removal with no user-visible effect: the persistence layer's
duplicate-key classifier no longer sniffs the driver's error text. It kept a
fallback that searched for SQLite's wording, `UNIQUE constraint failed:`, and
tried to read a constraint name out of it. That branch could never run on
either supported dialect: the SQLite driver's error translator replaces the
driver error with GORM's bare duplicate-key sentinel and throws the wording
away, and PostgreSQL words its own refusal differently and would not match the
marker even if it did survive. Classification now reads the translated sentinel
alone — the signal both dialects genuinely produce, and the one the database
configuration is already tested to enable.

The name a duplicate-key error carries is documented for what it is: the
constraint the calling repository declares for the write, not a name read back
from the database's refusal. It had always been caller-supplied while reading
as though it were derived from the schema, and the deleted branch was the only
thing that made the other reading plausible. No caller branches on the value —
the three consumers in the service layer test only that the write was refused
by a unique index — so refusing a duplicate email, a duplicate OIDC identity or
a duplicate symptom name reaches the owner exactly as before.

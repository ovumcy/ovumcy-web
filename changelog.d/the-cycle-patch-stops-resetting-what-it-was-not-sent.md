### Fixed

- **Breaking (API shape): `PATCH /api/v1/users/current/cycle` no longer rewrites the settings a
  request never mentioned.** A JSON save that carried the cycle geometry alone was read as a full
  snapshot, so every member it did not name was written back as that member's zero value. The
  expensive one was the tracking mode: an owner tracking to **avoid pregnancy** was silently moved
  to general health tracking, which reframes the fertile window, the badges and the summaries
  across every surface — a mode the product otherwise changes only on an explicit owner action.
  The age bracket was wiped to unspecified the same way, and the three cycle flags were cleared.
  The endpoint is now the partial update its verb promises: a member the body carries is written
  (including one carrying `false` or `""`, which is a value, not an absence), and a member it omits
  is left exactly as it stands. What changes for a client that was relying on the old behaviour: to
  clear a flag or a bracket, send it explicitly rather than leaving it out.

  The dashboard's mode quick switch is fixed on the same edge — it used to take a one-column path
  whenever the two lengths were missing, so a flag travelling beside the mode was dropped — and its
  success body no longer echoes the stored `usage_goal`, which made it the one save whose `200`
  a client validating against the published `OkResponse` schema rejected.

  The settings form is unaffected. It submits every control it owns on every save, and an unchecked
  box submits nothing at all, so a form body has no absent members to honour and is still read as
  the full snapshot it is — the toggles keep switching off.

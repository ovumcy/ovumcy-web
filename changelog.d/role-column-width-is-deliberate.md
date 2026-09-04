none

Documentation only: records why `users.role`'s CHECK still admits `'partner'` — the leftover value
is what lets the containment tests build a row carrying a role the product does not have, so
narrowing it would delete that coverage rather than close a gap. No code or schema changes.

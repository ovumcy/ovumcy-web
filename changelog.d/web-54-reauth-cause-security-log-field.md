none

Fix round for this PR's own settings re-auth merge: adds a `reauth_cause`
security-log field (server log only, never the response) so an operator can
still tell "wrong password" apart from "no local password" now that the
caller-visible refusal is byte-identical, and corrects docs/comments that
overclaimed the distinction was already logged, plus a stale comment about
which state actually triggers the merged refusal. No caller-visible behavior
changes.

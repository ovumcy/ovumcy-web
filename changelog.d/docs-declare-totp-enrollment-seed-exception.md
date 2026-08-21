### Internal

- **The security docs now declare the TOTP enrollment seed as the second bounded exception to *no
  usable secret in transport*.** The enrollment page has always rendered the seed — as the QR code an
  authenticator scans and as the manual-entry string beneath it, which is the only way an
  authenticator can be provisioned — while the invariant text named TOTP secrets flatly among the
  values that never reach transport and called the `.ics` capability token the single exception. The
  code was right and the text was short: `docs/SECURITY_INVARIANTS.md`, `docs/security/cryptography.md`
  and the `SECURITY.md` matrix now state the exception together with the bounds that make it safe —
  minted fresh on every visit and never re-served from storage, held only in the sealed owner-bound
  five-minute enrollment cookie until the first code verifies, written to the account (field-encrypted,
  AAD-bound) only by that verification, and shown only on an authenticated owner page. The `.ics`
  token remains the only exception for a value usable on its own; the enrollment seed is a
  not-yet-active credential, inert until the account confirms it. No behaviour changes.

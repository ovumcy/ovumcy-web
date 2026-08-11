### Internal

- **The webhook egress invariants get their own section in `docs/SECURITY_INVARIANTS.md`.** They had
  accreted into a single 2400-character bullet — twice the file's next longest — fusing five separate
  topics: envelope hardening, the private-address block, host-only logging, the write-only URL in
  settings, and the operator CLI's output discipline. Split into flat bullets under
  *Webhook notifications (outbound egress)*, mirroring how the other egress subsystem, the `.ics`
  calendar feed, is already laid out. No invariant changes meaning.

  The same pass carries two claims the public mirror was missing, both already true in code and
  already recorded in `SECURITY.md`: that a saved endpoint must name a host and an in-range port,
  re-checked before delivery, and that the response header block is explicitly capped.

none

Test-only: the recovery-lookup timing barrier gained a second guard that reads
the shipped `equalizeRecoveryCodeLookupTiming` body and pins each of its two
`bcrypt.CompareHashAndPassword` calls to the exact hash and operand it must
compare — the recovery code against its placeholder, the password against
its own. The existing guard only counted the calls, so a body comparing the
wrong operand against a placeholder still passed it. No wall-clock assertion
is involved.

none

Dead-code removal with no user-visible effect: the two `internal/security`
declarations only tests could reach — `EncryptFieldNoAADForTest` and the
`SealedCipher.NonceSize` accessor. The AEAD construction, the sealed-cookie
envelope and the legacy-ciphertext fallback are untouched, and every assertion
that stood on those declarations now runs against the shipping path instead.

none

Tests only: the two sealing assertions on the flash and recovery-code cookies
decoded the whole cookie value instead of the payload behind the `v2.` version
envelope, so the decode always failed and the check nested behind it never ran.
Both now split the envelope, decode only the payload, and refuse a hand-built
`v2.` + base64url(plaintext JSON) cookie. No product code.

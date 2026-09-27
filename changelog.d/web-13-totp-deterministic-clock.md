none

Test-only fix: `TestTOTPService_ValidateCodeRaw_Invalid` and
`TestVerifyTOTP2FAEnrollment_InvalidCode_DoesNotEnable` asserted a fixed
literal code ("000000") was invalid, with a comment admitting it was "possible
but extremely unlikely" to coincide with a real code — a flake, not a proof.
Both now compute a code that is proven invalid across the whole ±1-step skew
window the TOTP validator checks at the instant it runs, using a wider ±3-step
exclusion margin so a step boundary crossed between test setup and the call
under test cannot narrow it, and assert the precondition (`totp.Validate`
returns false for that code, right now) directly in the test body before
exercising the path under test. `TOTPService.ValidateCodeRaw` has no
injectable clock — it forwards straight to `totp.Validate`, which reads
`time.Now()` itself — so this is a test-side fix; no production code changed.

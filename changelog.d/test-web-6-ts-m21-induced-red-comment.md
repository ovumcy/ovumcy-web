none

Doc-only: TS-M21 (WEB-6) asked for the verifier/fingerprint check's induced-red
proof to be written down, not just implied by an existing test. Added a comment
to `TestVerifyCalendarFeedTokenRejectsWrongVerifier` naming the mutant
(verifierMatch hardcoded to 1, or the MAC check dropped) and its two observable
results: this unit test flips PASS, and the API-level no-oracle regression's
"wrongVerifier" case flips from 404 to 200. No test logic or assertions changed.

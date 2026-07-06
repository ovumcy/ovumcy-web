package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

// These tests guard the AEAD-sealed cookie codec against the four classes of
// breakage that "ovumcy.cookie.<purpose>" AAD-binding is supposed to prevent:
// cross-purpose reuse, ciphertext tampering, foreign-key acceptance, and
// truncation. They run codec-level (no Fiber, no DB) so failures point
// directly at the codec rather than at integration glue.

func TestSecureCookieCodecRoundtripsAllKnownPurposes(t *testing.T) {
	t.Parallel()

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}

	purposes := []string{
		authCookieName,
		flashCookieName,
		recoveryCodeCookieName,
		calendarFeedRevealCookieName,
		registerPickupCookieName,
		resetPasswordCookieName,
		oidcStateCookieName,
		oidcStepupCookieName,
		oidcLogoutBridgeCookieName,
		oidcLinkPendingCookieName,
		totpPendingCookieName,
		totpSetupCookieName,
	}
	plaintext := []byte(`{"hello":"world","n":42}`)

	for _, purpose := range purposes {

		t.Run(purpose, func(t *testing.T) {
			t.Parallel()

			sealed, err := codec.seal(purpose, plaintext)
			if err != nil {
				t.Fatalf("seal under %q: %v", purpose, err)
			}
			recovered, err := codec.open(purpose, sealed)
			if err != nil {
				t.Fatalf("open under %q: %v", purpose, err)
			}
			if string(recovered) != string(plaintext) {
				t.Fatalf("expected plaintext to round-trip, got %q", recovered)
			}
		})
	}
}

func TestSecureCookieCodecRejectsCrossPurposeOpen(t *testing.T) {
	t.Parallel()

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}

	sealed, err := codec.seal(authCookieName, []byte("payload-for-auth"))
	if err != nil {
		t.Fatalf("seal under auth purpose: %v", err)
	}

	otherPurposes := []string{
		flashCookieName,
		recoveryCodeCookieName,
		calendarFeedRevealCookieName,
		registerPickupCookieName,
		resetPasswordCookieName,
		oidcStateCookieName,
		oidcStepupCookieName,
		oidcLogoutBridgeCookieName,
		oidcLinkPendingCookieName,
		totpPendingCookieName,
		totpSetupCookieName,
	}
	for _, purpose := range otherPurposes {

		t.Run("opened_as_"+purpose, func(t *testing.T) {
			t.Parallel()

			if _, err := codec.open(purpose, sealed); !errors.Is(err, errInvalidSecureCookieValue) {
				t.Fatalf("expected AAD-binding to reject cross-purpose open as %q, got %v", purpose, err)
			}
		})
	}
}

func TestSecureCookieCodecRejectsTamperedCiphertext(t *testing.T) {
	t.Parallel()

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}

	sealed, err := codec.seal(authCookieName, []byte("genuine-payload"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	version, encoded, _ := strings.Cut(sealed, ".")
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode sealed payload: %v", err)
	}
	if len(payload) < 16 {
		t.Fatalf("unexpectedly short sealed payload: %d bytes", len(payload))
	}

	// Flip the last byte (inside the GCM auth tag).
	tamperedTail := append([]byte{}, payload...)
	tamperedTail[len(tamperedTail)-1] ^= 0xFF
	tamperedTailEncoded := version + "." + base64.RawURLEncoding.EncodeToString(tamperedTail)
	if _, err := codec.open(authCookieName, tamperedTailEncoded); !errors.Is(err, errInvalidSecureCookieValue) {
		t.Fatalf("expected tampered auth tag to be rejected, got %v", err)
	}

	// Flip a byte in the middle (inside ciphertext body, past the nonce).
	nonceSize := codec.aead.NonceSize()
	tamperedBody := append([]byte{}, payload...)
	tamperedBody[nonceSize+1] ^= 0x01
	tamperedBodyEncoded := version + "." + base64.RawURLEncoding.EncodeToString(tamperedBody)
	if _, err := codec.open(authCookieName, tamperedBodyEncoded); !errors.Is(err, errInvalidSecureCookieValue) {
		t.Fatalf("expected tampered ciphertext byte to be rejected, got %v", err)
	}

	// Flip a byte in the nonce.
	tamperedNonce := append([]byte{}, payload...)
	tamperedNonce[0] ^= 0x01
	tamperedNonceEncoded := version + "." + base64.RawURLEncoding.EncodeToString(tamperedNonce)
	if _, err := codec.open(authCookieName, tamperedNonceEncoded); !errors.Is(err, errInvalidSecureCookieValue) {
		t.Fatalf("expected tampered nonce to be rejected, got %v", err)
	}
}

func TestSecureCookieCodecRejectsTamperedCiphertextForTOTPAndLinkPendingCookies(t *testing.T) {
	t.Parallel()

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}

	for _, purpose := range []string{oidcLinkPendingCookieName, totpPendingCookieName, totpSetupCookieName} {

		t.Run(purpose, func(t *testing.T) {
			t.Parallel()

			sealed, err := codec.seal(purpose, []byte(`{"step":1}`))
			if err != nil {
				t.Fatalf("seal under %q: %v", purpose, err)
			}

			version, encoded, _ := strings.Cut(sealed, ".")
			payload, err := base64.RawURLEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("decode sealed payload: %v", err)
			}
			payload[len(payload)-1] ^= 0xFF // flip a byte inside the GCM auth tag
			tampered := version + "." + base64.RawURLEncoding.EncodeToString(payload)

			if _, err := codec.open(purpose, tampered); !errors.Is(err, errInvalidSecureCookieValue) {
				t.Fatalf("expected tampered %q ciphertext to be rejected, got %v", purpose, err)
			}
		})
	}
}

func TestSecureCookieCodecRejectsForeignKeySigning(t *testing.T) {
	t.Parallel()

	sealingCodec, err := newSecureCookieCodec([]byte("primary-secret-key"))
	if err != nil {
		t.Fatalf("new sealing codec: %v", err)
	}
	openingCodec, err := newSecureCookieCodec([]byte("rotated-secret-key"))
	if err != nil {
		t.Fatalf("new opening codec: %v", err)
	}

	sealed, err := sealingCodec.seal(authCookieName, []byte("classified-payload"))
	if err != nil {
		t.Fatalf("seal with primary key: %v", err)
	}

	if _, err := openingCodec.open(authCookieName, sealed); !errors.Is(err, errInvalidSecureCookieValue) {
		t.Fatalf("expected payload sealed by primary key to be rejected by rotated key, got %v", err)
	}
}

func TestSecureCookieCodecRejectsTruncatedPayload(t *testing.T) {
	t.Parallel()

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}

	cases := []struct {
		name  string
		value string
	}{
		{name: "empty_payload_after_version", value: secureCookieVersion + "."},
		{name: "missing_version_separator", value: "no-separator-payload"},
		{name: "version_only", value: secureCookieVersion},
		{name: "wrong_version_prefix", value: "v9." + base64.RawURLEncoding.EncodeToString(randomBytes(t, 32))},
		{name: "non_base64_payload", value: secureCookieVersion + ".!!not-base64!!"},
		{name: "shorter_than_nonce", value: secureCookieVersion + "." + base64.RawURLEncoding.EncodeToString(randomBytes(t, 8))},
		{name: "exactly_nonce_no_ciphertext", value: secureCookieVersion + "." + base64.RawURLEncoding.EncodeToString(randomBytes(t, codec.aead.NonceSize()))},
	}
	for _, tc := range cases {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := codec.open(authCookieName, tc.value); !errors.Is(err, errInvalidSecureCookieValue) {
				t.Fatalf("expected truncated/malformed payload %q to be rejected, got %v", tc.name, err)
			}
		})
	}
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()

	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("read random bytes: %v", err)
	}
	return buf
}

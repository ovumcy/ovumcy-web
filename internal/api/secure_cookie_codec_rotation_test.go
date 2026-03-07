package api

import (
	"errors"
	"strings"
	"testing"
)

const legacyV1AuthCookieValueForTest = "v1.AQIDBAUGBwgJCgsMgMCNVdfeoxzg5RdfW2gB0u8QSayB"

func TestSecureCookieCodecSealsWithVersion2Prefix(t *testing.T) {
	t.Parallel()

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}

	encoded, err := codec.seal(authCookieName, []byte("token"))
	if err != nil {
		t.Fatalf("seal secure cookie payload: %v", err)
	}

	if !strings.HasPrefix(encoded, secureCookieVersion+".") {
		t.Fatalf("expected secure cookie prefix %q, got %q", secureCookieVersion+".", encoded)
	}
}

func TestSecureCookieCodecRejectsLegacyV1Payload(t *testing.T) {
	t.Parallel()

	codec, err := newSecureCookieCodec([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("new secure cookie codec: %v", err)
	}

	if _, err := codec.open(authCookieName, legacyV1AuthCookieValueForTest); !errors.Is(err, errInvalidSecureCookieValue) {
		t.Fatalf("expected invalid legacy secure cookie error, got %v", err)
	}
}

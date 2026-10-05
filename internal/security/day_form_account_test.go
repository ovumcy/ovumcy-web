package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
)

func TestDayFormAccountBindingIsOpaqueAndPerAccount(t *testing.T) {
	t.Parallel()

	secret := []byte("day-form-account-test-secret-key-0123456789")
	first, err := DayFormAccountBinding(secret, 42)
	if err != nil {
		t.Fatalf("bind account 42: %v", err)
	}
	again, err := DayFormAccountBinding(secret, 42)
	if err != nil {
		t.Fatalf("bind account 42 again: %v", err)
	}
	if first != again {
		t.Fatalf("one account must render one binding, got %q then %q", first, again)
	}
	if decoded, decodeErr := base64.RawURLEncoding.DecodeString(first); decodeErr != nil || len(decoded) != sha256.Size {
		t.Fatalf("binding %q must be a base64url HMAC-SHA256 (err=%v)", first, decodeErr)
	}

	second, err := DayFormAccountBinding(secret, 43)
	if err != nil {
		t.Fatalf("bind account 43: %v", err)
	}
	if first == second {
		t.Fatal("two accounts must not share a binding")
	}

	otherSecret, err := DayFormAccountBinding([]byte("another-secret-key-entirely-0123456789"), 42)
	if err != nil {
		t.Fatalf("bind under another secret: %v", err)
	}
	if otherSecret == first {
		t.Fatal("the binding must be keyed by SECRET_KEY")
	}

	// Purpose separation: the same id MACed under the auth-session key must not
	// reproduce the binding.
	sessionKey, err := DeriveTokenSigningKey(secret, AuthSessionTokenKeyLabel)
	if err != nil {
		t.Fatalf("derive session key: %v", err)
	}
	mac := hmac.New(sha256.New, sessionKey)
	_, _ = mac.Write([]byte("42"))
	if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) == first {
		t.Fatal("the binding key must be separated from the auth-session key")
	}
}

func TestDayFormAccountBindingRefusesMissingOperands(t *testing.T) {
	t.Parallel()

	if _, err := DayFormAccountBinding([]byte("secret"), 0); !errors.Is(err, ErrDayFormAccountIDRequired) {
		t.Fatalf("a zero account id must be refused, got %v", err)
	}
	if _, err := DayFormAccountBinding(nil, 7); !errors.Is(err, ErrTokenSigningKeyMissing) {
		t.Fatalf("a missing secret must be refused, got %v", err)
	}
}

func TestDayFormAccountBindingMatches(t *testing.T) {
	t.Parallel()

	secret := []byte("day-form-account-match-secret-0123456789")
	own, err := DayFormAccountBinding(secret, 5)
	if err != nil {
		t.Fatalf("bind account 5: %v", err)
	}
	other, err := DayFormAccountBinding(secret, 6)
	if err != nil {
		t.Fatalf("bind account 6: %v", err)
	}

	cases := []struct {
		name      string
		secret    []byte
		userID    uint
		presented string
		want      bool
	}{
		{name: "the rendering account matches", secret: secret, userID: 5, presented: own, want: true},
		{name: "another account's binding does not match", secret: secret, userID: 5, presented: other, want: false},
		{name: "an empty value does not match", secret: secret, userID: 5, presented: "", want: false},
		{name: "a zero account id never matches", secret: secret, userID: 0, presented: own, want: false},
		{name: "a missing secret never matches", secret: nil, userID: 5, presented: own, want: false},
		{name: "a truncated binding does not match", secret: secret, userID: 5, presented: own[:len(own)-1], want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := DayFormAccountBindingMatches(testCase.secret, testCase.userID, testCase.presented); got != testCase.want {
				t.Fatalf("DayFormAccountBindingMatches = %v, want %v", got, testCase.want)
			}
		})
	}
}

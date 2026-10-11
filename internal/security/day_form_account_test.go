package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync"
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

// dayFormAccountGoldenSecret and dayFormAccountGolden pin the exact bytes a day
// form carries: the values were computed independently of this package
// (HKDF-SHA256 over the secret with the token-signing salt and the day-form
// label, then HMAC-SHA256 of the decimal id, base64url without padding), so a
// change to how the key is derived, cached or encoded cannot pass by agreeing
// with itself.
const dayFormAccountGoldenSecret = "golden-secret-key-for-day-form-binding-0123456789"

var dayFormAccountGolden = map[uint]string{
	1:          "19C4OPHIHuv36QyKUEIqZW2DI6t_B4eeXH6qF1vyQec",
	42:         "fHeSKhj9g4IguQ4sepR32tlYiHovmtGUyJu2w_ed57w",
	4294967295: "BiqXItbi4672LLGRhGMYdCypWLYUGZCYkU3aGl9bJfM",
}

func TestDayFormAccountBindingGoldenValues(t *testing.T) {
	t.Parallel()

	secret := []byte(dayFormAccountGoldenSecret)
	binder := NewDayFormAccountBinder(secret)
	for userID, want := range dayFormAccountGolden {
		// Twice from the cached key: reuse must not drift the output.
		for range 2 {
			got, err := binder.Binding(userID)
			if err != nil || got != want {
				t.Fatalf("binder.Binding(%d) = %q, %v; want %q", userID, got, err, want)
			}
			if !binder.Matches(userID, want) {
				t.Fatalf("binder.Matches(%d) refused the golden binding", userID)
			}
		}
		oneShot, err := DayFormAccountBinding(secret, userID)
		if err != nil || oneShot != want {
			t.Fatalf("DayFormAccountBinding(%d) = %q, %v; want %q", userID, oneShot, err, want)
		}
		if !DayFormAccountBindingMatches(secret, userID, want) {
			t.Fatalf("DayFormAccountBindingMatches(%d) refused the golden binding", userID)
		}
	}
	if binder.Matches(1, dayFormAccountGolden[42]) {
		t.Fatal("another account's golden binding must not match")
	}
}

func TestDayFormAccountBinderIsSafeAcrossGoroutines(t *testing.T) {
	t.Parallel()

	binder := NewDayFormAccountBinder([]byte(dayFormAccountGoldenSecret))
	var group sync.WaitGroup
	for worker := range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			for range 200 {
				userID := uint(1)
				if worker%2 == 1 {
					userID = 42
				}
				got, err := binder.Binding(userID)
				if err != nil || got != dayFormAccountGolden[userID] || !binder.Matches(userID, got) {
					t.Errorf("shared binder drifted for account %d: %q, %v", userID, got, err)
					return
				}
			}
		}()
	}
	group.Wait()
}

func TestDayFormAccountBinderDerivesTheKeyOnce(t *testing.T) {
	secret := []byte(dayFormAccountGoldenSecret)
	binder := NewDayFormAccountBinder(secret)
	cached := testing.AllocsPerRun(50, func() { _, _ = binder.Binding(42) })
	rederived := testing.AllocsPerRun(50, func() { _, _ = DayFormAccountBinding(secret, 42) })
	if cached >= rederived {
		t.Fatalf("a cached binder must allocate less than a per-call derivation, got %v vs %v", cached, rederived)
	}
}

func BenchmarkDayFormAccountBinderBinding(b *testing.B) {
	binder := NewDayFormAccountBinder([]byte(dayFormAccountGoldenSecret))
	b.ReportAllocs()
	for b.Loop() {
		_, _ = binder.Binding(42)
	}
}

func BenchmarkDayFormAccountBindingPerCallDerivation(b *testing.B) {
	secret := []byte(dayFormAccountGoldenSecret)
	b.ReportAllocs()
	for b.Loop() {
		_, _ = DayFormAccountBinding(secret, 42)
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

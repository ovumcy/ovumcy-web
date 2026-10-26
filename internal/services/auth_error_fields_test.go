package services

import "testing"

func TestInvalidAuthFieldsNamesEveryComparedOperandOrNone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want []string
	}{
		// An enumeration-safe refusal names both operands, never the wrong one.
		{key: "auth.error.invalid_credentials", want: []string{"email", "password"}},
		{key: "auth.error.invalid_recovery_code", want: []string{"recovery_code", "password"}},
		{key: "auth.error.password_mismatch", want: []string{"confirm_password"}},
		{key: "auth.error.consent_required", want: []string{"consent"}},
		{key: "error.totp_invalid_code", want: []string{"code"}},
		// A refusal about no field in particular marks none.
		{key: "auth.error.too_many_login_attempts", want: nil},
		{key: "", want: nil},
	}
	for _, testCase := range tests {
		t.Run(testCase.key, func(t *testing.T) {
			t.Parallel()
			got := InvalidAuthFields(testCase.key)
			if got == nil {
				t.Fatal("expected a non-nil map so templates can index it")
			}
			if len(got) != len(testCase.want) {
				t.Fatalf("InvalidAuthFields(%q) = %v, want %v", testCase.key, got, testCase.want)
			}
			for _, name := range testCase.want {
				if !got[name] {
					t.Fatalf("InvalidAuthFields(%q) lacks %q: %v", testCase.key, name, got)
				}
			}
		})
	}
}

// Every key the map names must exist in the catalogue; a key named here that
// no page can render marks nothing. The catalogue side is pinned by
// TestEveryLocaleKeyIsReachableFromTheApplication in internal/i18n, which
// reads this map's literals as reachable keys.
func TestInvalidAuthFieldsKeysAreAuthPageErrorKeys(t *testing.T) {
	t.Parallel()
	for key := range invalidAuthFields {
		if key == "" || (len(key) < 11 || (key[:11] != "auth.error." && key[:6] != "error.")) {
			t.Fatalf("%q is not an auth-page error key", key)
		}
	}
}

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

// Every key the map names must be a locale key an auth page can actually
// resolve from an error source — a value of authErrorTranslationKeys — or the
// markup silently stops firing when that table is renamed while the notice
// still renders. The catalogue side is pinned by
// TestEveryLocaleKeyIsReachableFromTheApplication in internal/i18n.
func TestInvalidAuthFieldsKeysAreAuthPageErrorKeys(t *testing.T) {
	t.Parallel()
	resolvable := map[string]bool{}
	for _, key := range authErrorTranslationKeys {
		resolvable[key] = true
	}
	for key := range invalidAuthFields {
		if !resolvable[key] {
			t.Fatalf("%q is not a key AuthErrorTranslationKey resolves to; the markup it names would never render", key)
		}
	}
}

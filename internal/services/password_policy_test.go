package services

import (
	"errors"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

func TestValidatePasswordStrength_RejectsWeakPasswords(t *testing.T) {
	testCases := []string{
		"Short1",
		"alllowercase1",
		"ALLUPPERCASE1",
		"NoDigitsHere",
	}

	for _, password := range testCases {
		if err := ValidatePasswordStrength(password); !errors.Is(err, ErrWeakPassword) {
			t.Fatalf("expected ErrWeakPassword for %q, got %v", password, err)
		}
	}
}

func TestValidatePasswordStrength_AcceptsStrongPassword(t *testing.T) {
	if err := ValidatePasswordStrength("StrongPass1"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestValidatePasswordStrength_EnforcesBcryptByteLimit pins the 72-byte
// maximum: bcrypt rejects longer inputs, so the policy must catch them as a
// stable validation error before hashing. The boundary is bytes, not code
// points — a multi-byte passphrase can exceed it with far fewer characters.
func TestValidatePasswordStrength_EnforcesBcryptByteLimit(t *testing.T) {
	atLimit := "Aa1" + strings.Repeat("x", maxPasswordBytes-3)
	if len(atLimit) != maxPasswordBytes {
		t.Fatalf("test setup: at-limit password is %d bytes, want %d", len(atLimit), maxPasswordBytes)
	}
	if err := ValidatePasswordStrength(atLimit); err != nil {
		t.Fatalf("expected exactly-%d-byte password to pass, got %v", maxPasswordBytes, err)
	}

	overLimit := atLimit + "x"
	if err := ValidatePasswordStrength(overLimit); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword for %d-byte password, got %v", len(overLimit), err)
	}

	// 35 two-byte Cyrillic runes + ASCII classes = 38 runes but 73 bytes.
	multibyte := "Aa1" + strings.Repeat("ф", 35)
	if got := len(multibyte); got <= maxPasswordBytes {
		t.Fatalf("test setup: multibyte password is %d bytes, want > %d", got, maxPasswordBytes)
	}
	if err := ValidatePasswordStrength(multibyte); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword for multibyte over-limit password, got %v", err)
	}
}

// weakPasswordMaximumQualifiers names, per locale, the clause that ties the
// number 72 to the Latin alphabet. A locale sentence that states "72" without
// its qualifier is claiming a plain character maximum, which is exactly the
// claim a multi-byte passphrase falsifies. The clause is asserted rather than
// the whole sentence so ordinary copy edits stay free while the load-bearing
// half stays pinned.
var weakPasswordMaximumQualifiers = map[string]string{
	i18n.LangEN: "72 Latin characters",
	i18n.LangRU: "72 латинских символа",
	i18n.LangDE: "72 lateinische Zeichen",
	i18n.LangES: "72 caracteres latinos",
	i18n.LangFR: "72 caractères latins",
	i18n.LangIT: "72 caratteri latini",
}

// TestPasswordLengthCopyStatesTheMaximumInTheUnitEnforced pins the length rule
// in BOTH of its units at once — the branch and the sentence the owner reads
// about it.
//
// The branch is unchanged by this test and by the change that introduced it:
// 72 bytes is bcrypt's real input limit and every assertion below passes
// against the policy as it already stood. What was wrong was the copy. All six
// catalogues advertised "8 to 72 characters", so a 38-character Cyrillic
// passphrase — 73 bytes — was refused with a message it visibly satisfied,
// leaving no action to take. Non-Latin scripts hit the cap at roughly half the
// advertised length, emoji sooner.
//
// So the guard has two halves that must agree: the rejected passphrase below is
// well under any character count the copy could state, and the copy the owner
// receives for that rejection states its maximum in a unit that stays true for
// it. Rewriting the copy back into a bare character range fails here even
// though no branch moved.
func TestPasswordLengthCopyStatesTheMaximumInTheUnitEnforced(t *testing.T) {
	atLimit := "Aa1" + strings.Repeat("x", maxPasswordBytes-3)
	if len(atLimit) != maxPasswordBytes || len([]rune(atLimit)) != maxPasswordBytes {
		t.Fatalf("test setup: Latin password is %d bytes / %d runes, want %d of each", len(atLimit), len([]rune(atLimit)), maxPasswordBytes)
	}
	if err := ValidatePasswordStrength(atLimit); err != nil {
		t.Fatalf("expected a %d-byte Latin password to be accepted, got %v", maxPasswordBytes, err)
	}
	if err := ValidatePasswordStrength(atLimit + "x"); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword for a %d-byte Latin password, got %v", maxPasswordBytes+1, err)
	}

	// The owner's case: a passphrase whose character count is nowhere near the
	// advertised maximum, and whose byte count is past it.
	cyrillic := "Пароль1" + strings.Repeat("ы", 30)
	if runes := len([]rune(cyrillic)); runes > maxPasswordBytes {
		t.Fatalf("test setup: Cyrillic passphrase is %d runes, want no more than the %d a character-based reading of the copy would allow", runes, maxPasswordBytes)
	}
	if bytes := len(cyrillic); bytes <= maxPasswordBytes {
		t.Fatalf("test setup: Cyrillic passphrase is %d bytes, want > %d", bytes, maxPasswordBytes)
	}
	if err := ValidatePasswordStrength(cyrillic); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword for a %d-byte Cyrillic passphrase, got %v", len(cyrillic), err)
	}

	key := AuthErrorTranslationKey(ErrWeakPassword.Error())
	if key != "auth.error.weak_password" {
		t.Fatalf("AuthErrorTranslationKey(%q) = %q, want auth.error.weak_password", ErrWeakPassword.Error(), key)
	}

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	languages := manager.SupportedLanguages()
	if len(languages) != len(weakPasswordMaximumQualifiers) {
		t.Fatalf("weakPasswordMaximumQualifiers covers %d languages, the manager reports %d: %v", len(weakPasswordMaximumQualifiers), len(languages), languages)
	}

	for _, language := range languages {
		qualifier, ok := weakPasswordMaximumQualifiers[language]
		if !ok {
			t.Errorf("locale %q has no expected maximum qualifier: add one when adding the language", language)
			continue
		}
		messages := manager.Messages(language)
		for _, copyKey := range []string{key, "auth.password_requirements"} {
			sentence, ok := messages[copyKey]
			if !ok || sentence == "" {
				t.Errorf("locale %q has no entry for %q", language, copyKey)
				continue
			}
			if !strings.Contains(sentence, qualifier) {
				t.Errorf("locale %q, key %q: %q does not state the maximum as %q — a passphrase of %d characters is refused by a %d-byte cap, so a bare character maximum is false for it", language, copyKey, sentence, qualifier, len([]rune(cyrillic)), maxPasswordBytes)
			}
		}
	}
}

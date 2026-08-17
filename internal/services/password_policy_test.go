package services

import (
	"errors"
	"strconv"
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
	if err := ValidatePasswordStrength(overLimit); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong for %d-byte password, got %v", len(overLimit), err)
	}

	// 35 two-byte Cyrillic runes + ASCII classes = 38 runes but 73 bytes.
	multibyte := "Aa1" + strings.Repeat("ф", 35)
	if got := len(multibyte); got <= maxPasswordBytes {
		t.Fatalf("test setup: multibyte password is %d bytes, want > %d", got, maxPasswordBytes)
	}
	if err := ValidatePasswordStrength(multibyte); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong for multibyte over-limit password, got %v", err)
	}
}

// passwordLengthCopyKeys are the three owner-facing strings that describe the
// length rule: the resting hint under the field, and the two refusals the split
// produced.
var passwordLengthCopyKeys = []string{
	"auth.password_requirements",
	"auth.error.weak_password",
	"auth.error.password_too_long",
}

// passwordCountsAsSeveralQualifiers names, per locale, the clause carrying the
// one fact that makes a character-stated limit honest: a letter outside the
// Latin alphabet consumes more than one character's worth of the budget.
//
// The invariant it enforces is a CLASS, not a sentence — no owner-facing
// message may state a bare character maximum. So the clause is required of any
// of the three strings that names the maximum, whichever of them that turns out
// to be, rather than of one nominated key. That way moving the number between
// the hint and an error cannot slip the qualifier off it.
var passwordCountsAsSeveralQualifiers = map[string]string{
	i18n.LangEN: "count as several",
	i18n.LangRU: "считаются за несколько",
	i18n.LangDE: "zählen mehrfach",
	i18n.LangES: "cuentan varias veces",
	i18n.LangFR: "comptent plusieurs fois",
	i18n.LangIT: "contano più volte",
}

// overLimitCyrillicPassphrase is the owner's case throughout this file: 37
// characters — nowhere near any maximum the copy could state — but 73 bytes,
// and carrying an uppercase letter, a lowercase letter and a digit, so length
// is the ONLY rule it breaks.
func overLimitCyrillicPassphrase(t *testing.T) string {
	t.Helper()

	passphrase := "Пароль1" + strings.Repeat("ы", 30)
	if runes := len([]rune(passphrase)); runes > maxPasswordBytes {
		t.Fatalf("test setup: passphrase is %d runes, want no more than the %d a character-based reading of the copy would allow", runes, maxPasswordBytes)
	}
	if bytes := len(passphrase); bytes <= maxPasswordBytes {
		t.Fatalf("test setup: passphrase is %d bytes, want > %d", bytes, maxPasswordBytes)
	}
	return passphrase
}

// TestValidatePasswordStrengthDistinguishesTooLongFromWeak is the case that
// read as false to the owner: a passphrase that satisfies every composition
// rule and breaks only the byte limit was refused as "weak", under a message
// reciting requirements it already met. Length now has its own sentinel, and
// the two must not be aliases — a caller switching on ErrPasswordTooLong has to
// be able to miss ErrWeakPassword and vice versa.
func TestValidatePasswordStrengthDistinguishesTooLongFromWeak(t *testing.T) {
	if errors.Is(ErrPasswordTooLong, ErrWeakPassword) || errors.Is(ErrWeakPassword, ErrPasswordTooLong) {
		t.Fatal("ErrPasswordTooLong and ErrWeakPassword must stay distinct sentinels, or every caller's switch collapses back into one message")
	}

	atLimit := "Aa1" + strings.Repeat("x", maxPasswordBytes-3)
	if len(atLimit) != maxPasswordBytes || len([]rune(atLimit)) != maxPasswordBytes {
		t.Fatalf("test setup: Latin password is %d bytes / %d runes, want %d of each", len(atLimit), len([]rune(atLimit)), maxPasswordBytes)
	}
	if err := ValidatePasswordStrength(atLimit); err != nil {
		t.Fatalf("expected a %d-byte Latin password to be accepted, got %v", maxPasswordBytes, err)
	}
	if err := ValidatePasswordStrength(atLimit + "x"); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong for a %d-byte Latin password, got %v", maxPasswordBytes+1, err)
	}

	cyrillic := overLimitCyrillicPassphrase(t)
	if err := ValidatePasswordStrength(cyrillic); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong for a %d-character / %d-byte Cyrillic passphrase, got %v", len([]rune(cyrillic)), len(cyrillic), err)
	}

	// The composition failures keep the weak sentinel, so the split did not
	// simply rename one error into the other.
	for _, weak := range []string{"Short1", "alllowercase1", "ALLUPPERCASE1", "NoDigitsHere"} {
		if err := ValidatePasswordStrength(weak); !errors.Is(err, ErrWeakPassword) {
			t.Errorf("expected ErrWeakPassword for %q, got %v", weak, err)
		}
	}
}

// TestValidatePasswordStrengthReportsTooLongBeforeMissingClasses pins the
// precedence documented on ValidatePasswordStrength. A password that is both
// over the byte limit and missing a character class is reported as too long,
// because shortening is required either way while adding the missing class to
// an over-long password cannot make it acceptable — reporting the class first
// sends someone through an edit that is guaranteed to fail again.
func TestValidatePasswordStrengthReportsTooLongBeforeMissingClasses(t *testing.T) {
	// All-lowercase Cyrillic: no uppercase, no digit, and 80 bytes.
	bothBroken := strings.Repeat("ы", 40)
	if len(bothBroken) <= maxPasswordBytes {
		t.Fatalf("test setup: password is %d bytes, want > %d", len(bothBroken), maxPasswordBytes)
	}
	if len([]rune(bothBroken)) < 8 {
		t.Fatalf("test setup: password is %d runes, want at least 8 so the minimum is not what fires", len([]rune(bothBroken)))
	}

	if err := ValidatePasswordStrength(bothBroken); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong to win over the missing character classes, got %v", err)
	}
}

// TestPasswordLengthCopySeparatesTooLongFromWeakAndNeverStatesABareMaximum is
// the copy half of the split, and it enforces a class rather than three
// sentences:
//
//   - Whichever owner-facing string names the maximum must carry the
//     "counts as several" qualifier. A bare character maximum is the original
//     defect — a 37-character passphrase refused by a message promising 72.
//   - The weak-password message must not name the maximum at all. Reciting the
//     length rule inside the composition error is what forced both messages to
//     be long enough to push the submit button off a 375px screen.
//   - The too-long message must carry the qualifier, since it is the one place
//     the owner learns why a short-looking passphrase was refused.
//
// The branch behaviour asserted at the top is unchanged by the copy: the same
// passphrase was refused before the split and is refused after it. What changed
// is which of the two messages it now gets.
func TestPasswordLengthCopySeparatesTooLongFromWeakAndNeverStatesABareMaximum(t *testing.T) {
	cyrillic := overLimitCyrillicPassphrase(t)
	if err := ValidatePasswordStrength(cyrillic); !errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("expected ErrPasswordTooLong for the %d-character passphrase this copy is about, got %v", len([]rune(cyrillic)), err)
	}

	tooLongKey := AuthErrorTranslationKey(ErrPasswordTooLong.Error())
	if tooLongKey != "auth.error.password_too_long" {
		t.Fatalf("AuthErrorTranslationKey(%q) = %q, want auth.error.password_too_long", ErrPasswordTooLong.Error(), tooLongKey)
	}
	weakKey := AuthErrorTranslationKey(ErrWeakPassword.Error())
	if weakKey != "auth.error.weak_password" {
		t.Fatalf("AuthErrorTranslationKey(%q) = %q, want auth.error.weak_password", ErrWeakPassword.Error(), weakKey)
	}

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	languages := manager.SupportedLanguages()
	if len(languages) != len(passwordCountsAsSeveralQualifiers) {
		t.Fatalf("passwordCountsAsSeveralQualifiers covers %d languages, the manager reports %d: %v", len(passwordCountsAsSeveralQualifiers), len(languages), languages)
	}

	maximum := strconv.Itoa(maxPasswordBytes)
	for _, language := range languages {
		qualifier, ok := passwordCountsAsSeveralQualifiers[language]
		if !ok {
			t.Errorf("locale %q has no expected qualifier: add one when adding the language", language)
			continue
		}

		messages := manager.Messages(language)
		for _, copyKey := range passwordLengthCopyKeys {
			sentence, ok := messages[copyKey]
			if !ok || sentence == "" {
				t.Errorf("locale %q has no entry for %q", language, copyKey)
				continue
			}
			if strings.Contains(sentence, maximum) && !strings.Contains(sentence, qualifier) {
				t.Errorf("locale %q, key %q: %q names the maximum %s without the qualifier %q — a %d-character passphrase is refused by a %d-byte cap, so a bare character maximum is false for it",
					language, copyKey, sentence, maximum, qualifier, len([]rune(cyrillic)), maxPasswordBytes)
			}
		}

		if sentence := messages[weakKey]; strings.Contains(sentence, maximum) {
			t.Errorf("locale %q, key %q: %q still names the maximum — the length rule belongs to %q now, and reciting it here is what made both messages too long to fit a phone screen",
				language, weakKey, sentence, tooLongKey)
		}
		if sentence := messages[tooLongKey]; !strings.Contains(sentence, qualifier) {
			t.Errorf("locale %q, key %q: %q must carry the qualifier %q — it is the only place the owner learns why a short-looking passphrase was refused",
				language, tooLongKey, sentence, qualifier)
		}
	}
}

// TestIsChangePasswordErrorMessageCoversTheTooLongRefusal guards the half of
// the threading that fails by MISPLACEMENT rather than by absence: an
// unrecognised change-password key still renders its copy, but as a general
// settings error rather than one attached to the form the owner just submitted.
func TestIsChangePasswordErrorMessageCoversTheTooLongRefusal(t *testing.T) {
	if !IsChangePasswordErrorMessage(SettingsPasswordChangeKeyPasswordTooLong) {
		t.Fatal("the too-long key must route to the change-password form, not to the general settings error slot")
	}
	if got := ClassifySettingsErrorSource(SettingsPasswordChangeKeyPasswordTooLong); got != SettingsErrorTargetChangePassword {
		t.Fatalf("ClassifySettingsErrorSource(too long) = %q, want %q", got, SettingsErrorTargetChangePassword)
	}
}

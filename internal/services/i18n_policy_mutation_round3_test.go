package services

import "testing"

// i18n_policy.go:170:17 NEGATION `lastDigit == 1`->`!=`.
// 21 -> lastTwoDigits 21 (not teen), lastDigit 1 -> ONE.
func TestMR3I18n_RussianPluralFormOne21(t *testing.T) {
	if got := russianPluralForm(21, "ONE", "FEW", "MANY"); got != "ONE" {
		t.Fatalf("russianPluralForm(21) = %q, want ONE", got)
	}
}

// i18n_policy.go:172:17 BOUNDARY `lastDigit >= 2`->`> 2`.
// 2 -> lastDigit 2 -> FEW; the `> 2` mutant would route 2 to the default MANY.
func TestMR3I18n_RussianPluralFormFew2(t *testing.T) {
	if got := russianPluralForm(2, "ONE", "FEW", "MANY"); got != "FEW" {
		t.Fatalf("russianPluralForm(2) = %q, want FEW", got)
	}
}

// i18n_policy.go:172:35 BOUNDARY `lastDigit <= 4`->`< 4`.
// 4 and 24 -> lastDigit 4 -> FEW; the `< 4` mutant routes them to default MANY.
func TestMR3I18n_RussianPluralFormFew4(t *testing.T) {
	if got := russianPluralForm(4, "ONE", "FEW", "MANY"); got != "FEW" {
		t.Fatalf("russianPluralForm(4) = %q, want FEW", got)
	}
	if got := russianPluralForm(24, "ONE", "FEW", "MANY"); got != "FEW" {
		t.Fatalf("russianPluralForm(24) = %q, want FEW", got)
	}
}

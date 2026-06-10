package services

import "testing"

// TestI18npolicyCovRussianPluralTeenLowerBoundary verifies the LOWER boundary of
// the teen exception (11-14) on line 165. A CONDITIONALS_BOUNDARY mutation of
// `lastTwoDigits >= 11` to `lastTwoDigits > 11` would drop the value 11 from the
// teen range, causing values ending in ...11 to fall through to `case lastDigit
// == 1` and return the "one" form instead of the "many" (teen) form. Distinct
// sentinel strings make the one/many distinction observable.
func TestI18npolicyCovRussianPluralTeenLowerBoundary(t *testing.T) {
	// 11 is the lower edge of the teen range -> many (NOT one, despite lastDigit==1).
	if got := russianPluralForm(11, "ONE", "FEW", "MANY"); got != "MANY" {
		t.Fatalf("russianPluralForm(11): expected %q (teen/many), got %q", "MANY", got)
	}

	// 111 also ends in ...11 -> teen exception -> many.
	if got := russianPluralForm(111, "ONE", "FEW", "MANY"); got != "MANY" {
		t.Fatalf("russianPluralForm(111): expected %q (teen/many), got %q", "MANY", got)
	}

	// Contrast: 21 ends in ...21, lastDigit==1 and NOT a teen -> one form.
	if got := russianPluralForm(21, "ONE", "FEW", "MANY"); got != "ONE" {
		t.Fatalf("russianPluralForm(21): expected %q (one), got %q", "ONE", got)
	}
}

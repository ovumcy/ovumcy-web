package services

import "testing"

// i18npolicyCovRussianPluralNegativeValue verifies that russianPluralForm handles
// negative values correctly by normalising them to their absolute value before
// computing plural form. Covers lines 160–161 (the `if absolute < 0` branch).
func TestI18nPolicyRussianPluralNegativeValue(t *testing.T) {
	// A negative value whose absolute is 1 should return the "one" form,
	// not the "many" form (which would happen if negation were skipped).
	got := russianPluralForm(-1, "один", "несколько", "много")
	if got != "один" {
		t.Fatalf("russianPluralForm(-1): expected %q (one form), got %q", "один", got)
	}

	// -2 → absolute 2 → few form
	got = russianPluralForm(-2, "один", "несколько", "много")
	if got != "несколько" {
		t.Fatalf("russianPluralForm(-2): expected %q (few form), got %q", "несколько", got)
	}

	// -5 → absolute 5 → many form
	got = russianPluralForm(-5, "один", "несколько", "много")
	if got != "много" {
		t.Fatalf("russianPluralForm(-5): expected %q (many form), got %q", "много", got)
	}
}

// TestI18nPolicyRussianPluralTeenBoundary verifies the upper boundary of the
// teen exception (11–14). Line 164 mutant: `<= 14` might be changed to `<= 13`.
// Value 14 must return the "many" form (teen exception), not "few" (lastDigit == 4).
func TestI18nPolicyRussianPluralTeenBoundary(t *testing.T) {
	// 14 is in the teen range → many
	got := russianPluralForm(14, "раз", "раза", "раз")
	if got != "раз" {
		t.Fatalf("russianPluralForm(14): expected %q (many/teen), got %q", "раз", got)
	}

	// 15 is NOT a teen → lastDigit == 5 → many (different path, same result,
	//   but make sure 14 didn't accidentally fall into the switch)
	got = russianPluralForm(15, "раз", "раза", "раз")
	if got != "раз" {
		t.Fatalf("russianPluralForm(15): expected %q (many), got %q", "раз", got)
	}

	// 24 has lastDigit==4 → few (NOT a teen), so result differs from 14
	got = russianPluralForm(24, "раз", "раза", "раз")
	if got != "раза" {
		t.Fatalf("russianPluralForm(24): expected %q (few, lastDigit==4), got %q", "раза", got)
	}

	// 11, 12, 13 are all teens → many
	for _, v := range []int{11, 12, 13} {
		got = russianPluralForm(v, "раз", "раза", "раз")
		if got != "раз" {
			t.Fatalf("russianPluralForm(%d): expected %q (many/teen), got %q", v, "раз", got)
		}
	}
}

// TestI18nPolicyRussianPluralSwitchCases directly exercises lines 170 and 172
// (the switch cases for lastDigit==1 and lastDigit in 2–4).
func TestI18nPolicyRussianPluralSwitchCases(t *testing.T) {
	tests := []struct {
		value    int
		wantForm string // "one", "few", or "many" signalled by distinct strings below
	}{
		// lastDigit == 1 → one (line 170)
		{value: 1, wantForm: "ONE"},
		{value: 21, wantForm: "ONE"},
		{value: 101, wantForm: "ONE"},

		// lastDigit in [2,4] → few (line 172)
		{value: 2, wantForm: "FEW"},
		{value: 3, wantForm: "FEW"},
		{value: 4, wantForm: "FEW"},
		{value: 22, wantForm: "FEW"},
		{value: 34, wantForm: "FEW"},

		// default → many
		{value: 5, wantForm: "MANY"},
		{value: 0, wantForm: "MANY"},
		{value: 20, wantForm: "MANY"},
	}

	for _, tc := range tests {
		got := russianPluralForm(tc.value, "ONE", "FEW", "MANY")
		if got != tc.wantForm {
			t.Errorf("russianPluralForm(%d): expected %q, got %q", tc.value, tc.wantForm, got)
		}
	}
}

// TestI18nPolicyFrenchCountSingular covers line 137 (`if count == 1` in the
// French branch). French does not change the word for "fois" between singular
// and plural — both are "fois" — so the branch is equivalent for count-word
// purposes. However the branch IS executed for count==1, and the test below
// verifies the full output is correct so that any mutation to the surrounding
// logic (e.g. flipping to lang=="de") is caught.
func TestI18nPolicyFrenchCountSingular(t *testing.T) {
	// count==1, days==1 → singular day form
	got := LocalizedSymptomFrequencySummary("fr", 1, 1)
	want := "1 fois (en 1 jour)"
	if got != want {
		t.Fatalf("French singular: expected %q, got %q", want, got)
	}

	// count==1, days==3 → plural day form
	got = LocalizedSymptomFrequencySummary("fr", 1, 3)
	want = "1 fois (en 3 jours)"
	if got != want {
		t.Fatalf("French singular count plural days: expected %q, got %q", want, got)
	}

	// count==5, days==1 → plural count, singular day
	got = LocalizedSymptomFrequencySummary("fr", 5, 1)
	want = "5 fois (en 1 jour)"
	if got != want {
		t.Fatalf("French plural count singular day: expected %q, got %q", want, got)
	}
}

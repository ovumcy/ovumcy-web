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

// TestI18nPolicyRussianPluralTeenBoundary verifies the teen exception (11–14),
// which the last TWO digits decide.
//
// The forms are distinct sentinels rather than the natural Russian words. That
// is not cosmetic: for «раз» the one form and the many form are the SAME word,
// so a teen assertion written with real copy cannot tell a teen routed to
// "many" from a teen that fell through to lastDigit == 1 and returned "one".
// The whole point of a teen case is that distinction, and with real copy every
// value below passes with the exception removed.
//
// 111 is here because the rule reads `n % 100`: it is the same teen two digits
// up, and it is the input a round-two mutation file contributed that this table
// did not carry. That file's other two values are covered — 11 here, 21 by the
// switch-case table below — and a round-three file restated rows from both, so
// both files were folded in here rather than kept in parallel.
func TestI18nPolicyRussianPluralTeenBoundary(t *testing.T) {
	// Teens, upper and lower edge included: `<= 14` → `<= 13` drops 14,
	// `>= 11` → `> 11` drops 11, and dropping `% 100` drops 111.
	for _, value := range []int{11, 12, 13, 14, 111} {
		if got := russianPluralForm(value, "ONE", "FEW", "MANY"); got != "MANY" {
			t.Fatalf("russianPluralForm(%d): expected MANY (teen exception), got %q", value, got)
		}
	}

	// 15 is NOT a teen → lastDigit == 5 → many by the default arm, so it must
	// not move when the teen range does.
	if got := russianPluralForm(15, "ONE", "FEW", "MANY"); got != "MANY" {
		t.Fatalf("russianPluralForm(15): expected MANY (default arm), got %q", got)
	}

	// 24 has lastDigit == 4 and is not a teen → few, which is where 14 would
	// land if the exception stopped covering it.
	if got := russianPluralForm(24, "ONE", "FEW", "MANY"); got != "FEW" {
		t.Fatalf("russianPluralForm(24): expected FEW (lastDigit 4, not a teen), got %q", got)
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

// TestI18nPolicyFrenchCountSingular pins the French summary output across the
// count/day combinations. French does not change the word for "fois" between
// singular and plural, so there is no count-word branch to cover here — the
// day word is the only form that varies, and the assertions below also catch a
// mutation to the surrounding language dispatch (e.g. flipping to lang=="de").
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

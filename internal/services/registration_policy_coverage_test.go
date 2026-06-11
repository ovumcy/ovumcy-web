package services

// registration_policy_coverage_test.go — covers ParseRegistrationMode and the
// RegistrationMode helpers, which had no direct test (gremlins reported the
// empty-input and Validate branches on lines 20/23 as NOT COVERED).

import "testing"

func TestRegistrationpolicyCovParseModeEmptyDefaultsToOpen(t *testing.T) {
	// Line 20–21: empty (or whitespace-only) input must default to "open".
	for _, raw := range []string{"", "   ", "\t\n"} {
		mode, err := ParseRegistrationMode(raw)
		if err != nil {
			t.Fatalf("ParseRegistrationMode(%q) unexpected error: %v", raw, err)
		}
		if mode != RegistrationModeOpen {
			t.Fatalf("ParseRegistrationMode(%q) = %q, want %q", raw, mode, RegistrationModeOpen)
		}
	}
}

func TestRegistrationpolicyCovParseModeNormalizesValidValues(t *testing.T) {
	// Line 23–26: a non-empty value passes through Validate and is lower-cased.
	cases := map[string]RegistrationMode{
		"open":    RegistrationModeOpen,
		"OPEN":    RegistrationModeOpen,
		"closed":  RegistrationModeClosed,
		" Closed": RegistrationModeClosed,
	}
	for raw, want := range cases {
		mode, err := ParseRegistrationMode(raw)
		if err != nil {
			t.Fatalf("ParseRegistrationMode(%q) unexpected error: %v", raw, err)
		}
		if mode != want {
			t.Fatalf("ParseRegistrationMode(%q) = %q, want %q", raw, mode, want)
		}
	}
}

func TestRegistrationpolicyCovParseModeRejectsUnknown(t *testing.T) {
	// Line 23–24: an unknown value reaches Validate and is rejected.
	if _, err := ParseRegistrationMode("banana"); err == nil {
		t.Fatal("ParseRegistrationMode(\"banana\") expected error, got nil")
	}
}

func TestRegistrationpolicyCovIsOpen(t *testing.T) {
	if !RegistrationModeOpen.IsOpen() {
		t.Fatal("RegistrationModeOpen.IsOpen() = false, want true")
	}
	if RegistrationModeClosed.IsOpen() {
		t.Fatal("RegistrationModeClosed.IsOpen() = true, want false")
	}
}

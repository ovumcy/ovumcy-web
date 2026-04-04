package services

import "testing"

func TestNormalizeInterfaceTheme(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "light", input: " light ", want: "light"},
		{name: "dark", input: "DARK", want: "dark"},
		{name: "invalid", input: "sepia", want: ""},
		{name: "empty", input: "   ", want: ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeInterfaceTheme(tc.input); got != tc.want {
				t.Fatalf("NormalizeInterfaceTheme(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveInterfaceUpdateStatus(t *testing.T) {
	service := NewSettingsService(nil)

	if got := service.ResolveInterfaceUpdateStatus(); got != "interface_updated" {
		t.Fatalf("expected interface_updated, got %q", got)
	}
}

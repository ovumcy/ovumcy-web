package services

import "testing"

func TestSanitizeRedirectPath_RootSlashKept(t *testing.T) {
	// "/" is the minimal candidate that passes the non-empty and
	// leading-slash checks with length exactly 1. The length>1 guard must
	// short-circuit before indexing candidate[1]; a boundary slip to >=1
	// would index out of range and panic instead of returning "/".
	if got := SanitizeRedirectPath("/", "/login"); got != "/" {
		t.Fatalf("SanitizeRedirectPath(%q) = %q, want %q", "/", got, "/")
	}
}

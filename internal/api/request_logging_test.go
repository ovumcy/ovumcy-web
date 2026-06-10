package api

import (
	"errors"
	"testing"
)

func TestSafeLogError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil error yields empty string", nil, ""},
		{"generic message is preserved", errors.New("internal server error"), "internal server error"},
		{"email is masked", errors.New("lookup failed for owner@example.com"), "lookup failed for :email"},
		{"opaque token is masked", errors.New("token AbCdEf0123456789AbCdEf012345 rejected"), "token :token rejected"},
		{"surrounding whitespace trimmed", errors.New("  boom  "), "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SafeLogError(tc.err); got != tc.want {
				t.Fatalf("SafeLogError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

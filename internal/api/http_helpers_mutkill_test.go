package api

import "testing"

// Mutation-kill test for localizedPageTitle (http_helpers.go L50). The function
// returns the localized title, falling back to `fallback` when the translation
// is missing (`title == key`) OR blank (`strings.TrimSpace(title) == ""`). The
// line carries two comparison operators; negating either (CONDITIONALS_NEGATION)
// inverts which inputs fall back. Pure function, so it is table-tested directly.
//
// The two cases below kill both operators:
//   - "present translation" kills `title == key`->`!=` (which would fall back on
//     a real translation) AND `TrimSpace(title) == ""`->`!=` (which forces an
//     unconditional fallback, since the second operand is otherwise always false
//     because translateMessage never returns a blank string).
//   - "missing translation" kills `title == key`->`!=` from the other side
//     (which would return the bare key instead of the fallback).
func TestLocalizedPageTitleMutKill(t *testing.T) {
	const key = "meta.title.stats"
	const fallback = "Ovumcy | Stats"

	cases := []struct {
		name     string
		messages map[string]string
		want     string
	}{
		{
			name:     "present translation is used",
			messages: map[string]string{key: "Localized Stats Title"},
			want:     "Localized Stats Title",
		},
		{
			name:     "missing translation falls back",
			messages: map[string]string{},
			want:     fallback,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := localizedPageTitle(testCase.messages, key, fallback); got != testCase.want {
				t.Fatalf("localizedPageTitle = %q, want %q", got, testCase.want)
			}
		})
	}
}

package api

import "testing"

// Mutation-kill test for localizedPageTitle (http_helpers.go). The function
// asks lookupMessage for the catalogue entry and returns `fallback` when the
// catalogue did not have one: `title, translated := lookupMessage(...)` then
// `if !translated`. One comparison, and negating it (CONDITIONALS_NEGATION)
// swaps which inputs fall back. Pure function, so it is table-tested directly.
//
// It used to re-derive the miss itself, as `title == key ||
// strings.TrimSpace(title) == ""` over translateMessage's answer, and the
// comment here described that line long after the lookupMessage sweep replaced
// it. The second operand was unreachable in that form — translateMessage
// returns a catalogue value only when its TrimSpace is non-blank, so a blank
// title was always the key echoed back and the first operand had already
// decided — which is why the blank case below now goes through the catalogue
// instead of through the title.
//
// The cases kill the operator from both sides and hold the miss's own
// definition:
//   - "present translation" negates `!translated` into an unconditional
//     fallback.
//   - "missing translation" negates it the other way, returning the bare key
//     rather than the fallback.
//   - "blank translation" and "whitespace translation" pin what counts as a
//     miss: a catalogue that carries the key with unrenderable text is as
//     missing as one that lacks it. Gutting lookupMessage's TrimSpace guard
//     turns both green-into-red.
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
		{
			name:     "blank translation falls back",
			messages: map[string]string{key: ""},
			want:     fallback,
		},
		{
			name:     "whitespace translation falls back",
			messages: map[string]string{key: "   "},
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

package services

import "testing"

type stubBuiltinSymptomMessages struct {
	supported []string
	messages  map[string]map[string]string
}

func (stub stubBuiltinSymptomMessages) SupportedLanguages() []string {
	return append([]string(nil), stub.supported...)
}

func (stub stubBuiltinSymptomMessages) Messages(language string) map[string]string {
	return stub.messages[language]
}

func TestBuiltinSymptomReservedNamesIncludesLocalizedLabels(t *testing.T) {
	provider := stubBuiltinSymptomMessages{
		supported: []string{"en", "ru", "de", "fr"},
		messages: map[string]map[string]string{
			"ru": {
				"symptoms.fatigue": "Усталость",
			},
			"de": {
				"symptoms.fatigue": "Muedigkeit",
			},
		},
	}

	names := BuiltinSymptomReservedNames(provider)

	assertContainsReservedName(t, names, "Fatigue")
	assertContainsReservedName(t, names, "Усталость")
	assertContainsReservedName(t, names, "Muedigkeit")
}

func assertContainsReservedName(t *testing.T, names []string, expected string) {
	t.Helper()

	expectedKey := normalizeSymptomNameKey(expected)
	for _, name := range names {
		if normalizeSymptomNameKey(name) == expectedKey {
			return
		}
	}

	t.Fatalf("expected reserved names to contain %q, got %#v", expected, names)
}

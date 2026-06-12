package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestLocaleKeysParity(t *testing.T) {
	locales := mustLoadAllLocaleMessages(t)
	reference, ok := locales[LangEN]
	if !ok {
		t.Fatalf("reference locale %q is missing", LangEN)
	}

	languages := make([]string, 0, len(locales))
	for language := range locales {
		languages = append(languages, language)
	}
	sort.Strings(languages)

	pluralBases := pluralBaseKeys(reference)

	for _, language := range languages {
		if language == LangEN {
			continue
		}

		expected := expectedKeysForLanguage(language, reference, pluralBases)
		missing := missingKeys(expected, locales[language])
		extra := missingKeys(locales[language], expected)
		if len(missing) == 0 && len(extra) == 0 {
			continue
		}
		if len(missing) > 0 {
			t.Errorf("keys missing in %s locale: %s", language, strings.Join(missing, ", "))
		}
		if len(extra) > 0 {
			t.Errorf("unexpected keys in %s locale: %s", language, strings.Join(extra, ", "))
		}
	}
}

// pluralBaseKeys finds the base keys of plural groups in the reference
// locale. A base key is a plural group only when the reference defines both
// its ".one" and ".other" variants — so keys that merely end in a category
// word (for example "symptoms.group.other") are not misread as plurals.
func pluralBaseKeys(reference map[string]string) map[string]bool {
	bases := map[string]bool{}
	for key := range reference {
		base, found := strings.CutSuffix(key, ".one")
		if !found {
			continue
		}
		if _, ok := reference[base+".other"]; ok {
			bases[base] = true
		}
	}
	return bases
}

// expectedKeysForLanguage projects the English reference key set onto a
// target language: plain keys carry over verbatim, while plural groups must
// provide exactly the CLDR categories that language can select (for example
// Russian needs one/few/many and must not carry a dead ".other").
func expectedKeysForLanguage(language string, reference map[string]string, pluralBases map[string]bool) map[string]string {
	expected := make(map[string]string, len(reference))
	for key := range reference {
		base := pluralVariantBase(key, pluralBases)
		if base == "" {
			expected[key] = ""
			continue
		}
		for _, category := range PluralCategories(language) {
			expected[base+"."+category] = ""
		}
	}
	return expected
}

func pluralVariantBase(key string, pluralBases map[string]bool) string {
	for _, category := range []string{"one", "few", "many", "other"} {
		if base, found := strings.CutSuffix(key, "."+category); found && pluralBases[base] {
			return base
		}
	}
	return ""
}

func mustLoadAllLocaleMessages(t *testing.T) map[string]map[string]string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("resolve test file path: runtime.Caller failed")
	}
	localesDir := filepath.Join(filepath.Dir(thisFile), "locales")
	entries, err := os.ReadDir(localesDir)
	if err != nil {
		t.Fatalf("read locales dir: %v", err)
	}

	locales := make(map[string]map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		language := strings.TrimSuffix(strings.ToLower(entry.Name()), filepath.Ext(entry.Name()))
		localePath := filepath.Join(localesDir, entry.Name())

		content, err := os.ReadFile(localePath)
		if err != nil {
			t.Fatalf("read locale %q: %v", language, err)
		}

		messages := map[string]string{}
		if err := json.Unmarshal(content, &messages); err != nil {
			t.Fatalf("parse locale %q: %v", language, err)
		}
		if len(messages) == 0 {
			t.Fatalf("locale %q is empty", language)
		}
		locales[language] = messages
	}

	if len(locales) == 0 {
		t.Fatal("expected at least one locale")
	}

	return locales
}

func missingKeys(source map[string]string, target map[string]string) []string {
	missing := make([]string, 0)
	for key := range source {
		if _, ok := target[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

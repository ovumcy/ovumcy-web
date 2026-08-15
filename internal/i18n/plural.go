package i18n

import "strings"

// Plural support for count-bearing UI strings (audit task #25). Locale files
// store one variant per CLDR plural category under suffixed keys
// ("key.one", "key.few", ...); PluralCategory selects the category for an
// integer count and TranslatePlural resolves the matching pattern. Only the
// categories integers can actually hit in the supported languages are
// modelled — fractional-only categories (e.g. Russian "other") are omitted
// on purpose, since every count in the product is a whole number of days or
// cycles.

// PluralCategory returns the CLDR plural category for integer count n in
// the given (already normalized) language. Unknown languages fall back to
// the English one/other rule.
func PluralCategory(language string, n int) string {
	if n < 0 {
		n = -n
	}

	switch strings.ToLower(strings.TrimSpace(language)) {
	case LangRU:
		lastTwo := n % 100
		if lastTwo >= 11 && lastTwo <= 14 {
			return "many"
		}
		switch last := n % 10; {
		case last == 1:
			return "one"
		case last >= 2 && last <= 4:
			return "few"
		default:
			return "many"
		}
	case LangFR:
		// CLDR French: 0 and 1 are singular.
		if n == 0 || n == 1 {
			return "one"
		}
		return "other"
	default:
		if n == 1 {
			return "one"
		}
		return "other"
	}
}

// PluralCategories lists every category PluralCategory can return for the
// given language, in CLDR order. It is the single source of truth for the set
// of suffixed keys a plural family owes per language — the locale parity test
// demands exactly those variants (no missing Russian "few", no dead English
// "many"), the printf-verb test compares them against each other, and the
// late-cycle locale test in internal/services enumerates them for its own key
// families. Callers in two packages are why this stays an exported
// declaration in production code rather than moving into a _test.go: a
// _test.go declaration is invisible outside its own package, and duplicating
// the table would let the copies disagree about which variants a locale file
// must carry. It is unreachable from the running binary on purpose; any check
// that has to enumerate a plural key family should compute it from here.
//
// Decision (2026-08-15): keep as production code, documented, rather than
// resolve the production-scope dead-code finding by duplicating the table.
func PluralCategories(language string) []string {
	if strings.ToLower(strings.TrimSpace(language)) == LangRU {
		return []string{"one", "few", "many"}
	}
	return []string{"one", "other"}
}

// TranslatePlural resolves the plural variant of key for count n from a
// merged Messages map. Lookup order: the language's exact category, then
// ".other" (covers the default-language overlay for locales whose category
// set differs), then the bare key so a not-yet-pluralized string keeps
// rendering during transitions. Like Translate, it returns the key itself
// when nothing matches.
func TranslatePlural(messages map[string]string, language string, key string, n int) string {
	for _, candidate := range []string{
		key + "." + PluralCategory(language, n),
		key + ".other",
		key,
	} {
		if value, ok := messages[candidate]; ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return key
}

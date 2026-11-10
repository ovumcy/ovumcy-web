package api

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestCryptographyCookieTableSealedColumnMatchesTheDeclaredRoster holds the
// published cookie table to the code: the rows marked Sealed are exactly the
// purposes the package seals under, so a cookie sealed later without a row, a
// row left behind by a removed cookie, or a Sealed cell that disagrees with
// the code fails here.
func TestCryptographyCookieTableSealedColumnMatchesTheDeclaredRoster(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "security", "cryptography.md"))
	if err != nil {
		t.Fatalf("read cryptography.md: %v", err)
	}

	rows := cookieTableRows(t, string(data))
	for name, want := range map[string]string{authCookieName: "yes", "ovumcy_csrf": "no"} {
		if got, ok := rows[name]; !ok || got != want {
			t.Fatalf("cookie table row %q: Sealed=%q present=%v, want %q — the parser is reading the wrong table", name, got, ok, want)
		}
	}

	var documented []string
	for name, sealed := range rows {
		switch sealed {
		case "yes":
			documented = append(documented, name)
		case "no":
		default:
			t.Fatalf("cookie table row %q has Sealed cell %q, want yes or no", name, sealed)
		}
	}
	slices.Sort(documented)

	declared := slices.Clone(declaredSealedCookiePurposes(t))
	slices.Sort(declared)

	if !slices.Equal(documented, declared) {
		t.Fatalf("cryptography.md lists sealed cookies %v, the package seals under %v", documented, declared)
	}
	for name, sealed := range rows {
		if sealed == "no" && slices.Contains(declared, name) {
			t.Fatalf("cryptography.md marks %q unsealed, but the package seals it", name)
		}
	}
}

// cookieTableRows maps each cookie name in the `## Cookies` table to its
// Sealed cell (the row's last column).
func cookieTableRows(t *testing.T, doc string) map[string]string {
	t.Helper()

	_, section, found := strings.Cut(doc, "\n## Cookies\n")
	if !found {
		t.Fatal("cryptography.md has no `## Cookies` section")
	}
	if end := strings.Index(section, "\n## "); end >= 0 {
		section = section[:end]
	}

	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		name := strings.Trim(strings.TrimSpace(cells[0]), "`")
		if _, dup := rows[name]; dup {
			t.Fatalf("cookie table lists %q twice", name)
		}
		rows[name] = strings.TrimSpace(cells[len(cells)-1])
	}
	return rows
}

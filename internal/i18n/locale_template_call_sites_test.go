package i18n_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"text/template/parse"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// The reverse of locale_reachability_test.go. That barrier asks whether the
// application still names each catalogue key; this one asks whether the
// catalogue still answers each key a shipped template renders. Both directions
// are needed, and neither implies the other: `settings.2fa.status_enabled_hint`
// was rendered by internal/templates/settings_2fa.html and defined by no
// catalogue at all, so it was invisible to key parity (nothing to compare
// across six files) and invisible to reachability (no catalogue key to call
// dead). Every account with TOTP enabled read the raw identifier under a
// correctly translated heading, in all six languages.
//
// The barrier is CALL-SITE scoped rather than literal scoped. A literal sweep
// answers "is this string a key somewhere", which is exactly the question that
// cannot fail for a key that exists nowhere. Walking the parsed templates for
// commands whose function is `t` or `tn` and reading the argument in the key
// position is what makes an undefined key a finding.

// keyArgumentPositions names, per template func, the index in a command's
// argument list that carries the catalogue key. Index 0 is the function
// identifier itself, so these are the 1-based positions after it:
// `{{t .Messages "key"}}` and `{{tn .Messages .Language "key" .N}}`.
//
// The two entries mirror newTemplateFuncMap in
// internal/api/handlers_template_helpers.go. `tn` is listed even though no
// shipped template calls it today: a barrier that only understood the func
// currently in use would go quiet on the first template that reaches for the
// other one.
var keyArgumentPositions = map[string]int{"t": 2, "tn": 3}

// templateKeyCallSite is one `t`/`tn` command found in a shipped template.
//
// keyIsLiteral separates the two cases deliberately. A key argument that is a
// pipeline, a `printf`, or a field is built at runtime and this barrier does
// not guess at it — the localeKeyFamilies registry in
// locale_reachability_test.go governs runtime-built keys from the other
// direction. A non-literal site still counts as a site, so it feeds the
// "did the sweep see anything at all" refusal below.
type templateKeyCallSite struct {
	file         string
	line         int
	function     string
	key          string
	keyIsLiteral bool
}

func (site templateKeyCallSite) String() string {
	return fmt.Sprintf("%s:%d {{%s ... %q}}", site.file, site.line, site.function, site.key)
}

// recordTemplateKeyCallSite is called for every command node the template
// collector walks.
func recordTemplateKeyCallSite(command *parse.CommandNode, origin templateOrigin, evidence *sourceEvidence) {
	if command == nil || len(command.Args) == 0 {
		return
	}
	identifier, ok := command.Args[0].(*parse.IdentifierNode)
	if !ok {
		return
	}
	position, ok := keyArgumentPositions[identifier.Ident]
	if !ok {
		return
	}

	site := templateKeyCallSite{
		file:     origin.file,
		line:     origin.lineOf(command.Position()),
		function: identifier.Ident,
	}
	if position < len(command.Args) {
		if literal, ok := command.Args[position].(*parse.StringNode); ok {
			site.key = literal.Text
			site.keyIsLiteral = true
		}
	}
	evidence.templateKeyCallSites = append(evidence.templateKeyCallSites, site)
}

// TestEveryTemplateTranslationKeyResolvesInTheCatalogue is the barrier.
func TestEveryTemplateTranslationKeyResolvesInTheCatalogue(t *testing.T) {
	evidence := newSourceEvidence()
	if err := collectFromShippedTemplates(evidence); err != nil {
		t.Fatalf("sweeping the shipped templates: %v", err)
	}

	// A guard fails by silence. Measured 2026-08-24 on the tree this barrier
	// ships with: 38 embedded templates carrying 787 `t`/`tn` call sites, 753
	// of which pass a plain string literal (the remaining 34 build their key
	// from a pipeline and are out of this barrier's reach by design). The
	// floors carry slack; what they refuse is a sweep that walked the tree,
	// recognised nothing, and reported success about nothing.
	if evidence.templateFiles < 30 {
		t.Fatalf("parsed only %d templates; the embedded set is far larger, so this sweep missed a directory", evidence.templateFiles)
	}
	if len(evidence.templateKeyCallSites) < 500 {
		t.Fatalf("found only %d t/tn call site(s) in the shipped templates; a sweep that walks the tree but recognises no call site passes this barrier about nothing", len(evidence.templateKeyCallSites))
	}

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("building the locale manager: %v", err)
	}
	english := manager.Messages(i18n.LangEN)

	literalSites, unresolved := unresolvedTemplateKeys(evidence.templateKeyCallSites, english)
	if literalSites < 500 {
		t.Fatalf("only %d call site(s) passed a plain string literal; the key-position reader stopped seeing literals, so the barrier is checking almost nothing", literalSites)
	}
	if len(unresolved) == 0 {
		return
	}

	var report strings.Builder
	for _, site := range unresolved {
		fmt.Fprintf(&report, "  %s\n", site)
	}
	t.Fatalf("%d translation key(s) a shipped template renders that the English catalogue does not answer:\n%s\n"+
		"translateMessage renders a miss as the key itself, so each of these puts a raw identifier on the page in every "+
		"language. A blank catalogue value counts as a miss here because lookupMessage counts it as one. Add the key to "+
		"all six catalogues; do not silence this by removing the call site unless the surface itself is going away.",
		len(unresolved), report.String())
}

// unresolvedTemplateKeys reports how many call sites carried a plain literal
// and which of those the catalogue cannot answer.
//
// Taking the catalogue as an argument rather than reading it is what lets the
// self-tests below drive this function with a fixture, so the barrier's own
// logic is measured instead of assumed.
func unresolvedTemplateKeys(sites []templateKeyCallSite, catalogue map[string]string) (int, []templateKeyCallSite) {
	bases := pluralBasesOf(catalogue)

	literalSites := 0
	var unresolved []templateKeyCallSite
	for _, site := range sites {
		if !site.keyIsLiteral {
			continue
		}
		literalSites++
		if strings.TrimSpace(catalogue[site.key]) != "" {
			continue
		}
		// A plural family is carried by its category variants; the bare key
		// need not exist. TranslatePlural resolves key+"."+category first, so
		// a base whose variants are all present is answerable even though the
		// base itself is absent.
		if bases[site.key] {
			continue
		}
		unresolved = append(unresolved, site)
	}

	sort.Slice(unresolved, func(a, b int) bool {
		if unresolved[a].file != unresolved[b].file {
			return unresolved[a].file < unresolved[b].file
		}
		return unresolved[a].line < unresolved[b].line
	})
	return literalSites, unresolved
}

// pluralBasesOf finds the keys the catalogue answers only as a plural family.
// The categories come from i18n.PluralCategories for the reference locale, the
// same declaration the parity test projects each language's key set from.
func pluralBasesOf(catalogue map[string]string) map[string]bool {
	bases := map[string]bool{}
	for key := range catalogue {
		base, found := strings.CutSuffix(key, ".one")
		if !found {
			continue
		}
		complete := true
		for _, category := range i18n.PluralCategories(i18n.LangEN) {
			if strings.TrimSpace(catalogue[base+"."+category]) == "" {
				complete = false
				break
			}
		}
		if complete {
			bases[base] = true
		}
	}
	return bases
}

// The barrier is only as good as the reader that finds the key position, so
// the reader is measured on a synthesised template rather than on the
// repository's own — a fixture that measures ovumcy-web against itself only
// proves ovumcy-web agrees with itself.
func TestTemplateKeyCallSiteReaderFindsTheKeyPosition(t *testing.T) {
	const fixture = `{{define "content"}}
<h1>{{t .Messages "probe.present"}}</h1>
<p>{{t .Messages "probe.absent"}}</p>
<p>{{t .Messages "probe.blank"}}</p>
<p>{{tn .Messages .Lang "probe.counted" .N}}</p>
<p>{{t .Messages (printf "probe.built.%s" .Kind)}}</p>
<p>{{if .Flag}}{{t .Messages "probe.in.branch"}}{{else}}{{t .Messages "probe.in.else"}}{{end}}</p>
<p>{{notATranslator .Messages "probe.other.func"}}</p>
{{end}}`

	evidence := newSourceEvidence()
	if err := collectFromTemplateSource("fixture.html", fixture, evidence); err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}

	found := map[string]templateKeyCallSite{}
	nonLiteral := 0
	for _, site := range evidence.templateKeyCallSites {
		if site.keyIsLiteral {
			found[site.key] = site
			continue
		}
		nonLiteral++
	}

	for _, want := range []string{"probe.present", "probe.absent", "probe.blank", "probe.counted", "probe.in.branch", "probe.in.else"} {
		if _, ok := found[want]; !ok {
			t.Errorf("the reader missed the %q call site; a call site it cannot see is a key it can never report", want)
		}
	}
	if _, ok := found["probe.other.func"]; ok {
		t.Errorf("a command whose function is not t/tn was read as a translation call site; the barrier would demand catalogue keys for unrelated helpers")
	}
	if nonLiteral != 1 {
		t.Errorf("read %d non-literal key argument(s); the printf-built key must be recorded as a site and skipped, never guessed at", nonLiteral)
	}
	if site := found["probe.counted"]; site.function != "tn" {
		t.Errorf("the tn call site was read as function %q; tn's key sits one argument later than t's, so a wrong reading takes the language code for the key", site.function)
	}
	if site := found["probe.present"]; site.file != "fixture.html" || site.line != 2 {
		t.Errorf("the call site reported %s:%d; a finding that cannot be opened is a finding nobody acts on", site.file, site.line)
	}

	// The verdict half: the same call sites against a catalogue that answers
	// only some of them.
	catalogue := map[string]string{
		"probe.present":       "Present",
		"probe.blank":         "   ",
		"probe.counted.one":   "1 day",
		"probe.counted.other": "%d days",
		"probe.in.branch":     "Branch",
		"probe.in.else":       "Else",
	}
	literalSites, unresolved := unresolvedTemplateKeys(evidence.templateKeyCallSites, catalogue)
	if literalSites != 6 {
		t.Errorf("counted %d literal call site(s); the fixture ships six", literalSites)
	}

	var names []string
	for _, site := range unresolved {
		names = append(names, site.key)
	}
	sort.Strings(names)
	want := []string{"probe.absent", "probe.blank"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("the verdict named %v, want %v; a missing key must fail, a blank value must fail because lookupMessage counts it as a miss, and a plural base carried by its categories must pass", names, want)
	}
}

// A barrier that reports success on an empty sweep is the failure this class of
// test cannot see in itself, so the floor is measured in both directions.
func TestTemplateKeyBarrierRefusesASweepThatSawNoCallSite(t *testing.T) {
	evidence := newSourceEvidence()
	if err := collectFromTemplateSource("fixture.html", `<p>{{.Something}}</p>`, evidence); err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}
	if len(evidence.templateKeyCallSites) != 0 {
		t.Fatalf("a template with no translation call reported %d site(s); the fixture proves nothing", len(evidence.templateKeyCallSites))
	}
	literalSites, unresolved := unresolvedTemplateKeys(evidence.templateKeyCallSites, map[string]string{"probe.present": "Present"})
	if literalSites != 0 || len(unresolved) != 0 {
		t.Fatalf("an empty sweep produced %d literal site(s) and %d finding(s); it must produce neither, which is precisely why the barrier refuses a sweep this small instead of reading its verdict", literalSites, len(unresolved))
	}
}

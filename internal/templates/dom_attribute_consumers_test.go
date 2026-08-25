package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The DOM contract runs one way only, and nothing checked the other end of it.
// A template declares a `data-*` hook so that something outside the template —
// a Go regression, a Playwright spec, a browser script, a component rule in the
// stylesheet — can address that element. A hook no such consumer names is not a
// contract: it is markup that reads like one, survives every refactor because
// it looks load-bearing, and quietly promises coverage that does not exist.
//
// This file is that missing direction: every `data-*` attribute rendered from
// internal/templates must be named at least once outside internal/templates.
// It deliberately does NOT walk the opposite direction — a selector in a script
// or a spec pointing at markup that no longer renders — which is its own check.
//
// Reading the guard's own blind spots as defects is the failure mode here, so
// the consumer side is deliberately generous and each gate below exists because
// a narrower reading reported a live hook as dead:
//
//  1. Literal attribute names, tokenized rather than substring-matched. A Go
//     assertion writes `htmlHasAttr(node, "data-foo")`, a spec writes
//     `[data-foo="bar"]`, a stylesheet writes `[data-foo]` — all three yield the
//     token `data-foo`. Substring matching instead reports `data-foo` as live
//     the moment anything anywhere mentions `data-foo-bar`, which is how three
//     hooks here (`data-auto-period-fill`, `data-onboarding-usage-goal`,
//     `data-pwa-install-title`) looked consumed while the only names in the
//     tree were their longer siblings.
//  2. The camel-cased dataset form, for scripts that never spell the attribute:
//     `data-theme-label-dark` is only ever read as `dataset.themeLabelDark`, and
//     the recovery panel passes its message keys as bare `"copySuccessMessage"`
//     strings. Both spellings count.
//  3. Stylesheets count as consumers, sources and built bundles alike: a hook
//     whose only reader is a component rule is live.
//  4. The built JS bundle under web/static counts alongside its sources, so a
//     hook read only by generated code is not reported.
//  5. Template comments are stripped before hooks are collected, so a hook named
//     only in prose is not counted as rendered.
//  6. Hidden directories and dependency/report trees are skipped, so a stray
//     checkout under one of them cannot supply a phantom consumer.
//
// What it still cannot see: a selector assembled at runtime
// (`"[data-" + name + "]"`). That is the one shape a residual entry below may
// legitimately carry as its reason.
//
// The residual map is the ratchet. It is not an allowlist that may sit still:
// every entry is re-checked, and an entry whose hook has since gained a consumer
// — or disappeared from the templates — fails just as loudly as a new dead hook,
// so the list can only shrink.

// domAttrConsumerResidual holds the `data-*` hooks that render today with no
// consumer outside internal/templates, each with the reason it is still here.
// Closing one is a per-hook judgement — delete the markup, add the assertion, or
// wire the reader that was supposed to exist — not a sweep, so they are recorded
// rather than removed wholesale.
var domAttrConsumerResidual = map[string]string{
	// State mirrored into an attribute that nothing reads back. The value is
	// already visible in the markup beside it, so the mirror is either an
	// assertion someone meant to write or markup to delete.
	"data-auto-period-fill":   "onboarding mirrors the auto-fill toggle; the spec drives the checkbox itself",
	"data-calendar-has-sex":   "day-cell flag with no grid assertion and no component rule",
	"data-cycle-factor-count": "the count is rendered as chip text beside it; no reader",
	"data-symptom-active":     "picker state mirrored from the same field the option already renders",
	"data-symptom-label":      "preview-only mirror behind IncludePreviewHooks; no preview reader exists",
	"data-temperature-unit":   "the unit is rendered in the field label; no reader",

	// Container and landmark hooks: addressable by design, addressed by nobody.
	"data-cycle-stack-rows":             "stats cycle-stack list container",
	"data-dashboard-cycle-start-form":   "manual cycle-start form container",
	"data-dashboard-quick-actions":      "quick-action row container; the buttons carry their own data-quick-action",
	"data-dashboard-shell":              "dashboard page shell",
	"data-export-presets":               "export preset row container",
	"data-onboarding-usage-goal":        "usage-goal step container; the spec addresses the choices",
	"data-settings-reminders-form":      "reminder settings form container",
	"data-stats-factor-patterns":        "stats factor-pattern grid container",
	"data-usage-goal-quick-switch-form": "quick-switch form container; the spec addresses data-usage-goal-choice",

	// Control hooks nothing drives.
	"data-day-editor-cancel":           "calendar day-editor cancel button",
	"data-dashboard-more-toggle":       "journal More disclosure summary; the details element carries data-dashboard-more",
	"data-saving-label":                "the in-flight button caption, declared at nine sites and read at none",
	"data-settings-cycle-submit":       "cycle settings submit button",
	"data-settings-reminder-lead-days": "reminder lead-days field",
	"data-settings-reminders-save":     "reminder settings save button",

	// Copy surfaces carrying a hook with no spec behind it.
	"data-calendar-feed-hint":       "calendar-feed URL hint",
	"data-calendar-month-label":     "calendar month label",
	"data-import-status":            "import status container; the id is what the request targets",
	"data-pwa-install-title":        "superseded by the data-pwa-install-title-key sibling the spec asserts",
	"data-stats-perimenopause-hint": "perimenopause hint card",
	"data-symptom-card-title":       "custom-symptom card title",

	// These two are not decoration, and closing them is a change of its own.
	"data-cycle-start-implantation": "the only unread field of the cycle-start confirm island: its siblings (conflict-date, short-gap, replace-message) are all read, so the implantation warning the server computes reaches no dialog",
	"data-next-period-paused-key":   "the state key beside data-dashboard-next-period-paused, which is asserted while its key is not",
	"data-projected":                "the only ribbon-day flag with no reader; data-today, data-fertile and the rest are all addressed",
}

// domAttrConsumerSourceExtensions are the file kinds that can name a hook:
// Go (backend regressions), the browser sources and their built bundle,
// TypeScript (Playwright specs and helpers) and CSS (component rules).
var domAttrConsumerSourceExtensions = map[string]bool{
	".go":  true,
	".js":  true,
	".mjs": true,
	".ts":  true,
	".tsx": true,
	".css": true,
}

// domAttrConsumerScriptExtensions are the subset whose camel-cased dataset
// spelling of a hook counts as naming it.
var domAttrConsumerScriptExtensions = map[string]bool{
	".js":  true,
	".mjs": true,
	".ts":  true,
	".tsx": true,
}

// domAttrConsumerSkipDirectories are the trees that hold no source of this
// repository: dependency installs and generated report output. Hidden
// directories are skipped separately, by name prefix.
var domAttrConsumerSkipDirectories = map[string]bool{
	"node_modules":      true,
	"coverage":          true,
	"test-results":      true,
	"playwright-report": true,
}

var (
	domAttrConsumerAttrPattern    = regexp.MustCompile(`\bdata-[a-z0-9-]*[a-z0-9]`)
	domAttrConsumerCommentPattern = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}|<!--.*?-->`)
	domAttrConsumerDatasetPattern = regexp.MustCompile(`dataset\.([A-Za-z0-9_$]+)|["']([A-Za-z][A-Za-z0-9]*)["']`)
)

// TestEveryTemplateDataHookIsNamedOutsideTheTemplates is the DOM contract's
// missing half: a rendered `data-*` hook that no Go regression, browser script,
// Playwright spec or stylesheet names addresses nothing.
func TestEveryTemplateDataHookIsNamedOutsideTheTemplates(t *testing.T) {
	root := domAttrConsumerRepoRoot(t)
	hooks := domAttrConsumerTemplateHooks(t, filepath.Join(root, "internal", "templates"), root)
	literals, datasets, files := domAttrConsumerConsumerIndex(t, root)

	// Corpus anchors: a mis-resolved root would otherwise report an empty tree
	// as a clean one. The bounds are floors, not counts, so ordinary churn does
	// not touch them.
	if len(hooks) < 100 {
		t.Fatalf("expected the templates to declare at least 100 distinct data-* hooks, found %d — the template scan resolved nothing", len(hooks))
	}
	if files < 100 {
		t.Fatalf("expected at least 100 consumer sources outside internal/templates, found %d — the consumer scan resolved nothing", files)
	}

	var unreported []string
	for hook, sites := range hooks {
		if domAttrConsumerNamed(hook, literals, datasets) {
			continue
		}
		if _, recorded := domAttrConsumerResidual[hook]; recorded {
			continue
		}
		unreported = append(unreported, hook+" — rendered at "+strings.Join(sites, ", "))
	}
	if len(unreported) > 0 {
		sort.Strings(unreported)
		t.Fatalf("%d data-* hook(s) render with no consumer outside internal/templates.\n"+
			"Give each one a reader (Go regression, browser script, Playwright spec or component rule) or delete the markup:\n\t%s",
			len(unreported), strings.Join(unreported, "\n\t"))
	}

	var stale []string
	for hook, reason := range domAttrConsumerResidual {
		sites, rendered := hooks[hook]
		switch {
		case !rendered:
			stale = append(stale, hook+" is no longer rendered by any template — drop the residual entry ("+reason+")")
		case domAttrConsumerNamed(hook, literals, datasets):
			stale = append(stale, hook+" (rendered at "+strings.Join(sites, ", ")+") now has a consumer — drop the residual entry ("+reason+")")
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("%d residual entr(ies) are stale; the list may only shrink:\n\t%s", len(stale), strings.Join(stale, "\n\t"))
	}
}

// TestDomAttrConsumerScanClassifiesItsOwnFixtures anchors the guard above on
// inputs this file owns: one hook that must classify as named and one that must
// classify as unnamed, plus the two spellings and the comment strip that the
// gates depend on. Anchoring on the live tree instead would leave the guard
// reporting success the day its scan silently stopped resolving anything.
func TestDomAttrConsumerScanClassifiesItsOwnFixtures(t *testing.T) {
	template := `<div data-fixture-live-hook data-fixture-dataset-hook="x">
  {{/* data-fixture-commented-hook is named only in prose */}}
  <span data-fixture-dead-hook></span>
</div>`
	hooks := domAttrConsumerHooksInTemplate(template, "fixture.html")
	for _, expected := range []string{"data-fixture-live-hook", "data-fixture-dataset-hook", "data-fixture-dead-hook"} {
		if _, found := hooks[expected]; !found {
			t.Fatalf("fixture template must yield the hook %q, got %v", expected, hooks)
		}
	}
	if _, found := hooks["data-fixture-commented-hook"]; found {
		t.Fatal("a hook named only inside a template comment must not count as rendered")
	}
	if got := hooks["data-fixture-live-hook"]; len(got) != 1 || got[0] != "fixture.html:1" {
		t.Fatalf("expected the hook site to carry file and line, got %v", got)
	}

	literals := map[string]bool{}
	datasets := map[string]bool{}
	domAttrConsumerIndexSource(`htmlHasAttr(node, "data-fixture-live-hook-suffix")`, ".go", literals, datasets)
	domAttrConsumerIndexSource(`const label = root.dataset.fixtureDatasetHook;`, ".js", literals, datasets)

	if !domAttrConsumerNamed("data-fixture-dataset-hook", literals, datasets) {
		t.Fatal("a hook read through its camel-cased dataset name must classify as named")
	}
	if domAttrConsumerNamed("data-fixture-live-hook", literals, datasets) {
		t.Fatal("a longer attribute that merely starts with the hook must not classify it as named")
	}
	if domAttrConsumerNamed("data-fixture-dead-hook", literals, datasets) {
		t.Fatal("a hook no source names must classify as unnamed")
	}
	domAttrConsumerIndexSource(`page.locator('[data-fixture-live-hook="on"]')`, ".ts", literals, datasets)
	if !domAttrConsumerNamed("data-fixture-live-hook", literals, datasets) {
		t.Fatal("a hook named by a spec selector must classify as named")
	}

	if got := domAttrConsumerDatasetName("data-fixture-dataset-hook"); got != "fixtureDatasetHook" {
		t.Fatalf("dataset spelling of data-fixture-dataset-hook must be fixtureDatasetHook, got %q", got)
	}
}

// domAttrConsumerNamed reports whether either spelling of the hook appears in
// the consumer index.
func domAttrConsumerNamed(hook string, literals, datasets map[string]bool) bool {
	return literals[hook] || datasets[domAttrConsumerDatasetName(hook)]
}

// domAttrConsumerDatasetName converts data-theme-label-dark to themeLabelDark,
// the name a script reads it under.
func domAttrConsumerDatasetName(hook string) string {
	segments := strings.Split(strings.TrimPrefix(hook, "data-"), "-")
	name := segments[0]
	for _, segment := range segments[1:] {
		if segment == "" {
			continue
		}
		name += strings.ToUpper(segment[:1]) + segment[1:]
	}
	return name
}

// domAttrConsumerRepoRoot resolves the repository root from this file's own
// location, so the scan does not depend on the working directory.
func domAttrConsumerRepoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve this test file's path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolved repository root %s carries no go.mod: %v", root, err)
	}
	return root
}

// domAttrConsumerTemplateHooks collects every data-* hook rendered under the
// templates directory, mapped to the file:line sites that render it.
func domAttrConsumerTemplateHooks(t *testing.T, templateDir, root string) map[string][]string {
	t.Helper()

	hooks := map[string][]string{}
	walkErr := filepath.WalkDir(templateDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".html" {
			return nil
		}
		content, readErr := os.ReadFile(path) //nolint:gosec // walked path under the repository's own template tree
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		for hook, sites := range domAttrConsumerHooksInTemplate(string(content), filepath.ToSlash(relative)) {
			hooks[hook] = append(hooks[hook], sites...)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", templateDir, walkErr)
	}
	for hook := range hooks {
		sort.Strings(hooks[hook])
	}
	return hooks
}

// domAttrConsumerHooksInTemplate is the pure half: hooks and their file:line
// sites in one template, with template and HTML comments stripped first.
func domAttrConsumerHooksInTemplate(content, name string) map[string][]string {
	stripped := domAttrConsumerCommentPattern.ReplaceAllStringFunc(content, func(comment string) string {
		return strings.Repeat("\n", strings.Count(comment, "\n"))
	})
	hooks := map[string][]string{}
	for index, line := range strings.Split(stripped, "\n") {
		for _, hook := range domAttrConsumerAttrPattern.FindAllString(line, -1) {
			site := name + ":" + strconv.Itoa(index+1)
			if sites := hooks[hook]; len(sites) > 0 && sites[len(sites)-1] == site {
				continue
			}
			hooks[hook] = append(hooks[hook], site)
		}
	}
	return hooks
}

// domAttrConsumerConsumerIndex indexes every attribute token and dataset name
// that any source outside the templates tree spells out, and returns how many
// files it read.
func domAttrConsumerConsumerIndex(t *testing.T, root string) (map[string]bool, map[string]bool, int) {
	t.Helper()

	literals := map[string]bool{}
	datasets := map[string]bool{}
	templateDir := filepath.Join(root, "internal", "templates")
	files := 0

	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || domAttrConsumerSkipDirectories[name]) {
				return filepath.SkipDir
			}
			if path == templateDir {
				return filepath.SkipDir
			}
			return nil
		}
		extension := filepath.Ext(path)
		if !domAttrConsumerSourceExtensions[extension] {
			return nil
		}
		content, readErr := os.ReadFile(path) //nolint:gosec // walked path under the repository root
		if readErr != nil {
			return readErr
		}
		files++
		domAttrConsumerIndexSource(string(content), extension, literals, datasets)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
	return literals, datasets, files
}

// domAttrConsumerIndexSource records the attribute tokens one source spells out,
// plus — for a browser or spec source — the dataset names it reads.
func domAttrConsumerIndexSource(content, extension string, literals, datasets map[string]bool) {
	for _, token := range domAttrConsumerAttrPattern.FindAllString(content, -1) {
		literals[token] = true
	}
	if !domAttrConsumerScriptExtensions[extension] {
		return
	}
	for _, match := range domAttrConsumerDatasetPattern.FindAllStringSubmatch(content, -1) {
		if match[1] != "" {
			datasets[match[1]] = true
			continue
		}
		datasets[match[2]] = true
	}
}

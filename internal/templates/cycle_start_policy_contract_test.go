package templates

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The manual cycle-start confirmation is the only thing standing between a
// mis-tap and replacing a cycle start the owner already recorded. It is driven
// entirely from markup: the browser script finds the hidden
// `[data-cycle-start-policy]` node beside the submitting form and reads the
// conflict flags, the dates and the dialog copy off it as attributes. A copy of
// that node missing one attribute does not fail — it silently degrades. A lost
// `data-cycle-start-conflict` skips the replace dialog altogether; a lost
// `data-cycle-start-replace-message` opens it with an empty question.
//
// This guard is the completeness half of that contract: every policy node the
// template tree renders must carry every attribute the confirm script reads off
// one. The expected set is derived from the script itself, never re-listed here
// — a list written by hand agrees with the reader by construction and stops
// agreeing the day the script gains a twelfth name or renames one.
//
// The derivation is deliberately narrow: only names the script reads *through
// the policy node* count — `getAttribute("…")` on the node it resolved, plus the
// selector that resolves it. The form-level hooks the same script names
// (`data-cycle-start-confirm-form`, and the hidden inputs it addresses as
// `[data-cycle-start-replace-input]` / `[data-cycle-start-uncertain-input]`)
// belong to the form, not to the policy node, and requiring them here would
// report every correct node as incomplete.
//
// What it does not check: that the attribute *values* are the right ones, and
// that a form carrying `data-cycle-start-confirm-form` has a policy node beside
// it at all. Both are their own contract; this file is only about a node that
// renders with a hole in it.

// cycleStartPolicyScriptPath is the browser source that defines the contract:
// the reader whose string literals are the expected attribute set. If it moves,
// this guard fails rather than quietly deriving an empty set.
const cycleStartPolicyScriptPath = "web/src/js/app/23-cycle-start-confirm.js"

var (
	// The two shapes in which the script names an attribute of the policy node:
	// the selector that resolves the node, and every read off it. Both are bound
	// to the receiver, because "an attribute of the policy node" is what this
	// guard means and a bare `getAttribute(` or `querySelector(` is not that: the
	// script also addresses form-level hooks by name, and those entering the
	// expected set would report every correct node as incomplete while naming a
	// remedy — put the form's hook on the policy div — that is wrong. If the
	// receiver is ever renamed the derivation empties, which the floor below
	// turns into a loud failure naming this file.
	cycleStartPolicySelectorPattern = regexp.MustCompile(`parentElement\.querySelector\(\s*"\[(data-cycle-start-[a-z0-9-]*[a-z0-9])]"`)
	cycleStartPolicyGetAttrPattern  = regexp.MustCompile(`policyNode\.getAttribute\(\s*"(data-cycle-start-[a-z0-9-]*[a-z0-9])"`)

	// Template actions and comments are removed before the markup is scanned:
	// an action carries quotes of its own (`{{t .Messages "key"}}`) that would
	// desynchronize the attribute-value scan, and a hook named only in prose is
	// not rendered.
	cycleStartPolicyActionPattern   = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}|<!--.*?-->|\{\{[^{}]*\}\}`)
	cycleStartPolicyAttrNamePattern = regexp.MustCompile(`\bdata-cycle-start-[a-z0-9-]*[a-z0-9]`)
)

// cycleStartPolicyNode is one rendered `[data-cycle-start-policy]` element: the
// file:line that renders it and the attribute names it carries.
type cycleStartPolicyNode struct {
	Site       string
	Attributes map[string]bool
}

// TestEveryCycleStartPolicyNodeCarriesTheAttributesTheConfirmScriptReads is the
// completeness guard: a partial copy of the confirm island is a skipped
// confirmation, not a rendering bug, so every node must carry the full set.
func TestEveryCycleStartPolicyNodeCarriesTheAttributesTheConfirmScriptReads(t *testing.T) {
	root := domAttrConsumerRepoRoot(t)

	source, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cycleStartPolicyScriptPath)))
	if err != nil {
		t.Fatalf("read the confirm script that defines the contract (%s): %v", cycleStartPolicyScriptPath, err)
	}
	expected := cycleStartPolicyAttributesFromScript(string(source))

	// Anchors against a silent no-op: a derivation that resolved nothing, or a
	// scan that found no node, would otherwise report a clean tree. Both are
	// floors, not counts — the contract may grow a name and the island may be
	// rendered from one place or from several.
	if len(expected) < 8 {
		t.Fatalf("expected %s to name at least 8 attributes of the policy node, derived %d: %v",
			cycleStartPolicyScriptPath, len(expected), expected)
	}
	nodes := cycleStartPolicyNodes(t, filepath.Join(root, "internal", "templates"), root)
	if len(nodes) == 0 {
		t.Fatal("found no [data-cycle-start-policy] node in the template tree — the markup scan resolved nothing")
	}

	var incomplete []string
	for _, node := range nodes {
		missing := cycleStartPolicyMissing(node.Attributes, expected)
		if len(missing) == 0 {
			continue
		}
		incomplete = append(incomplete, node.Site+" is missing "+strings.Join(missing, ", "))
	}
	if len(incomplete) > 0 {
		sort.Strings(incomplete)
		t.Fatalf("%d of %d [data-cycle-start-policy] node(s) render without every attribute %s reads.\n"+
			"A node with a hole in it skips the confirmation instead of failing:\n\t%s",
			len(incomplete), len(nodes), cycleStartPolicyScriptPath, strings.Join(incomplete, "\n\t"))
	}
}

// TestCycleStartPolicyScanClassifiesItsOwnFixtures anchors the guard above on
// inputs this file owns — one node that must classify as complete and one that
// must classify as incomplete, plus the two derivation shapes and the form-level
// hooks that must stay out of the expected set. Anchoring on the live tree
// instead would leave the guard green the day its scan stopped resolving
// anything.
func TestCycleStartPolicyScanClassifiesItsOwnFixtures(t *testing.T) {
	script := `
	  return form.parentElement.querySelector("[data-cycle-start-policy]");
	  hasConflict: policyNode.getAttribute("data-cycle-start-conflict") === "true",
	  replaceMessage: String(policyNode.getAttribute("data-cycle-start-replace-message") || ""),
	  setCycleStartHiddenValue(form, "[data-cycle-start-replace-input]", false);
	  form.querySelector("[data-cycle-start-uncertain-input]");
	  form.getAttribute("data-cycle-start-confirm-form");
	  if (!form.matches("form[data-cycle-start-confirm-form]")) { return; }
	`
	expected := cycleStartPolicyAttributesFromScript(script)
	want := []string{
		"data-cycle-start-conflict",
		"data-cycle-start-policy",
		"data-cycle-start-replace-message",
	}
	if strings.Join(expected, " ") != strings.Join(want, " ") {
		t.Fatalf("expected the fixture script to yield exactly %v, derived %v", want, expected)
	}

	template := `<div
  hidden
  data-cycle-start-policy
  data-cycle-start-conflict="{{if .ManualCycleStartConflict}}true{{else}}false{{end}}"
  data-cycle-start-replace-message="{{t .Messages "dashboard.cycle_start_replace_message"}}"></div>
{{/* data-cycle-start-policy named in prose is not a rendered node */}}
<form data-cycle-start-confirm-form>
  <input type="hidden" data-cycle-start-replace-input>
</form>
<div hidden data-cycle-start-policy data-cycle-start-conflict="false"></div>`

	nodes := cycleStartPolicyNodesInTemplate(template, "fixture.html")
	if len(nodes) != 2 {
		t.Fatalf("expected the fixture markup to yield exactly two policy nodes, got %d: %v", len(nodes), nodes)
	}
	if nodes[0].Site != "fixture.html:1" {
		t.Fatalf("expected the node site to carry file and line, got %q", nodes[0].Site)
	}
	if missing := cycleStartPolicyMissing(nodes[0].Attributes, expected); len(missing) != 0 {
		t.Fatalf("the complete fixture node must classify as complete, reported missing %v", missing)
	}
	missing := cycleStartPolicyMissing(nodes[1].Attributes, expected)
	if strings.Join(missing, " ") != "data-cycle-start-replace-message" {
		t.Fatalf("the partial fixture node must be reported as missing data-cycle-start-replace-message, got %v", missing)
	}
}

// cycleStartFormComponentPath is the one definition of the confirm island, and
// cycleStartFormTemplateName the name every surface renders it by.
const (
	cycleStartFormComponentPath = "internal/templates/components/cycle_start_form.html"
	cycleStartFormTemplateName  = "cycle_start_form"
)

var (
	// The component's parameters, derived from the component: every `.Field` its
	// body reads, after its own comments are stripped. Written by hand this list
	// would agree with the component by construction and stop agreeing the day it
	// gains a parameter — which is the day a call site can silently omit one.
	cycleStartFormParamPattern = regexp.MustCompile(`\.([A-Z][A-Za-z0-9]*)`)
	cycleStartFormCommentOnly  = regexp.MustCompile(`(?s)\{\{/\*.*?\*/\}\}`)

	// One invocation, from `{{template "cycle_start_form"` to the `}}` that
	// closes it. The dict spans lines and carries parenthesised sub-expressions,
	// but no nested action, so the first `}}` is the right end.
	cycleStartFormCallPattern = regexp.MustCompile(`(?s)\{\{template "` + cycleStartFormTemplateName + `"(.*?)\}\}`)
)

// TestEveryCycleStartFormCallSitePassesTheWholeParameterSet is the other half of
// the contract, and after the extraction it is the half that can still fail.
// Rendering the island from one component means no surface can lose an attribute
// any more — but `dict` builds a plain map with no arity or key checking, and Go
// templates read a missing map key as the zero value rather than as an error. So
// a call site that drops `"Conflict"` from its fifteen key/value lines renders
// `data-cycle-start-conflict="false"`, the confirm script reads false, the
// replace dialog never opens, and a mis-tap replaces a cycle start the owner
// already recorded. The completeness guard above stays green throughout: the one
// policy node, in the component, still declares every attribute.
//
// `Implantation` and `Active` fail the same silent way. `ShortGap` happens to
// error instead — `{{gt .ShortGap 0}}` refuses nil — which is luck, not a
// property to rely on.
func TestEveryCycleStartFormCallSitePassesTheWholeParameterSet(t *testing.T) {
	root := domAttrConsumerRepoRoot(t)

	component, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cycleStartFormComponentPath)))
	if err != nil {
		t.Fatalf("read the component that defines the parameter set (%s): %v", cycleStartFormComponentPath, err)
	}
	parameters := cycleStartFormParameters(string(component))
	if len(parameters) < 10 {
		t.Fatalf("expected %s to read at least 10 parameters, derived %d: %v — a derivation that resolved almost nothing would pass every call site",
			cycleStartFormComponentPath, len(parameters), parameters)
	}

	sites := cycleStartFormCallSites(t, filepath.Join(root, "internal", "templates"), root)
	if len(sites) == 0 {
		t.Fatalf("found no {{template %q}} call site — the scan resolved nothing", cycleStartFormTemplateName)
	}

	var incomplete []string
	for _, site := range sites {
		var missing []string
		for _, parameter := range parameters {
			if !strings.Contains(site.Action, `"`+parameter+`"`) {
				missing = append(missing, parameter)
			}
		}
		if len(missing) > 0 {
			incomplete = append(incomplete, site.Site+" omits "+strings.Join(missing, ", "))
		}
	}
	if len(incomplete) > 0 {
		sort.Strings(incomplete)
		t.Fatalf("%d of %d %q call site(s) omit a parameter the component reads.\n"+
			"An omitted key is not an error — the component reads it as the zero value, so a dropped policy flag renders as \"false\" and skips the confirmation:\n\t%s",
			len(incomplete), len(sites), cycleStartFormTemplateName, strings.Join(incomplete, "\n\t"))
	}
}

// cycleStartFormParameters derives the component's parameter names from its own
// body, with its documentation comment stripped first so the parameter list in
// the prose cannot stand in for a parameter the markup actually reads.
func cycleStartFormParameters(component string) []string {
	found := map[string]bool{}
	for _, match := range cycleStartFormParamPattern.FindAllStringSubmatch(cycleStartFormCommentOnly.ReplaceAllString(component, ""), -1) {
		found[match[1]] = true
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// cycleStartFormCallSite is one invocation: where it is, and the action text
// whose quoted keys are the parameters it passes.
type cycleStartFormCallSite struct {
	Site   string
	Action string
}

// cycleStartFormCallSites collects every invocation under the templates
// directory, skipping the component's own definition.
func cycleStartFormCallSites(t *testing.T, templateDir, root string) []cycleStartFormCallSite {
	t.Helper()

	var sites []cycleStartFormCallSite
	walkErr := filepath.WalkDir(templateDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".html" {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		name := filepath.ToSlash(relative)
		if name == cycleStartFormComponentPath {
			return nil
		}
		for _, match := range cycleStartFormCallPattern.FindAllStringSubmatchIndex(string(content), -1) {
			line := strconv.Itoa(strings.Count(string(content)[:match[0]], "\n") + 1)
			sites = append(sites, cycleStartFormCallSite{
				Site:   name + ":" + line,
				Action: string(content)[match[0]:match[1]],
			})
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", templateDir, walkErr)
	}
	return sites
}

// cycleStartPolicyAttributesFromScript derives the expected attribute set from
// the confirm script: the selector that resolves the policy node plus every
// attribute read off it, sorted and deduplicated.
func cycleStartPolicyAttributesFromScript(source string) []string {
	found := map[string]bool{}
	for _, pattern := range []*regexp.Regexp{cycleStartPolicySelectorPattern, cycleStartPolicyGetAttrPattern} {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			found[match[1]] = true
		}
	}
	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// cycleStartPolicyMissing reports which of the expected attribute names the node
// does not carry.
func cycleStartPolicyMissing(attributes map[string]bool, expected []string) []string {
	var missing []string
	for _, name := range expected {
		if !attributes[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

// cycleStartPolicyNodes collects every policy node rendered under the templates
// directory, with sites relative to the repository root.
func cycleStartPolicyNodes(t *testing.T, templateDir, root string) []cycleStartPolicyNode {
	t.Helper()

	var nodes []cycleStartPolicyNode
	walkErr := filepath.WalkDir(templateDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".html" {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		nodes = append(nodes, cycleStartPolicyNodesInTemplate(string(content), filepath.ToSlash(relative))...)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", templateDir, walkErr)
	}
	return nodes
}

// cycleStartPolicyNodesInTemplate is the pure half: the policy nodes one
// template renders, with template actions and comments removed first.
func cycleStartPolicyNodesInTemplate(content, name string) []cycleStartPolicyNode {
	stripped := cycleStartPolicyActionPattern.ReplaceAllStringFunc(content, func(action string) string {
		return strings.Repeat("\n", strings.Count(action, "\n"))
	})

	var nodes []cycleStartPolicyNode
	cursor := 0
	for {
		relative := strings.IndexByte(stripped[cursor:], '<')
		if relative < 0 {
			return nodes
		}
		open := cursor + relative
		end := cycleStartPolicyTagEnd(stripped, open)
		if end < 0 {
			return nodes
		}
		attributes := map[string]bool{}
		for _, attribute := range cycleStartPolicyAttrNamePattern.FindAllString(stripped[open:end], -1) {
			attributes[attribute] = true
		}
		if attributes["data-cycle-start-policy"] {
			line := strconv.Itoa(strings.Count(stripped[:open], "\n") + 1)
			nodes = append(nodes, cycleStartPolicyNode{Site: name + ":" + line, Attributes: attributes})
		}
		cursor = end + 1
	}
}

// cycleStartPolicyTagEnd returns the index of the `>` that closes the tag
// opening at start, ignoring one inside a quoted attribute value.
func cycleStartPolicyTagEnd(content string, start int) int {
	var quote byte
	for index := start + 1; index < len(content); index++ {
		character := content[index]
		switch {
		case quote != 0:
			if character == quote {
				quote = 0
			}
		case character == '"' || character == '\'':
			quote = character
		case character == '>':
			return index
		}
	}
	return -1
}

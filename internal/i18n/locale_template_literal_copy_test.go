package i18n_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"text/template/parse"
	"unicode"

	"golang.org/x/net/html"
)

// The third direction on the template side. The other two barriers both start
// from a `t`/`tn` call: reachability asks whether the application still names
// each catalogue key, and the call-site barrier asks whether the catalogue
// still answers each key a template renders. Neither can see copy that never
// went through a call at all, and that is the whole defect class here —
// `placeholder="DD"`, `placeholder="MM"`, `placeholder="YYYY"` shipped in
// components/date_field.html beside a `<label>` and an `aria-label` that were
// both correctly translated, so every non-English account read the English
// abbreviations inside the boxes it was typing a date into.
//
// The reader works on the source regions the template parser attributed to
// literal TEXT rather than to an action, with everything else masked out. That
// masking is what makes "carries no `{{ }}`" a measurement instead of a guess:
// a value assembled from an action leaves no letters behind to find, and one
// typed into the markup leaves all of them. Two questions are asked of the
// unmasked remainder, because untranslated copy reaches a reader by two routes
// and they need different verdicts:
//
//   - a human-readable ATTRIBUTE (`placeholder`, `title`, `alt`, `aria-label`)
//     whose value still carries letters — a defect unless the value is a
//     format mask that reads the same in every language, which is a per-site
//     judgement and therefore an explicit, justified exemption list;
//   - visible TEXT between tags that still carries letters — a defect unless
//     the text is a reviewed language-independent symbol such as `°C`, which
//     is a small closed set and therefore an allow-list keyed by the symbol
//     itself rather than by where it appears.

// humanReadableAttributes are the attributes whose value a person reads. An
// attribute outside this set may legitimately hold English forever
// (`class`, `data-*`, `hx-*`, `id`, `pattern`, `autocomplete`, `type`), which
// is why this barrier is keyed on a named set rather than on "any attribute
// holding letters".
var humanReadableAttributes = map[string]bool{
	"alt":              true,
	"aria-description": true,
	"aria-label":       true,
	"aria-placeholder": true,
	"placeholder":      true,
	"title":            true,
}

// reviewedLanguageIndependentText is the closed set of visible text nodes that
// may stay out of the catalogue. Membership is a claim that the string reads
// identically in en/ru/es/fr/de/it, not that translating it is inconvenient.
//
// The degree symbols qualify: `°C` and `°F` are the SI and customary unit
// symbols, unchanged across all six locales, and each is rendered immediately
// beside its translated name (`settings.tracking.temperature_unit_celsius` /
// `_fahrenheit`) in components/settings_tracking.html, so the localized word is
// on screen either way. Translating them would put six identical copies of a
// symbol into the catalogues and add a seventh place to edit.
var reviewedLanguageIndependentText = map[string]string{
	"°C": "SI unit symbol; identical in every supported locale and rendered beside settings.tracking.temperature_unit_celsius",
	"°F": "unit symbol; identical in every supported locale and rendered beside settings.tracking.temperature_unit_fahrenheit",
}

// literalAttributeExemption is one human-readable attribute value that is
// allowed to stay in the markup, named by file, attribute and exact value.
//
// Nothing here is matched by shape. An exemption whose value drifts stops
// matching and the site reports as a finding again; an exemption that matches
// nothing at all is itself a failure (see the barrier), because a stale
// exemption is a hole nobody is looking through any more.
type literalAttributeExemption struct {
	file      string
	attribute string
	value     string
	reason    string
}

var reviewedLiteralAttributes = []literalAttributeExemption{
	{
		file:      "forgot_password.html",
		attribute: "placeholder",
		value:     "OVUM-XXXX-XXXX-XXXX",
		reason: "the recovery-code mask, not prose: `OVUM` is the literal prefix every generated " +
			"recovery code carries and the X groups are its fixed shape, so a translated copy would " +
			"describe a code the product never issues",
	},
}

// templateTextSpan is one source region the parser read as literal text.
type templateTextSpan struct {
	start int
	end   int
}

// literalCopyKind separates the two routes so a finding says which rule it
// broke and the two barriers can read the same collected evidence.
type literalCopyKind string

const (
	literalCopyAttribute literalCopyKind = "attribute"
	literalCopyText      literalCopyKind = "text"
)

// templateLiteralCopySite is one piece of human-readable copy typed into the
// markup rather than resolved from the catalogue.
type templateLiteralCopySite struct {
	file      string
	line      int
	kind      literalCopyKind
	attribute string
	value     string
}

func (site templateLiteralCopySite) String() string {
	if site.kind == literalCopyAttribute {
		return fmt.Sprintf("%s:%d %s=%q", site.file, site.line, site.attribute, site.value)
	}
	return fmt.Sprintf("%s:%d text %q", site.file, site.line, site.value)
}

// recordTemplateTextSpan is called for every text node the template collector
// walks. Only the SPAN is kept: the text itself is read back out of the file's
// own source, so the reader below sees the bytes a browser would, including the
// tag markup that surrounds them.
func recordTemplateTextSpan(node *parse.TextNode, evidence *sourceEvidence) {
	if node == nil || len(node.Text) == 0 {
		return
	}
	start := int(node.Position())
	evidence.pendingTemplateTextSpans = append(evidence.pendingTemplateTextSpans, templateTextSpan{
		start: start,
		end:   start + len(node.Text),
	})
}

// maskTemplateActions rewrites every byte the parser did NOT attribute to a
// text node as a space, keeping newlines wherever they stood.
//
// Same length in, same length out, so an offset in the result is an offset in
// the original and a finding can name a line a reader can open. The masked form
// is what makes the barrier's central question decidable: an attribute value or
// a text node built from an action collapses to blanks, and only copy typed
// into the markup survives with its letters.
func maskTemplateActions(source string, spans []templateTextSpan) string {
	masked := make([]byte, len(source))
	for index := range len(source) {
		if source[index] == '\n' {
			masked[index] = '\n'
			continue
		}
		masked[index] = ' '
	}
	for _, span := range spans {
		if span.start < 0 || span.end > len(source) || span.start > span.end {
			continue
		}
		copy(masked[span.start:span.end], source[span.start:span.end])
	}
	return string(masked)
}

// recordTemplateLiteralCopy reads one masked template and appends what it
// finds. It consumes the pending spans, so each file is read against its own
// text and the next file starts clean.
func recordTemplateLiteralCopy(origin templateOrigin, evidence *sourceEvidence) {
	masked := maskTemplateActions(origin.source, evidence.pendingTemplateTextSpans)
	evidence.pendingTemplateTextSpans = nil

	tokenizer := html.NewTokenizer(strings.NewReader(masked))
	offset := 0
	// Text inside these elements is not copy a reader sees. The strict CSP
	// keeps inline scripts out of the templates entirely, but a barrier that
	// depends on that would start reporting the day a JSON island lands.
	rawTextDepth := 0
	for {
		tokenType := tokenizer.Next()
		raw := string(tokenizer.Raw())
		start := offset
		offset += len(raw)

		switch tokenType {
		case html.ErrorToken:
			return
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttribute := tokenizer.TagName()
			tag := strings.ToLower(string(name))
			evidence.templateMarkupElementsScanned++
			if tokenType == html.StartTagToken && isRawTextElement(tag) {
				rawTextDepth++
			}
			for hasAttribute {
				var key, value []byte
				key, value, hasAttribute = tokenizer.TagAttr()
				attribute := strings.ToLower(string(key))
				if !humanReadableAttributes[attribute] {
					continue
				}
				evidence.templateHumanReadableAttrs++
				text := strings.TrimSpace(string(value))
				if !containsLetter(text) {
					continue
				}
				evidence.templateLiteralCopySites = append(evidence.templateLiteralCopySites, templateLiteralCopySite{
					file:      origin.file,
					line:      origin.lineOf(parse.Pos(start + offsetOfAttribute(raw, attribute))),
					kind:      literalCopyAttribute,
					attribute: attribute,
					value:     text,
				})
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			if isRawTextElement(strings.ToLower(string(name))) && rawTextDepth > 0 {
				rawTextDepth--
			}
		case html.TextToken:
			if rawTextDepth > 0 {
				continue
			}
			text := strings.TrimSpace(string(tokenizer.Text()))
			if !containsLetter(withoutReviewedSymbols(text, evidence.reviewedSymbolsSeen)) {
				continue
			}
			evidence.templateLiteralCopySites = append(evidence.templateLiteralCopySites, templateLiteralCopySite{
				file:  origin.file,
				line:  origin.lineOf(parse.Pos(start + leadingBlankBytes(raw))),
				kind:  literalCopyText,
				value: text,
			})
		}
	}
}

func isRawTextElement(tag string) bool {
	return tag == "script" || tag == "style" || tag == "textarea"
}

// offsetOfAttribute locates an attribute inside its own start tag, so a finding
// on a tag spread over a dozen lines names the line the attribute is on.
func offsetOfAttribute(raw string, attribute string) int {
	index := strings.Index(strings.ToLower(raw), attribute+"=")
	if index < 0 {
		return 0
	}
	return index
}

// leadingBlankBytes skips the whitespace a text token opens with, which after
// masking is usually a masked action, so the reported line is the line the
// visible characters are on.
func leadingBlankBytes(raw string) int {
	return len(raw) - len(strings.TrimLeft(raw, " \t\r\n"))
}

func containsLetter(text string) bool {
	for _, symbol := range text {
		if unicode.IsLetter(symbol) {
			return true
		}
	}
	return false
}

// withoutReviewedSymbols blanks every allow-listed symbol out of a text node
// and records which ones it actually found. The record is what lets the text
// barrier refuse an entry that matches nothing: the attribute list already
// treats a stale exemption as a hole nobody is looking through, and an entry
// here reaches further than one there — it is keyed by the symbol rather than
// by the site, so it allows that symbol in every template at once.
//
// seen may be nil, which is what the fixture self-tests pass when they are
// measuring the reader rather than the allow-list.
func withoutReviewedSymbols(text string, seen map[string]bool) string {
	for symbol := range reviewedLanguageIndependentText {
		if !strings.Contains(text, symbol) {
			continue
		}
		if seen != nil {
			seen[symbol] = true
		}
		text = strings.ReplaceAll(text, symbol, " ")
	}
	return text
}

// sortedReviewedSymbols keeps the staleness report deterministic; map order
// would shuffle two failures that must read the same on every run.
func sortedReviewedSymbols() []string {
	symbols := make([]string, 0, len(reviewedLanguageIndependentText))
	for symbol := range reviewedLanguageIndependentText {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	return symbols
}

// literalCopyFindings splits the collected sites into the ones each barrier
// owns and applies the exemption list, reporting which exemptions fired.
//
// Taking the sites as an argument rather than reading them is what lets the
// self-tests below drive this function with a fixture, so the barrier's own
// logic is measured instead of assumed.
func literalCopyFindings(sites []templateLiteralCopySite, kind literalCopyKind) (findings []templateLiteralCopySite, exemptionsUsed map[int]bool) {
	exemptionsUsed = map[int]bool{}
	for _, site := range sites {
		if site.kind != kind {
			continue
		}
		exempt := false
		for index, exemption := range reviewedLiteralAttributes {
			if site.kind != literalCopyAttribute {
				continue
			}
			if exemption.file != site.file || exemption.attribute != site.attribute || exemption.value != site.value {
				continue
			}
			exemptionsUsed[index] = true
			exempt = true
		}
		if exempt {
			continue
		}
		findings = append(findings, site)
	}
	sort.Slice(findings, func(a, b int) bool {
		if findings[a].file != findings[b].file {
			return findings[a].file < findings[b].file
		}
		return findings[a].line < findings[b].line
	})
	return findings, exemptionsUsed
}

// TestShippedTemplatesLocalizeEveryHumanReadableAttribute is the barrier for
// attribute-borne copy.
func TestShippedTemplatesLocalizeEveryHumanReadableAttribute(t *testing.T) {
	evidence := newSourceEvidence()
	if err := collectFromShippedTemplates(evidence); err != nil {
		t.Fatalf("sweeping the shipped templates: %v", err)
	}
	if err := literalCopySweepIsUnderfed(evidence); err != nil {
		t.Fatalf("%v", err)
	}

	findings, exemptionsUsed := literalCopyFindings(evidence.templateLiteralCopySites, literalCopyAttribute)
	for index, exemption := range reviewedLiteralAttributes {
		if exemptionsUsed[index] {
			continue
		}
		t.Errorf("the exemption for %s %s=%q matched no attribute in the shipped templates; a stale exemption is a hole nobody is looking through, so delete it or fix the value it names",
			exemption.file, exemption.attribute, exemption.value)
	}
	if len(findings) == 0 {
		return
	}

	var report strings.Builder
	for _, site := range findings {
		fmt.Fprintf(&report, "  %s\n", site)
	}
	t.Fatalf("%d human-readable attribute value(s) typed into the markup instead of resolved from the catalogue:\n%s\n"+
		"the value carries letters and no template action, so every account reads it in English whatever language it "+
		"selected — and the visible label beside it is usually translated, which is what keeps this invisible. Add a key "+
		"to every catalogue and render it with `t`; if the value is a format mask that reads the same in all six "+
		"languages, add it to reviewedLiteralAttributes with the reason spelled out.",
		len(findings), report.String())
}

// TestShippedTemplatesLeaveNoUntranslatedVisibleText is the barrier for copy
// typed between tags.
func TestShippedTemplatesLeaveNoUntranslatedVisibleText(t *testing.T) {
	evidence := newSourceEvidence()
	if err := collectFromShippedTemplates(evidence); err != nil {
		t.Fatalf("sweeping the shipped templates: %v", err)
	}
	if err := literalCopySweepIsUnderfed(evidence); err != nil {
		t.Fatalf("%v", err)
	}

	// Same refusal the attribute barrier applies to a stale exemption. An
	// entry here reaches further than one there — it is keyed by the symbol,
	// not by the site, so it allows that symbol in every template at once —
	// which makes an entry describing markup the tree no longer carries the
	// wider hole of the two, and the reason written beside it the more
	// misleading claim.
	for _, symbol := range sortedReviewedSymbols() {
		if evidence.reviewedSymbolsSeen[symbol] {
			continue
		}
		t.Errorf("the reviewed-symbol allow-list entry %q (%s) matched no visible text in the shipped templates; a stale entry allows that symbol everywhere on the strength of a site that is gone, so delete it or fix the text it names",
			symbol, reviewedLanguageIndependentText[symbol])
	}

	findings, _ := literalCopyFindings(evidence.templateLiteralCopySites, literalCopyText)
	if len(findings) == 0 {
		return
	}

	var report strings.Builder
	for _, site := range findings {
		fmt.Fprintf(&report, "  %s\n", site)
	}
	t.Fatalf("%d visible text node(s) typed into the markup instead of resolved from the catalogue:\n%s\n"+
		"render the text with `t` and define its key in every catalogue. Only a string that reads identically in "+
		"en/ru/es/fr/de/it belongs in reviewedLanguageIndependentText, and it belongs there with the reason written out.",
		len(findings), report.String())
}

// literalCopySweepIsUnderfed is the silence guard both barriers need. Zero
// findings is the passing verdict, which is exactly the verdict a reader that
// stopped reading also produces — so the counters below are what separate
// "nothing is wrong" from "nothing was looked at". Measured 2026-08-25 on the
// tree this barrier ships with: 38 templates, 1 787 elements, 67
// human-readable attribute values. The floors carry slack; what they refuse is
// a reader that walked the tree and recognised no markup.
//
// It returns the refusal rather than failing a *testing.T so the self-test
// below can measure the guard itself without a fake T, whose Fatalf would take
// the calling goroutine down with it.
func literalCopySweepIsUnderfed(evidence *sourceEvidence) error {
	if evidence.templateFiles < 30 {
		return fmt.Errorf("parsed only %d templates; the embedded set is far larger, so this sweep missed a directory", evidence.templateFiles)
	}
	if evidence.templateMarkupElementsScanned < 1200 {
		return fmt.Errorf("read only %d element(s) out of the shipped templates; a masked source the tokenizer cannot walk yields no findings for the same reason clean markup does", evidence.templateMarkupElementsScanned)
	}
	if evidence.templateHumanReadableAttrs < 40 {
		return fmt.Errorf("read only %d human-readable attribute value(s); the attribute reader stopped seeing attributes, so the barrier is checking almost nothing", evidence.templateHumanReadableAttrs)
	}
	return nil
}

// The barriers are only as good as the reader that decides what carries an
// action, so the reader is measured on a synthesised template rather than on
// the repository's own — a fixture that measures ovumcy-web against itself only
// proves ovumcy-web agrees with itself.
func TestTemplateLiteralCopyReaderSeparatesTypedCopyFromResolvedCopy(t *testing.T) {
	const fixture = `{{define "content"}}{{$dayLabel := "Day"}}{{$openPickerLabel := "Open the picker"}}
<input placeholder="DD" aria-label="{{$dayLabel}}" class="input-field" data-part="day">
<input placeholder="{{t .Messages "probe.month_placeholder"}}" pattern="[0-9]*">
<img src="/static/x.png" alt="Chart of the last cycle">
<button
  type="button"
  title="{{$openPickerLabel}}"
  aria-label="{{$openPickerLabel}}">
  <span>°C</span>
  <span>{{t .Messages "probe.celsius"}}</span>
</button>
<p>Body temperature</p>
<p>{{t .Messages "probe.hint"}}</p>
<p>·</p>
<script>var untranslated = "Body temperature";</script>
{{end}}`

	evidence := newSourceEvidence()
	if err := collectFromTemplateSource("fixture.html", fixture, evidence); err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}

	attributes, _ := literalCopyFindings(evidence.templateLiteralCopySites, literalCopyAttribute)
	var attributeNames []string
	for _, site := range attributes {
		attributeNames = append(attributeNames, fmt.Sprintf("%s=%s@%d", site.attribute, site.value, site.line))
	}
	wantAttributes := []string{"placeholder=DD@2", "alt=Chart of the last cycle@4"}
	if strings.Join(attributeNames, ",") != strings.Join(wantAttributes, ",") {
		t.Errorf("the attribute reader reported %v, want %v; a value typed into the markup must fail, a value resolved by an action must pass because masking leaves it without letters, an attribute nobody reads (class, pattern, data-*, src, type) must never be read at all, and each finding must name the line its own attribute sits on rather than the line its tag opens with",
			attributeNames, wantAttributes)
	}

	texts, _ := literalCopyFindings(evidence.templateLiteralCopySites, literalCopyText)
	var textValues []string
	for _, site := range texts {
		textValues = append(textValues, fmt.Sprintf("%s@%d", site.value, site.line))
	}
	wantTexts := []string{"Body temperature@12"}
	if strings.Join(textValues, ",") != strings.Join(wantTexts, ",") {
		t.Errorf("the text reader reported %v, want %v; typed prose must fail, a reviewed symbol (°C) must pass, text produced by an action must pass, punctuation carrying no letters must pass, and script contents are not copy a reader sees",
			textValues, wantTexts)
	}

	if evidence.templateHumanReadableAttrs != 6 {
		t.Errorf("counted %d human-readable attribute value(s); the fixture ships six (placeholder twice, alt, title, aria-label twice), and the count is what the silence guard reads", evidence.templateHumanReadableAttrs)
	}
	if len(evidence.pendingTemplateTextSpans) != 0 {
		t.Errorf("%d text span(s) were left pending after the file was read; spans that survive into the next file mask that file against the wrong source", len(evidence.pendingTemplateTextSpans))
	}
}

// The masking is the whole measurement, so it is measured directly: same
// length, same lines, and the action bytes gone.
func TestTemplateActionMaskingKeepsEveryOffsetWhereItWas(t *testing.T) {
	const source = "<p class=\"a\">{{t .Messages \"probe.key\"}}</p>\n<p>plain</p>\n"
	spans := []templateTextSpan{{start: 0, end: 13}, {start: 40, end: len(source)}}

	masked := maskTemplateActions(source, spans)
	if len(masked) != len(source) {
		t.Fatalf("masking returned %d bytes for a %d-byte source; every reported line number depends on the two being equal", len(masked), len(source))
	}
	if strings.Count(masked, "\n") != strings.Count(source, "\n") {
		t.Errorf("masking changed the newline count from %d to %d; a finding would then name a line the file does not have", strings.Count(source, "\n"), strings.Count(masked, "\n"))
	}
	if strings.Contains(masked, "probe.key") || strings.Contains(masked, "Messages") {
		t.Errorf("the masked source still carries the action text %q; an attribute or text node assembled by an action would read as typed copy", masked)
	}
	if !strings.Contains(masked, `<p class="a">`) || !strings.Contains(masked, "plain") {
		t.Errorf("the masked source lost literal markup: %q; masking away the text nodes leaves the barrier reading nothing", masked)
	}
}

// A barrier that reports success on an empty sweep is the failure this class of
// test cannot see in itself, so the floor is measured in both directions.
func TestLiteralCopyBarrierRefusesASweepThatReadNoMarkup(t *testing.T) {
	evidence := newSourceEvidence()
	if err := collectFromTemplateSource("fixture.html", `{{t .Messages "probe.only"}}`, evidence); err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}
	if len(evidence.templateLiteralCopySites) != 0 {
		t.Fatalf("a template with no literal copy reported %d site(s); the fixture proves nothing", len(evidence.templateLiteralCopySites))
	}

	if err := literalCopySweepIsUnderfed(evidence); err == nil {
		t.Fatalf("the silence guard passed a sweep that read %d template(s), %d element(s) and %d attribute(s); it is the only thing standing between a reader that stopped reading and a green barrier",
			evidence.templateFiles, evidence.templateMarkupElementsScanned, evidence.templateHumanReadableAttrs)
	}
}

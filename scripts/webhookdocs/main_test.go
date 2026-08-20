// Package webhookdocs guards the four documents that restate the outbound
// webhook egress contract against a PARTIAL move. A change to the address
// classifier or to the delivery envelope touches SECURITY.md,
// docs/SECURITY_INVARIANTS.md, docs/notifications.md and docs/self-hosted.md,
// and nothing failed when it moved only three: #406 moved the matrix and left
// the public mirror stale for a day, #407 repaired it.
//
// The obvious shape for this guard — "every address form named in every
// document" — was measured and refuted before it was written: the four
// documents name different subsets of the six IPv6 transition prefixes
// (1/1/4/1 hits), because they are written for different readers. Such a check
// is red on day one and stays red. What each pair genuinely owes is narrower,
// and that is what this file checks:
//
//   - SECURITY.md and its public mirror owe the same CLAIM. Every row of the
//     matrix's webhook section is either mirrored (the mirror states the same
//     mechanism, in its own register) or explicitly declared matrix-only with
//     a reason. The row set is swept out of the document itself rather than
//     listed here, so a row added later cannot join without a verdict — a
//     hand-kept list of covered rows would reproduce the very defect this
//     guards against, one layer up.
//   - The matrix's citation must resolve: every test named by a webhook row is
//     defined in a file that same row links.
//   - The two operator documents owe the GATE and its DEFAULT, never the
//     prefix list — and the default is read out of the code that parses it,
//     never restated here, so flipping the default in code fails until every
//     document that states it has moved.
//
// gdpr_claims_test.go extends the same pair discipline past the webhook rows,
// to the GDPR cross-reference table: the rows whose claims the code can
// contradict outright — consent capture, outbound transmission, and the
// privacy-by-default settings — are read against the code that decides them.
// The directory name predates that second subject and the guards share every
// helper, so they stay in one package.
package webhookdocs

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	matrixPath = "SECURITY.md"
	mirrorPath = "docs/SECURITY_INVARIANTS.md"

	// webhookHeading is the matrix section this guard sweeps. The mirror
	// points readers at it by name, which is asserted below: a rename that
	// touches one document and not the other would otherwise leave the sweep
	// reading an empty section.
	webhookHeading = "Webhook Notifications (outbound egress)"

	// gateEnv is the operator-facing name of the private-address block, and
	// gateParsePath is the file whose parse of it defines the default every
	// document must agree with.
	gateEnv       = "WEBHOOK_BLOCK_PRIVATE_ADDRESSES"
	gateParseGlob = "cmd/ovumcy/*.go"
)

// operatorDocs are the two documents an operator actually follows. They owe
// the gate and its default; they deliberately owe NO enumeration of the
// address forms (measured: they carry 4 and 1 of the six transition prefixes
// respectively, against 1 in each policy document).
var operatorDocs = []string{"docs/notifications.md", "docs/self-hosted.md"}

// claimAnchor ties one matrix row to what the public mirror owes for it.
// Exactly one of mirror / why is set: mirror lists the patterns the mirror
// text must all carry, why records the reason a row is matrix-only.
type claimAnchor struct {
	name   string
	row    string
	mirror []string
	why    string
}

// claimAnchors is the verdict per matrix row. It is not an allowlist of rows
// to check — every row must match exactly one anchor and every anchor exactly
// one row, so adding a claim to the matrix fails here until its verdict is
// recorded, and deleting one fails until its anchor goes too.
var claimAnchors = []claimAnchor{
	{
		name: "save is owner-only and CSRF-protected",
		row:  "settings save is OwnerOnly",
		why:  "the mirror states OwnerOnly + CSRF once, app-wide (its endpoint and CSRF sections), and does not restate it per subsystem",
	},
	{
		name:   "stored URL is a write-only secret",
		row:    "write-only secret: after it is stored",
		mirror: []string{"never rendered back into the page"},
	},
	{
		name:   "URL encrypted at rest",
		row:    "encrypted at rest",
		mirror: []string{"encrypted at rest"},
	},
	{
		name:   "non-http scheme refused at save",
		row:    "rejected at save",
		mirror: []string{"`http`/`https` only"},
	},
	{
		name:   "blank keeps, remove clears",
		row:    "blank URL submission",
		mirror: []string{"blank URL on save leaves the stored endpoint unchanged"},
	},
	{
		name:   "one owner's reminder never reaches another's endpoint",
		row:    "multi-owner batch",
		mirror: []string{"never reaches another owner.s endpoint"},
	},
	{
		name:   "zero redirects",
		row:    "refuses ALL redirects",
		mirror: []string{"zero redirects"},
	},
	{
		name:   "only http/https delivered",
		row:    "schemes are delivered",
		mirror: []string{"`http`/`https` only"},
	},
	{
		name: "private-address gate: resolve, refuse, dial the validated IP",
		row:  "gate on, a hostname that resolves",
		mirror: []string{
			"off-by-default private-address block",
			gateEnv,
			"every IPv6 transition form that embeds an IPv4",
			"validated IP",
		},
	},
	{
		name:   "bounded timeout and capped body read",
		row:    "cannot stall the pass",
		mirror: []string{"bounded timeout", "size-capped response read"},
	},
	{
		name:   "capped response header block",
		row:    "unbounded response HEADER block",
		mirror: []string{"cap on the response header block"},
	},
	{
		name:   "endpoint must name a host and an in-range port",
		row:    "must name a host",
		mirror: []string{"must name a host and, when it carries one, an in-range port"},
	},
	{
		name:   "logs carry the host only",
		row:    "destination HOST only",
		mirror: []string{"host only"},
	},
	{
		name:   "disclaimer in every payload",
		row:    "mandatory medical-safety disclaimer",
		mirror: []string{"medical-safety disclaimer"},
	},
	{
		name: "watermark advances only on success",
		row:  "per-kind watermark",
		why:  "delivery-pass idempotency, not a privacy or egress invariant: the mirror states what may leave the instance, not how often a reminder is retried",
	},
	{
		name: "undecryptable URL is skipped",
		row:  "fails to decrypt",
		why:  "the mirror states the SECRET_KEY-rotation consequence as one class; the per-owner skip is delivery behaviour, and the canonical per-domain table is docs/security/cryptography.md",
	},
	{
		name: "--dry-run makes no request",
		row:  "computes what would be sent",
		why:  "an operator-CLI affordance documented in docs/notifications.md; the mirror carries the CLI's privacy line (no health specifics), not its flag behaviour",
	},
	{
		name:   "notify prints no health specifics by default",
		row:    "prints no health specific",
		mirror: []string{"--show-health-details"},
	},
	{
		name:   "endpoint reaches the CLI from one source",
		row:    "exactly one out-of-band source",
		mirror: []string{"--url-stdin"},
	},
}

type matrixRow struct {
	claim    string
	enforced string
}

// TestClaimAnchorLedgerIsWellFormed keeps the ledger itself honest: a row is
// either mirrored or matrix-only-with-a-reason, never both and never neither.
func TestClaimAnchorLedgerIsWellFormed(t *testing.T) {
	seen := map[string]struct{}{}
	for _, anchor := range claimAnchors {
		if _, dup := seen[anchor.name]; dup {
			t.Errorf("duplicate anchor name %q: names appear in failure messages and must identify one row", anchor.name)
		}
		seen[anchor.name] = struct{}{}

		mirrored := len(anchor.mirror) > 0
		if mirrored == (anchor.why != "") {
			t.Errorf("anchor %q must either carry mirror patterns or state why the mirror does not owe the claim, not both and not neither", anchor.name)
		}
	}
}

// TestWebhookMatrixRowsAreMirroredOrDeclaredMatrixOnly is the pair check. It
// sweeps every row of the matrix's webhook section and requires the public
// mirror to carry the claims that are the mirror's to carry.
func TestWebhookMatrixRowsAreMirroredOrDeclaredMatrixOnly(t *testing.T) {
	root := repoRoot(t)
	rows := webhookMatrixRows(t, root)
	mirror := normalizeSpace(readDoc(t, root, mirrorPath))

	// The mirror points at this section by name. A rename on one side only
	// would leave the sweep above reading a section that no longer exists.
	pointer := "See the matrix's *" + webhookHeading + "* rows"
	if !strings.Contains(mirror, pointer) {
		t.Errorf("%s does not point at the matrix section it mirrors (looked for %q): keep the heading and the cross-reference in step", mirrorPath, pointer)
	}

	matches := map[string]int{}
	for _, row := range rows {
		var hits []string
		for _, anchor := range claimAnchors {
			if matchesPattern(t, anchor.row, row.claim) {
				hits = append(hits, anchor.name)
			}
		}
		switch len(hits) {
		case 0:
			t.Errorf("no anchor covers this %s webhook row, so nothing decides whether the public mirror owes it:\n  %s\nAdd an anchor to claimAnchors — with the mirror sentence it owes, or with the reason the mirror does not carry it.", matrixPath, truncate(row.claim))
		case 1:
			matches[hits[0]]++
		default:
			t.Errorf("row matched %d anchors (%s), so the verdict is ambiguous — make the row patterns distinctive:\n  %s", len(hits), strings.Join(hits, ", "), truncate(row.claim))
		}
	}

	for _, anchor := range claimAnchors {
		switch matches[anchor.name] {
		case 1:
		case 0:
			t.Errorf("anchor %q matches no row in the %s webhook section: the claim was reworded or removed — move the anchor with it", anchor.name, matrixPath)
			continue
		default:
			t.Errorf("anchor %q matches %d rows: its row pattern no longer identifies a single claim", anchor.name, matches[anchor.name])
			continue
		}

		for _, pattern := range anchor.mirror {
			if !matchesPattern(t, pattern, mirror) {
				t.Errorf("%s states %q but the public mirror %s does not carry it (no match for %q).\nA change to the webhook egress contract moves BOTH documents: restate the claim in the mirror's own register, or declare the row matrix-only with a reason.", matrixPath, anchor.name, mirrorPath, pattern)
			}
		}
	}
}

// TestWebhookMatrixCitationsResolve reads each webhook row's citation the way
// a reviewer would: every backticked test name must be defined in a file that
// the same row links. A renamed test that left its row behind fails here.
func TestWebhookMatrixCitationsResolve(t *testing.T) {
	root := repoRoot(t)
	rows := webhookMatrixRows(t, root)

	citations := 0
	for _, row := range rows {
		citations += resolveRowCitations(t, root, truncate(row.claim), row.enforced)
	}

	if citations == 0 {
		t.Fatalf("no test citations found in the %s webhook section: the citation sweep is checking nothing", matrixPath)
	}
}

var (
	citedTestName = regexp.MustCompile("`(Test[A-Za-z0-9_]*)`")
	// A citation resolves through the row's LINK TARGET, so the path form is
	// what counts; the same basename also appears as the link's label, and
	// reading that as a path would look for it in the repository root.
	citedTestFile = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+_test\.go`)
)

// resolveRowCitations reads one row's enforcing cell the way a reviewer would:
// every backticked test name must be defined in a file that the same row links.
// It returns how many citations it checked, so a caller can refuse a sweep that
// found none. label identifies the row in failure text.
func resolveRowCitations(t *testing.T, root string, label string, enforced string) int {
	t.Helper()

	files := map[string]string{}
	for _, path := range citedTestFile.FindAllString(enforced, -1) {
		if _, done := files[path]; done {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("row %q cites %s, which does not exist: %v", label, path, err)
			continue
		}
		files[path] = string(content)
	}

	citations := 0
	for _, match := range citedTestName.FindAllStringSubmatch(enforced, -1) {
		name := match[1]
		citations++
		found := false
		for _, content := range files {
			if strings.Contains(content, "func "+name+"(") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("row %q cites %s, but no file the row links defines it (linked: %s): cite the test that can observe the claim, and move the row when the test is renamed", label, name, strings.Join(sortedKeys(files), ", "))
		}
	}
	return citations
}

// TestOperatorDocsStateTheGateAndTheCodeDefault pins the second half of the
// four-document contract. The operator documents owe the gate and the default
// it actually has — the default is read from the code that parses the
// environment variable, so flipping it there fails until every document that
// states a default has moved.
func TestOperatorDocsStateTheGateAndTheCodeDefault(t *testing.T) {
	root := repoRoot(t)
	codeDefault := gateDefaultFromCode(t, root)

	stated, contradicted := `default:? .?false`, `default:? .?true`
	posture := "off-by-default"
	if codeDefault {
		stated, contradicted = contradicted, stated
		posture = "on-by-default"
	}

	// Both policy documents state the posture in prose rather than as a
	// "default: x" pair, so they are checked against the same code default in
	// their own register.
	for _, doc := range []string{matrixPath, mirrorPath} {
		text := normalizeSpace(readDoc(t, root, doc))
		if !strings.Contains(text, gateEnv) {
			t.Errorf("%s no longer names %s: it is the gate every document in this family describes", doc, gateEnv)
			continue
		}
		if !strings.Contains(text, posture) {
			t.Errorf("%s names %s but no longer calls it %q, which is what the code does (%s default %t)", doc, gateEnv, posture, gateParseGlob, codeDefault)
		}
	}

	for _, doc := range operatorDocs {
		text := normalizeSpace(readDoc(t, root, doc))
		windows := mentionWindows(text, gateEnv, 300)
		if len(windows) == 0 {
			t.Errorf("%s does not mention %s: an operator document owes the gate and its default (it does NOT owe the address-form list)", doc, gateEnv)
			continue
		}

		agrees := false
		for _, window := range windows {
			if matchesPattern(t, stated, window) {
				agrees = true
			}
			if matchesPattern(t, contradicted, window) {
				t.Errorf("%s states a default for %s that contradicts the code (%s parses default %t):\n  …%s…", doc, gateEnv, gateParseGlob, codeDefault, truncate(window))
			}
		}
		if !agrees {
			t.Errorf("%s mentions %s but never states its default; the code parses default %t, so the document owes %q near the mention", doc, gateEnv, codeDefault, stated)
		}
	}
}

// gateDefaultFromCode reads the default out of every getEnvBool call for the
// gate and fails when they disagree — the server path and the CLI path parse
// it separately, and a default that drifts between them is its own defect.
func gateDefaultFromCode(t *testing.T, root string) bool {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(gateParseGlob)))
	if err != nil {
		t.Fatalf("glob %s: %v", gateParseGlob, err)
	}

	call := regexp.MustCompile(`getEnvBool\("` + gateEnv + `", (true|false)\)`)
	defaults := map[string]string{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, match := range call.FindAllStringSubmatch(string(content), -1) {
			defaults[match[1]] = filepath.Base(path)
		}
	}

	switch len(defaults) {
	case 1:
		for value := range defaults {
			return value == "true"
		}
	case 0:
		t.Fatalf("no getEnvBool(%q, …) call found in %s: this guard reads the documented default out of the code and now reads nothing", gateEnv, gateParseGlob)
	}
	t.Fatalf("%s is parsed with more than one default (%v): the server and CLI paths must agree before any document can state one", gateEnv, defaults)
	return false
}

// webhookMatrixRows returns the claim/enforced-by cells of the matrix's
// webhook section. It fails closed: a section that no longer parses into rows
// is a broken guard, not a passing one.
func webhookMatrixRows(t *testing.T, root string) []matrixRow {
	t.Helper()

	section := docSection(t, readDoc(t, root, matrixPath), "### "+webhookHeading)
	var rows []matrixRow
	for line := range strings.SplitSeq(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 2 {
			continue
		}
		claim, enforced := strings.TrimSpace(cells[0]), strings.TrimSpace(cells[1])
		if claim == "Claim" || strings.HasPrefix(claim, "---") {
			continue
		}
		rows = append(rows, matrixRow{claim: normalizeSpace(claim), enforced: normalizeSpace(enforced)})
	}

	// The section held 19 rows when this guard was written; the floor is a
	// tripwire for a parse that silently stops matching, not a row budget.
	if len(rows) < 15 {
		t.Fatalf("parsed %d rows from the %s section of %s, expected at least 15: the sweep is reading almost nothing", len(rows), webhookHeading, matrixPath)
	}
	return rows
}

// docSection returns the text under heading, up to the next heading of the
// same or a higher level.
func docSection(t *testing.T, doc string, heading string) string {
	t.Helper()

	start := strings.Index(doc, heading+"\n")
	if start < 0 {
		t.Fatalf("heading %q not found: this guard sweeps that section and cannot report on a section it cannot find", heading)
	}
	rest := doc[start+len(heading):]

	level := strings.Count(strings.SplitN(heading, " ", 2)[0], "#")
	for line := range strings.SplitSeq(rest, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		if depth := len(trimmed) - len(strings.TrimLeft(trimmed, "#")); depth <= level {
			if end := strings.Index(rest, "\n"+trimmed); end >= 0 {
				return rest[:end]
			}
		}
	}
	return rest
}

// mentionWindows returns the text around each occurrence of needle, so a
// default stated for one setting cannot be read as another's.
func mentionWindows(text string, needle string, radius int) []string {
	var windows []string
	for offset := 0; ; {
		index := strings.Index(text[offset:], needle)
		if index < 0 {
			return windows
		}
		index += offset
		start := max(index-radius, 0)
		end := min(index+len(needle)+radius, len(text))
		windows = append(windows, text[start:end])
		offset = index + len(needle)
	}
}

func matchesPattern(t *testing.T, pattern string, text string) bool {
	t.Helper()

	expression, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		t.Fatalf("pattern %q does not compile: %v", pattern, err)
	}
	return expression.MatchString(text)
}

func readDoc(t *testing.T, root string, rel string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(content)
}

var whitespace = regexp.MustCompile(`\s+`)

// normalizeSpace collapses runs of whitespace so a claim that wraps across
// lines in one document and not in another still matches the same pattern.
func normalizeSpace(text string) string {
	return strings.TrimSpace(whitespace.ReplaceAllString(text, " "))
}

func truncate(text string) string {
	const limit = 160
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

func sortedKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return []string{"none"}
	}
	sort.Strings(keys)
	return keys
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

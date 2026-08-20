package webhookdocs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The GDPR cross-reference table states what this codebase does about
// obligations an operator answers for. Two of its claims are the kind the code
// can contradict outright rather than drift from, and one row managed to make
// both mistakes at once: Art. 9(2)(a) said deployments "rely on
// operator-captured consent" and that the codebase "exposes no third-party
// transmission", with "no external network calls" as the control — while
// registration refuses a falsy consent field in the app, and the tree carries
// two egress paths: webhook delivery POSTs to the owner's endpoint, and the
// read-only .ics feed serves an owner's predicted days to whichever calendar
// client holds the capability token, usually a third-party service. Its cited
// test could observe none of that, so nothing failed.
//
// The three guards below read the code first and hold the document to it, in
// both directions: a build that really did drop consent enforcement, or either
// egress path, would fail these tests until the row moved with it. None of them
// pins wording — only which claim a row is making.
//
// What they deliberately do NOT do is sweep the whole table for citations the
// way the webhook section is swept. Rows here cite concern documents and
// production files as often as tests, and several long-standing rows name a
// test without linking its file; auditing that is its own change, and a guard
// that starts red is a guard that gets switched off.

const gdprHeading = "## GDPR Cross-Reference"

// The code facts these rows describe, each read out of the one file that
// decides it rather than restated here.
const (
	// deliverySitePath performs the outbound webhook request.
	deliverySitePath = "internal/services/webhook_delivery.go"
	// feedRouteSitePath mounts the .ics feed. The MOUNT is what makes the feed
	// an egress path — a handler nobody routes to serves nothing — so the route
	// table is read rather than the handler file.
	feedRouteSitePath = "internal/api/routes.go"
	// consentSiteGlob holds the registration handler. Consent is enforced in
	// the transport layer, so the whole layer is read rather than one file that
	// could be renamed around the guard.
	consentSiteGlob = "internal/api/*.go"
	// autoPeriodFillSourcePath declares the privacy-by-default setting whose
	// value Art. 25 states.
	autoPeriodFillSourcePath = "internal/models/user.go"
)

var (
	outboundRequestCall  = regexp.MustCompile(`httpClient\.Do\(`)
	feedRouteMount       = regexp.MustCompile(`app\.Get\(calendarFeedRoutePath, handler\.ServeCalendarFeed\)`)
	consentRejectionCall = regexp.MustCompile(`ParseBoolLike\(credentials\.Consent\)`)
	consentErrorSpec     = regexp.MustCompile(`authConsentRequiredErrorSpec\(\)`)
	autoPeriodFillValue  = regexp.MustCompile(`DefaultAutoPeriodFill\s*=\s*(true|false)`)
)

// egressPath is one way health-derived data can leave the instance: whether the
// code still has it, what the Art. 9 row must say when it does, and what the
// public mirror owes for it.
//
// The set is enumerated rather than exemplified. The row this guard was written
// for named ONE path and read as complete, which is how the .ics feed — whose
// usual subscriber is a third-party calendar service, and therefore the more
// consequential of the two for an Art. 9 claim — went unmentioned in the row
// that governs third-party transmission. A guard that checked "the webhook is
// named" would have passed on that wording and resisted the correction.
type egressPath struct {
	name string
	// present reports whether the shipped code still carries this path.
	present func(t *testing.T, root string) bool
	// site is the file `present` reads, for failure text.
	site string
	// row is what the Art. 9 row must say while the path exists, and must not
	// say once it is gone.
	row string
	// mirror is what docs/SECURITY_INVARIANTS.md owes for the same path.
	mirror string
}

var egressPaths = []egressPath{
	{
		name:    "webhook reminder delivery",
		present: codePerformsOutboundDelivery,
		site:    deliverySitePath,
		row:     `webhook`,
		mirror:  `owner-scoped egress`,
	},
	{
		name:    "read-only .ics calendar feed",
		present: codeServesCalendarFeed,
		site:    feedRouteSitePath,
		row:     `calendar feed|\.ics`,
		mirror:  `calendar feed`,
	},
}

// transmissionDenials are the shapes a row uses to say nothing leaves the
// instance. They are patterns rather than exact sentences because the claim is
// what matters, not its phrasing — and because the row that carried them was
// rewritten, so matching the old sentence verbatim would guard only history.
var transmissionDenials = []string{
	`no external network call`,
	`no third-party transmission`,
	`exposes no third-party`,
	`nothing leaves the instance`,
}

type gdprRow struct {
	article    string
	obligation string
	control    string
	enforced   string
}

// TestGDPRTableDoesNotDenyTransmissionTheCodePerforms is the mutually-exclusive
// half. Owner-armed egress and "no external network calls" cannot both be true
// of one tree, and an operator reading the table makes data-flow decisions from
// whichever they find first.
//
// It sweeps the egress SET, so naming one path is not enough: every path the
// code still has must appear in the row, and a path the code has dropped must
// not.
func TestGDPRTableDoesNotDenyTransmissionTheCodePerforms(t *testing.T) {
	root := repoRoot(t)
	rows := gdprRows(t, root)
	mirror := normalizeSpace(readDoc(t, root, mirrorPath))
	art9 := gdprRowByArticle(t, rows, "Art. 9")
	claim := art9.obligation + " " + art9.control

	transmits := false
	for _, path := range egressPaths {
		present := path.present(t, root)
		if present {
			transmits = true
		}

		named := matchesPattern(t, path.row, claim)
		switch {
		case present && !named:
			t.Errorf("the code still carries %s (%s) but the %s row never names it:\n  %s\nThe row is where an operator looks for what leaves the instance, and naming one path reads as naming them all.", path.name, path.site, art9.article, truncate(claim))
		case !present && named:
			t.Errorf("the %s row describes %s, which %s no longer carries:\n  %s\nA control that was removed is retracted from the table, not left standing.", art9.article, path.name, path.site, truncate(claim))
		}

		if present && !matchesPattern(t, path.mirror, mirror) {
			t.Errorf("%s does not describe %s: the two security documents must agree on what leaves the instance", mirrorPath, path.name)
		}
	}

	if !transmits {
		t.Fatalf("no egress path was found in the tree at all: this guard reads what leaves the instance out of %s and %s, and now reads nothing", deliverySitePath, feedRouteSitePath)
	}

	for _, row := range rows {
		cells := row.obligation + " " + row.control
		for _, denial := range transmissionDenials {
			if !matchesPattern(t, denial, cells) {
				continue
			}
			t.Errorf("the %s row denies outbound transmission (%q) while the tree still carries an egress path:\n  %s\nState the paths the code has — owner-armed, owner-scoped, revocable — instead of denying them.", row.article, denial, truncate(cells))
		}
	}
}

// TestGDPRConsentClaimAgreesWithTheCode is the second half of the same row.
// "Deployments rely on operator-captured consent" reads as "the application
// captures none", which understates a control this codebase has: registration
// refuses an account without it.
func TestGDPRConsentClaimAgreesWithTheCode(t *testing.T) {
	root := repoRoot(t)
	rows := gdprRows(t, root)
	enforced := codeEnforcesRegistrationConsent(t, root)

	art9 := gdprRowByArticle(t, rows, "Art. 9")
	claim := art9.obligation + " " + art9.control

	if matchesPattern(t, `rely on operator-captured consent`, claim) && enforced {
		t.Errorf("the %s row hands consent capture to the operator while the registration handler refuses a falsy consent field itself:\n  %s", art9.article, truncate(claim))
	}

	statesCapture := matchesPattern(t, `consent`, claim) && matchesPattern(t, `refuses|rejects|requires|captures`, claim)
	switch {
	case enforced && !statesCapture:
		t.Errorf("registration enforces consent in %s but the %s row does not say the application captures it:\n  %s", consentSiteGlob, art9.article, truncate(claim))
	case !enforced && statesCapture:
		t.Errorf("the %s row claims the application captures consent, which %s no longer enforces:\n  %s", art9.article, consentSiteGlob, truncate(claim))
	}

	// A claim is only as good as a test that can fail on it. The row that was
	// wrong cited the privacy-page regressions, which mention neither consent
	// nor delivery.
	if citations := resolveRowCitations(t, root, art9.article, art9.enforced); citations == 0 {
		t.Errorf("the %s row cites no test by name: both of its claims are observable, so the row owes the tests that observe them", art9.article)
	}
}

// TestGDPRPrivacyByDefaultRowStatesTheCodeDefault reads the auto-period-fill
// default out of the code and holds Art. 25 to it. The row stated "off by
// default" while every producer set it on — the same class as the Art. 9 row,
// one obligation down.
func TestGDPRPrivacyByDefaultRowStatesTheCodeDefault(t *testing.T) {
	root := repoRoot(t)
	rows := gdprRows(t, root)
	codeDefault := autoPeriodFillDefaultFromCode(t, root)

	stated, contradicted := `auto-period-fill \*\*off by default\*\*`, `auto-period-fill \*\*on by default\*\*`
	if codeDefault {
		stated, contradicted = contradicted, stated
	}

	art25 := gdprRowByArticle(t, rows, "Art. 25")
	claim := art25.obligation + " " + art25.control
	if matchesPattern(t, contradicted, claim) || !matchesPattern(t, stated, claim) {
		t.Errorf("the %s row states an auto-period-fill default that %s does not carry (models.DefaultAutoPeriodFill is %t, so the row owes %q):\n  %s", art25.article, autoPeriodFillSourcePath, codeDefault, stated, truncate(claim))
	}

	if citations := resolveRowCitations(t, root, art25.article, art25.enforced); citations == 0 {
		t.Errorf("the %s row cites no test by name: the default it states is observable at the constructors, the schema and the clear-data reset", art25.article)
	}
}

func codePerformsOutboundDelivery(t *testing.T, root string) bool {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(deliverySitePath)))
	if err != nil {
		t.Fatalf("read %s: %v — this guard reads the transmission claim out of that file and now reads nothing", deliverySitePath, err)
	}
	return outboundRequestCall.Match(content)
}

// codeServesCalendarFeed reads the MOUNT, not the handler: the .ics feed is an
// egress path because a route serves it to whoever holds the token. An
// unmounted handler transmits nothing, and a mounted one transmits to a
// subscriber this instance never sees.
func codeServesCalendarFeed(t *testing.T, root string) bool {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(feedRouteSitePath)))
	if err != nil {
		t.Fatalf("read %s: %v — this guard reads the feed's existence out of the route table and now reads nothing", feedRouteSitePath, err)
	}
	return feedRouteMount.Match(content)
}

func codeEnforcesRegistrationConsent(t *testing.T, root string) bool {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(consentSiteGlob)))
	if err != nil {
		t.Fatalf("glob %s: %v", consentSiteGlob, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no files matched %s: this guard reads the consent claim out of the transport layer and now reads nothing", consentSiteGlob)
	}

	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if consentRejectionCall.Match(content) && consentErrorSpec.Match(content) {
			return true
		}
	}
	return false
}

func autoPeriodFillDefaultFromCode(t *testing.T, root string) bool {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(autoPeriodFillSourcePath)))
	if err != nil {
		t.Fatalf("read %s: %v", autoPeriodFillSourcePath, err)
	}

	matches := autoPeriodFillValue.FindAllStringSubmatch(string(content), -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one models.DefaultAutoPeriodFill declaration in %s, found %d: the document can only state a default the code states once", autoPeriodFillSourcePath, len(matches))
	}
	return matches[0][1] == "true"
}

// gdprRows returns the four cells of every row of the cross-reference table. It
// fails closed: a table that no longer parses is a broken guard, not a passing
// one.
func gdprRows(t *testing.T, root string) []gdprRow {
	t.Helper()

	section := docSection(t, readDoc(t, root, matrixPath), gdprHeading)
	var rows []gdprRow
	for line := range strings.SplitSeq(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 4 {
			continue
		}
		article := strings.TrimSpace(cells[0])
		if article == "GDPR Article" || strings.HasPrefix(article, "---") {
			continue
		}
		rows = append(rows, gdprRow{
			article:    normalizeSpace(article),
			obligation: normalizeSpace(cells[1]),
			control:    normalizeSpace(cells[2]),
			enforced:   normalizeSpace(cells[3]),
		})
	}

	// The table held 15 rows when this guard was written; the floor is a
	// tripwire for a parse that silently stops matching, not a row budget.
	if len(rows) < 12 {
		t.Fatalf("parsed %d rows from %s of %s, expected at least 12: the sweep is reading almost nothing", len(rows), gdprHeading, matrixPath)
	}
	return rows
}

func gdprRowByArticle(t *testing.T, rows []gdprRow, article string) gdprRow {
	t.Helper()

	var found []gdprRow
	for _, row := range rows {
		if strings.HasPrefix(row.article, article) {
			found = append(found, row)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly one %s row in the %s table of %s, found %d: this guard reports on that row and cannot report on none or several", article, gdprHeading, matrixPath, len(found))
	}
	return found[0]
}

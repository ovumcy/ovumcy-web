package main

import (
	"bufio"
	"strings"
	"testing"
)

// TestTableFromReportNormalizesSortsAndSkipsOwnModule pins the three transform
// properties the committed inventory depends on: the own module's row never
// appears (the prologue already states the application's license), backslashes
// from a Windows-run go-licenses normalize to forward slashes so the committed
// block is identical whichever OS generated it, and rows sort by package path
// so regeneration is order-stable.
func TestTableFromReportNormalizesSortsAndSkipsOwnModule(t *testing.T) {
	report := strings.Join([]string{
		`github.com/zzz/last,https://example.com/zzz/LICENSE,MIT`,
		`github.com/ovumcy/ovumcy-web,https://github.com/ovumcy/ovumcy-web/blob/HEAD/LICENSE,AGPL-3.0`,
		`github.com/aaa/first/sub,https://example.com/aaa\sub\LICENSE,BSD-3-Clause`,
	}, "\n")

	table, err := tableFromReport(bufio.NewScanner(strings.NewReader(report)))
	if err != nil {
		t.Fatalf("tableFromReport: %v", err)
	}
	if strings.Contains(table, "ovumcy-web") {
		t.Fatalf("the own module's row must be skipped, got:\n%s", table)
	}
	if strings.Contains(table, `\`) {
		t.Fatalf("backslashes must normalize to forward slashes, got:\n%s", table)
	}
	first := strings.Index(table, "aaa/first")
	last := strings.Index(table, "zzz/last")
	if first < 0 || last < 0 || first > last {
		t.Fatalf("rows must sort by package path, got:\n%s", table)
	}
	if !strings.HasPrefix(table, "| Package | License | Text |\n| --- | --- | --- |\n") {
		t.Fatalf("table must carry the header rows, got:\n%s", table)
	}
}

// TestTableFromReportRefusesAnUnknownWithoutAnOverride keeps the override map
// honest in the failing direction: a dependency go-licenses cannot classify —
// in either column — must stop the generator with a message naming the
// package, never emit an "Unknown" row into a compliance document.
func TestTableFromReportRefusesAnUnknownWithoutAnOverride(t *testing.T) {
	for name, row := range map[string]string{
		"both unknown":            "example.com/mystery,Unknown,Unknown",
		"url unknown license not": "example.com/mystery,Unknown,MIT",
	} {
		if _, err := tableFromReport(bufio.NewScanner(strings.NewReader(row))); err == nil || !strings.Contains(err.Error(), "example.com/mystery") {
			t.Fatalf("%s: expected a refusal naming the package, got %v", name, err)
		}
	}
}

// TestTableFromReportRefusesANonHTTPSLink: a URL column carrying a local
// filesystem path (go-licenses falls back to the module-cache path when it
// cannot derive a remote URL) must be refused, not committed as a
// machine-local link.
func TestTableFromReportRefusesANonHTTPSLink(t *testing.T) {
	row := `example.com/local,C:\Users\someone\go\pkg\mod\example.com\local@v1.0.0\LICENSE,MIT`
	if _, err := tableFromReport(bufio.NewScanner(strings.NewReader(row))); err == nil || !strings.Contains(err.Error(), "example.com/local") {
		t.Fatalf("expected a refusal naming the package, got %v", err)
	}
}

// TestTableFromReportRefusesAMalformedLine: a row that is not three
// comma-separated fields stops the generator loudly.
func TestTableFromReportRefusesAMalformedLine(t *testing.T) {
	if _, err := tableFromReport(bufio.NewScanner(strings.NewReader("only,two"))); err == nil {
		t.Fatal("expected a refusal for a malformed line")
	}
}

// TestTableFromReportRefusesAnEmptyReport: the CI step leans on this — a
// failed go-licenses half that produces no rows must fail the check rather
// than compare an empty table.
func TestTableFromReportRefusesAnEmptyReport(t *testing.T) {
	if _, err := tableFromReport(bufio.NewScanner(strings.NewReader("\n\n"))); err == nil {
		t.Fatal("expected a refusal for an empty report")
	}
}

// TestTableFromReportAppliesARecordedOverride is the passing half: an
// override replaces the reported row unconditionally, covering both the
// unclassifiable case (mathutil) and the misclassified case (libc, which
// go-licenses reports as MIT via a notices file).
func TestTableFromReportAppliesARecordedOverride(t *testing.T) {
	table, err := tableFromReport(bufio.NewScanner(strings.NewReader(strings.Join([]string{
		"modernc.org/mathutil,Unknown,Unknown",
		"modernc.org/libc,https://gitlab.com/cznic/libc/blob/v1.73.4/LICENSE-3RD-PARTY.md,MIT",
	}, "\n"))))
	if err != nil {
		t.Fatalf("tableFromReport: %v", err)
	}
	if !strings.Contains(table, "cznic/mathutil/-/blob/v1.7.1/LICENSE") {
		t.Fatalf("expected the mathutil override to apply, got:\n%s", table)
	}
	if strings.Contains(table, "MIT") || strings.Contains(table, "LICENSE-3RD-PARTY") {
		t.Fatalf("expected the libc misclassification to be replaced, got:\n%s", table)
	}
}

// TestDiffSummaryNamesBothDirections pins the -check failure output: a row
// only in the committed block reads as stale, a row only in the fresh report
// reads as missing.
func TestDiffSummaryNamesBothDirections(t *testing.T) {
	summary := diffSummary("shared\nold-only", "shared\nnew-only")
	if !strings.Contains(summary, "missing: new-only") || !strings.Contains(summary, "stale:   old-only") {
		t.Fatalf("diffSummary must name both directions, got:\n%s", summary)
	}
	if strings.Contains(summary, "shared") {
		t.Fatalf("diffSummary must not name unchanged rows, got:\n%s", summary)
	}
}

// TestReplaceBlockSwapsOnlyTheMarkedRegion pins the surgery: everything before
// the BEGIN marker and from the END marker on survives byte for byte, and the
// old block comes back so -check can compare it.
func TestReplaceBlockSwapsOnlyTheMarkedRegion(t *testing.T) {
	document := "prologue\n" + beginMarker + "\nold rows\n" + endMarker + "\nepilogue\n"
	updated, current, err := replaceBlock(document, "new rows")
	if err != nil {
		t.Fatalf("replaceBlock: %v", err)
	}
	if current != "old rows" {
		t.Fatalf("current block = %q, want %q", current, "old rows")
	}
	want := "prologue\n" + beginMarker + "\nnew rows\n" + endMarker + "\nepilogue\n"
	if updated != want {
		t.Fatalf("updated document = %q, want %q", updated, want)
	}
}

// TestReplaceBlockRefusesAMarkerlessDocument: a file that lost its markers must
// fail loudly rather than have the generator guess where the block belongs.
func TestReplaceBlockRefusesAMarkerlessDocument(t *testing.T) {
	if _, _, err := replaceBlock("no markers here", "rows"); err == nil {
		t.Fatal("expected an error for a document without markers")
	}
}

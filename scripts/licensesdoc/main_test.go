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
// honest in the failing direction: a dependency go-licenses cannot classify
// must stop the generator with a message naming the package, never emit an
// "Unknown" row into a compliance document.
func TestTableFromReportRefusesAnUnknownWithoutAnOverride(t *testing.T) {
	_, err := tableFromReport(bufio.NewScanner(strings.NewReader("example.com/mystery,Unknown,Unknown")))
	if err == nil || !strings.Contains(err.Error(), "example.com/mystery") {
		t.Fatalf("expected a refusal naming the package, got %v", err)
	}
}

// TestTableFromReportAppliesARecordedOverride is the passing half: the one
// recorded Unknown resolves to its hand-classified license and URL.
func TestTableFromReportAppliesARecordedOverride(t *testing.T) {
	table, err := tableFromReport(bufio.NewScanner(strings.NewReader("modernc.org/mathutil,Unknown,Unknown")))
	if err != nil {
		t.Fatalf("tableFromReport: %v", err)
	}
	if !strings.Contains(table, "BSD-3-Clause") || !strings.Contains(table, "cznic/mathutil") {
		t.Fatalf("expected the mathutil override to apply, got:\n%s", table)
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

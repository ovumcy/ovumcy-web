package api

import (
	"strings"
	"testing"
)

// The Insights cycle stack draws every row against one axis, and that axis is
// capped (statsCycleRibbonMaxAxisDays, 60) so the DOM stays bounded. A cycle
// longer than the cap therefore has its band stop at the cap rather than at its
// own end: two such rows are drawn the same width whatever their lengths.
//
// The service reports that — StatsCycleRibbon.AxisTruncated for the stack,
// StatsCycleRibbonRow.Truncated for the row — and its own tests cover both
// flags. What no service test can see is whether the render path carries them:
// a flag computed correctly and then never read leaves every service test green
// while the reader sees an unannotated cut. These tests seed real period starts,
// GET /stats, and assert the presence and the absence of the two data hooks.
//
// Both directions are asserted, and both anchor on the stack's own hook first:
// a positive-only check passes just as well against a template that prints the
// notice unconditionally, and a negative-only check passes when the stack does
// not render at all.
//
// The assertions name the hooks, never the sentence: a copy edit to
// stats.cycle_stack_truncated must not redden a structural test.
func TestStatsCycleStackMarksACycleLongerThanTheAxis(t *testing.T) {
	body := statsBodyForCyclePattern(t, "stats-cycle-stack-truncated@example.com", 70, 3)

	if !strings.Contains(body, "data-stats-cycle-stack") {
		t.Fatal("expected the cycle stack to render for three completed cycles")
	}
	if !strings.Contains(body, `data-truncated="true"`) {
		t.Fatal("expected a band cut off at the axis to be marked on its row")
	}
	if !strings.Contains(body, "data-cycle-stack-truncated-notice") {
		t.Fatal("expected the stack to disclose that a cycle outran the axis")
	}
}

func TestStatsCycleStackCarriesNoTruncationMarksWhenEveryCycleFits(t *testing.T) {
	body := statsBodyForCyclePattern(t, "stats-cycle-stack-within-axis@example.com", 30, 3)

	if !strings.Contains(body, "data-stats-cycle-stack") {
		t.Fatal("expected the cycle stack to render for three completed cycles")
	}
	if strings.Contains(body, `data-truncated="true"`) {
		t.Fatal("no band is cut off when every cycle fits the axis")
	}
	if strings.Contains(body, "data-cycle-stack-truncated-notice") {
		t.Fatal("the truncation notice must not render when every cycle fits the axis")
	}
}

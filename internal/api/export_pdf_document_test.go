package api

import (
	"bytes"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestExportPDFCalendarMonthsKeepsLatestSixValidMonths(t *testing.T) {
	t.Parallel()

	days := []services.ExportPDFCalendarDay{
		{Date: "invalid"},
		{Date: "2026-01-14", HasData: true},
		{Date: "2026-02-14", HasData: true},
		{Date: "2026-03-14", HasData: true},
		{Date: "2026-04-14", HasData: true},
		{Date: "2026-05-14", HasData: true},
		{Date: "2026-06-14", HasData: true},
		{Date: "2026-07-14", HasData: true},
		{Date: "2026-08-14", HasData: true},
		{Date: "2026-08-30", IsPeriod: true, HasData: true},
	}

	months := exportPDFCalendarMonths(days)
	if len(months) != 6 {
		t.Fatalf("expected latest six months, got %d", len(months))
	}

	want := []string{
		"2026-03-01",
		"2026-04-01",
		"2026-05-01",
		"2026-06-01",
		"2026-07-01",
		"2026-08-01",
	}
	for index, month := range months {
		if got := month.Format("2006-01-02"); got != want[index] {
			t.Fatalf("expected month %d to be %s, got %s", index, want[index], got)
		}
	}
}

func TestBuildExportPDFDocumentExpandsWhenCalendarDaysArePresent(t *testing.T) {
	t.Parallel()

	baseReport := services.ExportPDFReport{
		GeneratedAt: time.Date(2026, time.April, 1, 9, 30, 0, 0, time.UTC).Format(time.RFC3339),
		Summary: services.ExportPDFSummary{
			LoggedDays:      2,
			CompletedCycles: 1,
			RangeStart:      "2026-03-01",
			RangeEnd:        "2026-03-02",
		},
		Cycles: []services.ExportPDFCycle{
			{
				StartDate:    "2026-03-01",
				EndDate:      "2026-03-28",
				CycleLength:  28,
				PeriodLength: 5,
				Entries: []services.ExportPDFCycleDay{
					{Date: "2026-03-01", CycleDay: 1, IsPeriod: true, Flow: "light"},
				},
			},
		},
	}

	withoutCalendar, err := buildExportPDFDocument(baseReport, map[string]string{})
	if err != nil {
		t.Fatalf("buildExportPDFDocument() without calendar unexpected error: %v", err)
	}

	withCalendarReport := baseReport
	withCalendarReport.CalendarDays = []services.ExportPDFCalendarDay{
		{Date: "2026-03-01", IsPeriod: true, HasData: true},
		{Date: "2026-03-02", HasData: true},
		{Date: "2026-04-01", HasData: true},
	}
	withCalendar, err := buildExportPDFDocument(withCalendarReport, map[string]string{})
	if err != nil {
		t.Fatalf("buildExportPDFDocument() with calendar unexpected error: %v", err)
	}

	if !bytes.HasPrefix(withCalendar, []byte("%PDF")) {
		t.Fatalf("expected PDF magic bytes, got %q", string(withCalendar[:4]))
	}
	if !bytes.Contains(withCalendar, []byte("Ovumcy report for doctor")) {
		t.Fatalf("expected doctor-facing title to remain visible in PDF payload")
	}
	if len(withCalendar) <= len(withoutCalendar)+1000 {
		t.Fatalf("expected calendar rendering to materially expand PDF payload, got without=%d with=%d", len(withoutCalendar), len(withCalendar))
	}
}

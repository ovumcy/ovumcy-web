package api

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ExportPDF(c *fiber.Ctx) error {
	user, from, to, spec := handler.exportUserAndRange(c)
	if spec != nil {
		return handler.respondMappedError(c, *spec)
	}

	now := time.Now().In(handler.location)
	report, err := handler.exportService.BuildPDFReport(user.ID, from, to, now, handler.location)
	if err != nil {
		spec := exportFetchLogsErrorSpec()
		handler.logSecurityError(c, "data.export", spec, securityEventField("export_format", "pdf"))
		return handler.respondMappedError(c, spec)
	}

	document, err := buildExportPDFDocument(report, currentMessages(c))
	if err != nil {
		spec := exportBuildErrorSpec()
		handler.logSecurityError(c, "data.export", spec, securityEventField("export_format", "pdf"))
		return handler.respondMappedError(c, spec)
	}

	setExportAttachmentHeaders(c, "application/pdf", buildExportFilename(now, "pdf"))
	handler.logSecurityEvent(c, "data.export", "success", securityEventField("export_format", "pdf"))
	return c.Send(document)
}

func buildExportPDFDocument(report services.ExportPDFReport, messages map[string]string) ([]byte, error) {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetTitle(exportPDFText(messages, "export.pdf.report_title", "Ovumcy report for doctor"), false)
	pdf.SetAuthor("Ovumcy", false)
	pdf.SetMargins(10, 10, 10)
	pdf.SetAutoPageBreak(true, 10)
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(0, 10, exportPDFText(messages, "export.pdf.report_title", "Ovumcy report for doctor"), "", 1, "", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	pdf.CellFormat(0, 6, fmt.Sprintf("%s: %s", exportPDFText(messages, "export.pdf.generated_at", "Generated"), report.GeneratedAt), "", 1, "", false, 0, "")
	pdf.Ln(2)

	pdf.SetFont("Helvetica", "B", 11)
	pdf.CellFormat(0, 7, exportPDFText(messages, "export.pdf.summary", "Summary"), "", 1, "", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	summaryLines := []string{
		fmt.Sprintf("%s: %d", exportPDFText(messages, "export.pdf.logged_days", "Logged days"), report.Summary.LoggedDays),
		fmt.Sprintf("%s: %d", exportPDFText(messages, "export.pdf.completed_cycles", "Completed cycles"), report.Summary.CompletedCycles),
	}
	if report.Summary.AverageCycleLength > 0 {
		summaryLines = append(summaryLines, fmt.Sprintf("%s: %.1f", exportPDFText(messages, "export.pdf.average_cycle", "Average cycle length"), report.Summary.AverageCycleLength))
	}
	if report.Summary.AveragePeriodLength > 0 {
		summaryLines = append(summaryLines, fmt.Sprintf("%s: %.1f", exportPDFText(messages, "export.pdf.average_period", "Average period length"), report.Summary.AveragePeriodLength))
	}
	if report.Summary.HasAverageMood {
		summaryLines = append(summaryLines, fmt.Sprintf("%s: %.1f / 5", exportPDFText(messages, "export.pdf.average_mood", "Average mood"), report.Summary.AverageMood))
	}
	if strings.TrimSpace(report.Summary.RangeStart) != "" && strings.TrimSpace(report.Summary.RangeEnd) != "" {
		summaryLines = append(summaryLines, fmt.Sprintf("%s: %s - %s", exportPDFText(messages, "export.pdf.range", "Range"), report.Summary.RangeStart, report.Summary.RangeEnd))
	}
	for _, line := range summaryLines {
		pdf.CellFormat(0, 5.5, line, "", 1, "", false, 0, "")
	}

	if len(report.Cycles) == 0 {
		pdf.Ln(3)
		pdf.MultiCell(0, 5.5, exportPDFText(messages, "export.pdf.no_cycles", "Not enough completed cycles to build a doctor-focused report yet."), "", "L", false)
		var output bytes.Buffer
		if err := pdf.Output(&output); err != nil {
			return nil, err
		}
		return output.Bytes(), nil
	}

	headers := []string{
		exportPDFText(messages, "export.pdf.column.date", "Date"),
		exportPDFText(messages, "export.pdf.column.cycle_day", "Cycle day"),
		exportPDFText(messages, "dashboard.period_day", "Period day"),
		exportPDFText(messages, "dashboard.flow", "Flow"),
		exportPDFText(messages, "dashboard.mood", "Mood"),
		exportPDFText(messages, "dashboard.sex", "Sex"),
		exportPDFText(messages, "dashboard.bbt", "BBT"),
		exportPDFText(messages, "dashboard.cervical_mucus", "Cervical mucus"),
		exportPDFText(messages, "dashboard.symptoms", "Symptoms"),
		exportPDFText(messages, "dashboard.notes", "Notes"),
	}
	widths := []float64{22, 18, 16, 18, 16, 24, 18, 28, 52, 65}

	for index, cycle := range report.Cycles {
		pdf.Ln(4)
		pdf.SetFont("Helvetica", "B", 11)
		title := fmt.Sprintf(
			"%s %d: %s - %s (%s %d, %s %d)",
			exportPDFText(messages, "export.pdf.cycle_heading", "Cycle"),
			index+1,
			cycle.StartDate,
			cycle.EndDate,
			exportPDFText(messages, "export.pdf.cycle_length_short", "len"),
			cycle.CycleLength,
			exportPDFText(messages, "export.pdf.period_length_short", "period"),
			cycle.PeriodLength,
		)
		pdf.CellFormat(0, 7, title, "", 1, "", false, 0, "")

		pdf.SetFont("Helvetica", "B", 8)
		for headerIndex, header := range headers {
			pdf.CellFormat(widths[headerIndex], 7, header, "1", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)

		pdf.SetFont("Helvetica", "", 8)
		for _, entry := range cycle.Entries {
			values := []string{
				entry.Date,
				fmt.Sprintf("%d", entry.CycleDay),
				exportPDFBooleanLabel(messages, entry.IsPeriod),
				exportPDFFlowLabel(messages, entry.Flow),
				exportPDFMoodLabel(entry.MoodRating),
				exportPDFSexActivityLabel(messages, entry.SexActivity),
				exportPDFBBTLabel(entry.BBT),
				exportPDFCervicalMucusLabel(messages, entry.CervicalMucus),
				exportPDFSymptomList(messages, entry.Symptoms),
				entry.Notes,
			}
			for valueIndex, value := range values {
				pdf.CellFormat(widths[valueIndex], 6.5, truncatePDFText(value, widths[valueIndex]), "1", 0, "L", false, 0, "")
			}
			pdf.Ln(-1)
		}
	}

	var output bytes.Buffer
	if err := pdf.Output(&output); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func exportPDFText(messages map[string]string, key string, fallback string) string {
	translated := translateMessage(messages, key)
	if translated == "" || translated == key {
		return fallback
	}
	return translated
}

func exportPDFBooleanLabel(messages map[string]string, value bool) string {
	if value {
		return exportPDFText(messages, "common.yes", "Yes")
	}
	return exportPDFText(messages, "common.no", "No")
}

func exportPDFFlowLabel(messages map[string]string, value string) string {
	return exportPDFText(messages, services.FlowTranslationKey(value), strings.TrimSpace(value))
}

func exportPDFMoodLabel(value int) string {
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf("%d/5", value)
}

func exportPDFSexActivityLabel(messages map[string]string, value string) string {
	return exportPDFText(messages, services.SexActivityTranslationKey(value), value)
}

func exportPDFBBTLabel(value float64) string {
	if value <= 0 {
		return ""
	}
	return fmt.Sprintf("%.2f C", value)
}

func exportPDFCervicalMucusLabel(messages map[string]string, value string) string {
	return exportPDFText(messages, services.CervicalMucusTranslationKey(value), value)
}

func exportPDFSymptomList(messages map[string]string, names []string) string {
	if len(names) == 0 {
		return ""
	}

	translated := make([]string, 0, len(names))
	for _, name := range names {
		key := services.BuiltinSymptomTranslationKey(name)
		if key == "" {
			translated = append(translated, name)
			continue
		}
		translated = append(translated, exportPDFText(messages, key, name))
	}
	return strings.Join(translated, ", ")
}

func truncatePDFText(value string, width float64) string {
	limit := int(width * 1.6)
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= limit {
		return trimmed
	}
	if limit <= 1 {
		return trimmed[:1]
	}
	return trimmed[:limit-1] + "…"
}

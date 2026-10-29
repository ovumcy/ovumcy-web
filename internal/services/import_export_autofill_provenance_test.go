package services

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Decided on WEB-282: export and import do not carry the auto-fill mark. The
// export says nothing about who wrote a day, and every imported day lands as
// the owner's own record, so un-ticking an imported anchor never clears it.
func TestExportCarriesNoAutoFillMarkAndImportLandsDaysAsOwnerWritten(t *testing.T) {
	for _, shape := range []reflect.Type{reflect.TypeFor[ExportJSONEntry](), reflect.TypeFor[ExportCSVRow]()} {
		for field := range shape.Fields() {
			if strings.Contains(strings.ToLower(field.Name), "auto") {
				t.Fatalf("%s.%s: the export must not carry auto-fill provenance (WEB-282 decision)", shape.Name(), field.Name)
			}
		}
	}

	dayService, database := newDayServiceIntegration(t)
	symptomService := NewSymptomService(db.NewRepositories(database).Symptoms)
	exportService := NewExportService(dayService, symptomService)

	source := createDayServiceTestUser(t, database, "provenance-export-source@example.com")
	filled := true
	logs := []models.DailyLog{
		{UserID: source.ID, Date: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowLight, AutoFilled: new(false), CycleFactorKeys: []string{}, SymptomIDs: []uint{}},
		{UserID: source.ID, Date: time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowLight, AutoFilled: &filled, CycleFactorKeys: []string{}, SymptomIDs: []uint{}},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create source logs: %v", err)
	}

	entries, err := exportService.BuildJSONEntries(context.Background(), source.ID, nil, nil, time.UTC)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := json.Marshal(importPayload{Entries: entries})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "auto") {
		t.Fatalf("the exported JSON names auto-fill provenance: %s", raw)
	}

	target := createDayServiceTestUser(t, database, "provenance-export-target@example.com")
	result, err := newImportServiceIntegration(t, database, symptomService).ImportJSON(context.Background(), target.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 2 {
		t.Fatalf("unexpected import result: %+v", result)
	}

	var imported []models.DailyLog
	if err := database.Where("user_id = ?", target.ID).Order("date ASC").Find(&imported).Error; err != nil {
		t.Fatalf("read imported days: %v", err)
	}
	for _, entry := range imported {
		if entry.AutoFilled == nil || *entry.AutoFilled {
			t.Fatalf("imported %s must land marked as the owner's own day, got AutoFilled=%v", entry.Date.Format("2006-01-02"), entry.AutoFilled)
		}
	}
}

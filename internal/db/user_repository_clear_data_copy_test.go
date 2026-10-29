package db

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// clearDataCopyGroups maps each item of the clear-data copy
// (settings.clear_data.resets.<group>) to the writes ClearAllDataAndResetSettings
// makes: a users column it sets, or "delete:<table>" for a table it deletes from.
var clearDataCopyGroups = map[string][]string{
	"calendar_feed": {"calendar_feed_selector", "calendar_feed_verifier_hash", "calendar_feed_verifier_mac", "calendar_feed_last_polled_on"},
	"webhook": {
		"webhook_enabled", "webhook_url", "webhook_notify_period", "webhook_notify_ovulation",
		"webhook_period_last_sent_cycle_start", "webhook_ovulation_last_sent_cycle_start",
		"webhook_last_delivered_at", "webhook_config_version", "reminder_lead_days",
	},
	"records": {"delete:daily_logs", "delete:symptom_types"},
	"cycle": {
		"cycle_length", "period_length", "luteal_phase", "auto_period_fill", "irregular_cycle",
		"unpredictable_cycle", "long_period_warning_cycle_start", "last_period_start", "age_group", "usage_goal",
	},
	"preferences": {
		"track_bbt", "temperature_unit", "track_cervical_mucus", "hide_sex_chip", "hide_cycle_factors",
		"hide_notes_field", "show_historical_phases", "week_starts_on", "shown_period_tip", "timezone",
	},
	"sessions": {"auth_session_version"},
}

// TestClearDataCopyNamesEveryColumnTheWipeWrites records every write the wipe
// issues and requires it to be exactly the union of clearDataCopyGroups, then
// requires every group to have copy in every locale and in the template. A column
// added to (or dropped from) the reset map without a matching copy change fails
// here, so the settings page cannot under- or over-name what clear-data does.
func TestClearDataCopyNamesEveryColumnTheWipeWrites(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "clear-data-copy@example.com")
	version := storedSessionVersionForTest(t, repo, user.ID)

	var written []string
	record := func(tx *gorm.DB) {
		if tx.Statement.Table != "users" {
			t.Errorf("unexpected update of table %q", tx.Statement.Table)
			return
		}
		values, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			t.Errorf("users update with a %T destination; this test can only read a column map", tx.Statement.Dest)
			return
		}
		for column := range values {
			written = append(written, column)
		}
	}
	if err := repo.database.Callback().Update().Before("gorm:update").Register("test:clear_data_copy_update", record); err != nil {
		t.Fatalf("register update callback: %v", err)
	}
	if err := repo.database.Callback().Delete().Before("gorm:delete").Register("test:clear_data_copy_delete", func(tx *gorm.DB) {
		written = append(written, "delete:"+tx.Statement.Table)
	}); err != nil {
		t.Fatalf("register delete callback: %v", err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID, version); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	var named []string
	for _, writes := range clearDataCopyGroups {
		named = append(named, writes...)
	}
	sort.Strings(named)
	sort.Strings(written)
	if !slices.Equal(written, named) {
		t.Fatalf("clear-data writes and the copy groups disagree\nwritten: %v\nnamed:   %v", written, named)
	}

	template, err := os.ReadFile(filepath.Join("..", "templates", "components", "settings_danger_zone.html"))
	if err != nil {
		t.Fatalf("read danger-zone template: %v", err)
	}
	locales, err := filepath.Glob(filepath.Join("..", "i18n", "locales", "*.json"))
	if err != nil || len(locales) == 0 {
		t.Fatalf("glob locales: %v (%d files)", err, len(locales))
	}
	// The password and the OIDC step-up variants of the card each render the list.
	if uses := strings.Count(string(template), `{{template "settings_clear_data_resets" .}}`); uses != 2 {
		t.Errorf("expected both clear-data variants to render settings_clear_data_resets, found %d uses", uses)
	}
	for group := range clearDataCopyGroups {
		key := "settings.clear_data.resets." + group
		if !strings.Contains(string(template), `"`+key+`"`) {
			t.Errorf("settings_danger_zone.html does not render %s", key)
		}
		for _, path := range locales {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var messages map[string]string
			if err := json.Unmarshal(raw, &messages); err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			if strings.TrimSpace(messages[key]) == "" {
				t.Errorf("%s has no copy for %s", filepath.Base(path), key)
			}
		}
	}
}

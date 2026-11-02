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
	"calendar_feed": {"calendar_feed_selector", "calendar_feed_verifier_hash", "calendar_feed_verifier_mac", "calendar_feed_last_polled_on", "calendar_feed_detailed_titles"},
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

// clearDataPreservedUserColumns lists every users column ClearAllDataAndResetSettings
// deliberately leaves alone, each with the one-line reason. Together with the
// columns the wipe writes it must cover the whole live users table.
var clearDataPreservedUserColumns = map[string]string{
	"id":                        "primary key of the account the wipe keeps",
	"email":                     "login identity; clear-data erases tracked data, not the account",
	"display_name":              "account profile, not tracked health data",
	"role":                      "account role; the wipe does not change who the owner is",
	"created_at":                "account creation timestamp",
	"password_hash":             "local credential; the wipe re-authenticates with it and keeps it",
	"recovery_code_hash":        "local credential kept with the account",
	"recovery_code_revealed_at": "one-time reveal mark of the kept recovery code; clearing it would re-offer the code",
	"must_change_password":      "operator-set account flag, independent of tracked data",
	"local_auth_enabled":        "sign-in method of the account",
	"totp_enabled":              "second-factor state of the account",
	"totp_secret":               "second-factor secret of the account",
	"totp_last_used_step":       "TOTP replay guard; clearing it would let a used code replay",
	"onboarding_completed":      "account onboarding status",
	"interface_language":        "interface language, an account preference rather than tracked data",
	"calendar_feed_revealed_at": "one-time reveal consumption mark (NULL is the armed value); only SaveCalendarFeedToken clears it, so a wipe that cleared it would re-arm a reveal for a sealed cookie still held",
	"calendar_feed_key_epoch":   "stamp of the key the stored token was minted under, written only by SaveCalendarFeedToken; the wipe clears the token so the stamp asserts nothing, and the next mint overwrites it",
}

// TestClearDataAccountsForEveryUsersColumn derives the users columns from the
// live schema and requires each to be either written by the wipe or listed in
// clearDataPreservedUserColumns with a reason. A health or preference column a
// later migration adds, that nobody threads through the reset map or this list,
// fails here instead of surviving clear-data unnoticed.
func TestClearDataAccountsForEveryUsersColumn(t *testing.T) {
	// The write recorder stays on SQLite (the wipe's column map is the same on
	// both engines); the live columns are derived per engine, because a
	// migration ships one file per dialect and a column only the Postgres file
	// adds is invisible to a SQLite-derived set.
	written := make(map[string]bool)
	for _, write := range clearDataWritesForTest(t) {
		if !strings.HasPrefix(write, "delete:") {
			written[write] = true
		}
	}

	forEachMigrationReplayStore(t, func(t *testing.T, store migrationReplayStore) {
		columnTypes, err := store.open(t).Migrator().ColumnTypes("users")
		requireNoErr(t, err, "column types of users")
		if len(columnTypes) == 0 {
			t.Fatal("derived no users columns from the live schema; the derivation is broken, not the schema")
		}
		live := make(map[string]bool, len(columnTypes))
		for _, column := range columnTypes {
			live[column.Name()] = true
		}

		for column := range written {
			if !live[column] {
				t.Errorf("the wipe writes %q, which is not a users column in the live %s schema", column, store.driver)
			}
		}
		for column, reason := range clearDataPreservedUserColumns {
			switch {
			case !live[column]:
				t.Errorf("clearDataPreservedUserColumns names %q, which is not a users column in the live %s schema; drop the entry", column, store.driver)
			case written[column]:
				t.Errorf("%q is listed as preserved but the wipe writes it; drop the entry", column)
			case strings.TrimSpace(reason) == "":
				t.Errorf("%q is preserved without a reason", column)
			}
		}
		var unaccounted []string
		for column := range live {
			if _, preserved := clearDataPreservedUserColumns[column]; !written[column] && !preserved {
				unaccounted = append(unaccounted, column)
			}
		}
		sort.Strings(unaccounted)
		if len(unaccounted) > 0 {
			t.Fatalf("%s users columns neither written by clear-data nor listed in clearDataPreservedUserColumns: %v\nreset the column in ClearAllDataAndResetSettings (and add its copy group), or list it as preserved with the reason", store.driver, unaccounted)
		}
	})
}

// TestClearDataCopyNamesEveryColumnTheWipeWrites records every write the wipe
// issues and requires it to be exactly the union of clearDataCopyGroups, then
// requires every group to have copy in every locale and in the template. A column
// added to (or dropped from) the reset map without a matching copy change fails
// here, so the settings page cannot under- or over-name what clear-data does.
func TestClearDataCopyNamesEveryColumnTheWipeWrites(t *testing.T) {
	written := clearDataWritesForTest(t)

	var named []string
	for _, writes := range clearDataCopyGroups {
		named = append(named, writes...)
	}
	sort.Strings(named)
	sort.Strings(written)
	if !slices.Equal(written, named) {
		t.Fatalf("clear-data writes and the copy groups disagree\nwritten: %v\nnamed:   %v", written, named)
	}

	assertClearDataCopyIsRendered(t)
}

// clearDataWritesForTest runs ClearAllDataAndResetSettings on a fresh account and
// returns every write it issued: a users column it set, or "delete:<table>" for
// a table it deleted from. An update of any other table fails the test.
func clearDataWritesForTest(t *testing.T) []string {
	t.Helper()
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
	return written
}

func assertClearDataCopyIsRendered(t *testing.T) {
	t.Helper()
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

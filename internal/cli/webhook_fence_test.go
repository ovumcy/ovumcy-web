package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestWebhookSetRefusesWithoutAConfirmedFence pins WEB-414's CLI half. Every
// `webhook set` that writes can withdraw an endpoint, and from a shell with no
// fence path the repository's own Advance records nothing, so the withdrawal
// would exist only in the database and a restore of an earlier backup would
// hand the endpoint back to notify. The command must refuse and write nothing;
// a dry run, which writes nothing, still answers.
func TestWebhookSetRefusesWithoutAConfirmedFence(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")
	fencePath := webhookTestFence(t, databasePath)
	if err := runWebhookCommandWithFence(sqliteConfig(databasePath), testWebhookSecretKey, fencePath,
		[]string{"set", owner.Email, "--enabled=true", "--url-stdin"}, strings.NewReader(testWebhookURLWithToken+"\n"), &bytes.Buffer{}); err != nil {
		t.Fatalf("arm the webhook through a confirmed fence: %v", err)
	}
	before := loadWebhookRow(t, databasePath, owner.ID)

	for _, args := range [][]string{
		{"set", owner.Email, "--clear-url"},
		{"set", owner.Email, "--enabled=false"},
		{"set", owner.Email, "--notify-period=false"},
	} {
		err := runWebhookCommandWithFence(sqliteConfig(databasePath), testWebhookSecretKey, "", args, strings.NewReader(""), &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), security.CalendarFeedFencePathEnv) || !strings.Contains(err.Error(), "Nothing was changed") {
			t.Fatalf("%v without a fence path must refuse and name the variable, got %v", args, err)
		}
		after := loadWebhookRow(t, databasePath, owner.ID)
		if after.WebhookURL != before.WebhookURL || after.WebhookEnabled != before.WebhookEnabled ||
			after.WebhookNotifyPeriod != before.WebhookNotifyPeriod || after.WebhookConfigVersion != before.WebhookConfigVersion {
			t.Fatalf("%v: a refused set must leave the row untouched", args)
		}
	}

	if err := runWebhookCommandWithFence(sqliteConfig(databasePath), testWebhookSecretKey, "",
		[]string{"set", owner.Email, "--clear-url", "--dry-run"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("a dry run writes nothing and needs no fence, got %v", err)
	}
}

// TestWebhookSetAdvancesTheServersFenceBeforeTheWithdrawal is the other half:
// with the server's own fence path the withdrawal goes through, the file holds
// a fresh token, and both halves agree on it, so the server's next boot reads
// continuity and revokes nothing on its account.
func TestWebhookSetAdvancesTheServersFenceBeforeTheWithdrawal(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")
	fencePath := webhookTestFence(t, databasePath)
	if err := runWebhookCommandWithFence(sqliteConfig(databasePath), testWebhookSecretKey, fencePath,
		[]string{"set", owner.Email, "--enabled=true", "--url-stdin"}, strings.NewReader(testWebhookURLWithToken+"\n"), &bytes.Buffer{}); err != nil {
		t.Fatalf("arm the webhook: %v", err)
	}
	tokenBefore := readFenceFileForTest(t, fencePath)

	var output bytes.Buffer
	if err := runWebhookCommandWithFence(sqliteConfig(databasePath), testWebhookSecretKey, fencePath,
		[]string{"set", owner.Email, "--clear-url"}, strings.NewReader(""), &output); err != nil {
		t.Fatalf("clear with a confirmed fence: %v", err)
	}
	if !strings.Contains(output.String(), "fence advanced") {
		t.Fatalf("expected the fence confirmation line, got %q", output.String())
	}
	if after := loadWebhookRow(t, databasePath, owner.ID); after.WebhookURL != "" || after.WebhookEnabled {
		t.Fatalf("the withdrawal should have gone through, got url %q enabled %t", after.WebhookURL, after.WebhookEnabled)
	}
	if readFenceFileForTest(t, fencePath) == tokenBefore {
		t.Fatal("the fence file must hold a fresh token after the withdrawal")
	}

	database, err := db.OpenDatabase(sqliteConfig(databasePath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	_, fence := buildRepositories(database, fencePath)
	outcome, err := fence.Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if outcome != (services.CalendarFeedRestoreFenceOutcome{}) {
		t.Fatalf("the server's next boot must read continuity, got %+v", outcome)
	}
}

// failingWebhookSaveRepository refuses the save alone, so a test can reach the
// one state no real database produces on cue: the fence confirmed and advanced,
// then the account write failing.
type failingWebhookSaveRepository struct {
	*db.UserRepository
	err error
}

func (repo failingWebhookSaveRepository) SaveWebhookSettings(context.Context, uint, models.WebhookSettingsColumns) error {
	return repo.err
}

// TestWebhookSetReportsAWriteFailureAfterTheFenceHasAlreadyAdvanced pins the
// message after the gate: the fence moved in both halves and the row did not,
// so the operator must hear both, not a bare "configure webhook" error that
// reads as "nothing happened".
func TestWebhookSetReportsAWriteFailureAfterTheFenceHasAlreadyAdvanced(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")
	database, err := db.OpenDatabase(sqliteConfig(databasePath))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	users := db.NewRepositories(database).Users
	refused := errors.New("disk I/O error")
	settings := services.NewWebhookSettingsService(failingWebhookSaveRepository{UserRepository: users, err: refused}, []byte(testWebhookSecretKey))
	service := services.NewWebhookSettingsCLIService(users, settings)

	appState := &fakeConfirmFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken}}
	anchor := &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true}
	fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

	disabled := false
	err = runWebhookSet(service, "/app/fence/calendar-feed.fence", fence,
		webhookSetOptions{email: owner.Email, enabled: &disabled}, services.WebhookSettingsPatch{Enabled: &disabled}, &bytes.Buffer{})
	if !errors.Is(err, refused) || !strings.Contains(err.Error(), "already advanced") {
		t.Fatalf("a save failing after the gate must name both the cause and the advanced fence, got %v", err)
	}
	if anchor.written == "" || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != anchor.written {
		t.Fatal("the gate must have advanced both halves before the failed save")
	}
}

func readFenceFileForTest(t *testing.T, fencePath string) string {
	t.Helper()
	content, err := os.ReadFile(fencePath)
	if err != nil {
		t.Fatalf("read fence file: %v", err)
	}
	return string(content)
}

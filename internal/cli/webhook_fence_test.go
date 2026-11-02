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

	// Every flag that changes what notify may deliver is a revoking write, so
	// each is tried on its own: a gate skipped for one patch shape (wrapped in a
	// condition on the flag it inspects) refuses the others and passes this
	// table everywhere but that row.
	revoking := []struct {
		name          string
		flag          string
		stdin         string
		startDisabled bool
	}{
		{name: "clear the endpoint", flag: "--clear-url"},
		{name: "disable", flag: "--enabled=false"},
		{name: "turn the period reminder off", flag: "--notify-period=false"},
		{name: "turn the ovulation reminder off", flag: "--notify-ovulation=false"},
		{name: "replace the endpoint", flag: "--url-stdin", stdin: "https://other.example/topic-xyz?token=tk_OTHERVALUE456\n"},
		{name: "arm a disabled webhook", flag: "--enabled=true", startDisabled: true},
		{name: "change the lead window", flag: "--reminder-lead-days=9"},
	}
	// The fence a shell can present when it cannot be confirmed: none at all,
	// and a file whose token the database marker does not hold.
	fences := []struct {
		name string
		path func(t *testing.T, databasePath string) string
	}{
		{name: "no fence path", path: func(*testing.T, string) string { return "" }},
		{name: "the file and the marker disagree", path: func(t *testing.T, databasePath string) string {
			fencePath := webhookTestFence(t, databasePath)
			if err := os.WriteFile(fencePath, []byte("a-different-generation\n"), 0o600); err != nil {
				t.Fatalf("rewrite fence file: %v", err)
			}
			return fencePath
		}},
	}

	for _, flag := range revoking {
		for _, shape := range fences {
			t.Run(flag.name+"/"+shape.name, func(t *testing.T) {
				t.Parallel()

				databasePath := createCLIWebhookDatabase(t)
				owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")
				armed := webhookTestFence(t, databasePath)
				if err := runWebhookCommandWithFence(sqliteConfig(databasePath), testWebhookSecretKey, armed,
					[]string{"set", owner.Email, "--enabled=true", "--url-stdin"}, strings.NewReader(testWebhookURLWithToken+"\n"), &bytes.Buffer{}); err != nil {
					t.Fatalf("arm the webhook through a confirmed fence: %v", err)
				}
				if flag.startDisabled {
					if err := runWebhookCommandWithFence(sqliteConfig(databasePath), testWebhookSecretKey, armed,
						[]string{"set", owner.Email, "--enabled=false"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
						t.Fatalf("disable the webhook through a confirmed fence: %v", err)
					}
				}
				fencePath := shape.path(t, databasePath)
				before := loadWebhookRow(t, databasePath, owner.ID)
				halvesBefore := readFenceHalves(t, databasePath, fencePath)

				err := runWebhookCommandWithFence(sqliteConfig(databasePath), testWebhookSecretKey, fencePath,
					[]string{"set", owner.Email, flag.flag}, strings.NewReader(flag.stdin), &bytes.Buffer{})
				if err == nil || !strings.Contains(err.Error(), "Nothing was changed") {
					t.Fatalf("%s must refuse at the gate, got %v", flag.flag, err)
				}
				if fencePath == "" && !strings.Contains(err.Error(), security.CalendarFeedFencePathEnv) {
					t.Fatalf("%s without a fence path must name the variable, got %v", flag.flag, err)
				}
				after := loadWebhookRow(t, databasePath, owner.ID)
				if after.WebhookURL != before.WebhookURL || after.WebhookEnabled != before.WebhookEnabled ||
					after.WebhookNotifyPeriod != before.WebhookNotifyPeriod || after.WebhookNotifyOvulation != before.WebhookNotifyOvulation ||
					after.ReminderLeadDays != before.ReminderLeadDays || after.WebhookConfigVersion != before.WebhookConfigVersion {
					t.Fatalf("%s: a refused set must leave the row untouched", flag.flag)
				}
				if halvesAfter := readFenceHalves(t, databasePath, fencePath); halvesAfter != halvesBefore {
					t.Fatalf("%s: a refused set must write neither fence half, before %+v after %+v", flag.flag, halvesBefore, halvesAfter)
				}
			})
		}
	}

	t.Run("a dry run needs no fence", func(t *testing.T) {
		t.Parallel()

		databasePath := createCLIWebhookDatabase(t)
		owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")
		if err := runWebhookCommandWithFence(sqliteConfig(databasePath), testWebhookSecretKey, "",
			[]string{"set", owner.Email, "--clear-url", "--dry-run"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
			t.Fatalf("a dry run writes nothing and needs no fence, got %v", err)
		}
	})
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

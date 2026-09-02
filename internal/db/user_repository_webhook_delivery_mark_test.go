package db

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// webhook_last_delivered_at (migration 039) is the only column in this schema
// that records that a delivery was ACCEPTED. Everything here defends the two
// properties that make it worth rendering: it moves only forward and only under
// the configuration that was delivered to, and it never outlives the endpoint it
// was about.

func markedDeliveryOf(t *testing.T, repo *UserRepository, userID uint) *time.Time {
	t.Helper()
	return reloadUserForWebhook(t, repo, userID).WebhookLastDeliveredAt
}

// TestMarkWebhookDeliveredStampsUnderTheClaimedEpochOnly proves the second write
// asks the same revocation question the claim did. A stamp accepted under a
// moved epoch would credit a delivery to a configuration the owner withdrew
// while the request was on the wire.
func TestMarkWebhookDeliveredStampsUnderTheClaimedEpochOnly(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-mark-epoch@example.com")
	ctx := context.Background()

	claimed := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	delivered := time.Date(2026, 4, 10, 9, 30, 0, 0, time.UTC)

	// The owner saves their settings while the request is on the wire, which
	// advances the epoch the delivering pass pinned.
	if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "ciphertext-a",
		ReminderLeadDays: models.DefaultReminderLeadDays,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}

	if err := repo.MarkWebhookDelivered(ctx, user.ID, delivered, claimed); err != nil {
		t.Fatalf("MarkWebhookDelivered under a stale epoch: %v", err)
	}
	if mark := markedDeliveryOf(t, repo, user.ID); mark != nil {
		t.Fatalf("expected a stale-epoch stamp to be discarded, got %s", mark)
	}

	current := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	if err := repo.MarkWebhookDelivered(ctx, user.ID, delivered, current); err != nil {
		t.Fatalf("MarkWebhookDelivered under the current epoch: %v", err)
	}
	mark := markedDeliveryOf(t, repo, user.ID)
	if mark == nil {
		t.Fatal("expected the stamp to be stored under the current epoch")
	}
	if !mark.Equal(delivered) {
		t.Fatalf("expected the stamp to be %s, got %s", delivered, *mark)
	}
}

// TestMarkWebhookDeliveredTouchesNoOtherOwnersRow is the isolation assertion the
// predicate's SHAPE makes necessary. The write carries a disjunction — "NULL or
// older than this stamp" — and it is scoped to one owner only while that
// disjunction stays parenthesised against the id and epoch conjuncts. Spelled as
// one flat condition it would read as "(this owner, this epoch, never delivered)
// OR (anyone whose mark is older)", and stamping one owner would walk every
// other owner's mark forward to a delivery they never received. Nothing else in
// the suite would notice: a single-row fixture satisfies both spellings.
func TestMarkWebhookDeliveredTouchesNoOtherOwnersRow(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	ctx := context.Background()
	bystander := createUserForTimezoneTest(t, repo, "wh-mark-bystander@example.com")
	deliveredTo := createUserForTimezoneTest(t, repo, "wh-mark-delivered-to@example.com")

	earlier := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	later := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)

	// The bystander carries an OLDER mark, which is what the second disjunct
	// would match, and the delivered-to owner carries none, which is what the
	// first one matches.
	if err := repo.MarkWebhookDelivered(ctx, bystander.ID, earlier, reloadUserForWebhook(t, repo, bystander.ID).WebhookConfigVersion); err != nil {
		t.Fatalf("MarkWebhookDelivered (bystander): %v", err)
	}
	if err := repo.MarkWebhookDelivered(ctx, deliveredTo.ID, later, reloadUserForWebhook(t, repo, deliveredTo.ID).WebhookConfigVersion); err != nil {
		t.Fatalf("MarkWebhookDelivered (delivered-to): %v", err)
	}

	untouched := markedDeliveryOf(t, repo, bystander.ID)
	if untouched == nil || !untouched.Equal(earlier) {
		t.Fatalf("recording a delivery for owner %d moved owner %d's mark from %s to %v", deliveredTo.ID, bystander.ID, earlier, untouched)
	}
	if got := markedDeliveryOf(t, repo, deliveredTo.ID); got == nil || !got.Equal(later) {
		t.Fatalf("expected owner %d's own mark at %s, got %v", deliveredTo.ID, later, got)
	}
}

// TestMarkWebhookDeliveredNeverMovesTheStampBackwards pins monotonicity. A
// delivery that took longer than the next one must not walk the mark back to
// its own start, or the ledger would report an older delivery as the latest.
func TestMarkWebhookDeliveredNeverMovesTheStampBackwards(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-mark-monotonic@example.com")
	ctx := context.Background()

	epoch := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	later := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	earlier := later.Add(-2 * time.Hour)

	if err := repo.MarkWebhookDelivered(ctx, user.ID, later, epoch); err != nil {
		t.Fatalf("MarkWebhookDelivered (later): %v", err)
	}
	if err := repo.MarkWebhookDelivered(ctx, user.ID, earlier, epoch); err != nil {
		t.Fatalf("MarkWebhookDelivered (earlier): %v", err)
	}

	mark := markedDeliveryOf(t, repo, user.ID)
	if mark == nil {
		t.Fatal("expected the stamp to stand")
	}
	if !mark.Equal(later) {
		t.Fatalf("expected the later stamp %s to stand, got %s", later, *mark)
	}
}

// TestMarkWebhookDeliveredLeavesTheRevocationEpochAlone is the trap migration
// 038's own prose would have led a reader into. Its writer list is about the
// DELIVERY CONFIGURATION, and this write is not one: advancing the epoch here
// would make the delivering pass lose the claim of its own second reminder, so
// an owner opted into both kinds would silently receive only the first.
func TestMarkWebhookDeliveredLeavesTheRevocationEpochAlone(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-mark-epoch-frozen@example.com")
	ctx := context.Background()

	before := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	if err := repo.MarkWebhookDelivered(ctx, user.ID, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC), before); err != nil {
		t.Fatalf("MarkWebhookDelivered: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookConfigVersion != before {
		t.Fatalf("recording a delivery moved webhook_config_version from %d to %d: the very next claim of this pass would be lost", before, after.WebhookConfigVersion)
	}
	if after.WebhookPeriodLastSentCycleStart != nil || after.WebhookOvulationLastSentCycleStart != nil {
		t.Fatal("recording a delivery moved a watermark: the mark writes exactly one column")
	}
}

// TestSaveWebhookSettingsClearsTheDeliveryMarkOnlyWhenTheDestinationChanges is
// the cleanup rule at the persistence layer, both directions. A toggle-only save
// re-encrypts the same endpoint and must keep the mark standing.
func TestSaveWebhookSettingsClearsTheDeliveryMarkOnlyWhenTheDestinationChanges(t *testing.T) {
	stamp := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)

	for _, testCase := range []struct {
		name  string
		clear bool
	}{
		{name: "a save that proved the destination unchanged keeps the mark", clear: false},
		{name: "a save that could not prove it clears the mark in the same write", clear: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := openWebhookRepoForTest(t)
			user := createUserForTimezoneTest(t, repo, "wh-mark-cleanup@example.com")
			ctx := context.Background()

			epoch := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
			if err := repo.MarkWebhookDelivered(ctx, user.ID, stamp, epoch); err != nil {
				t.Fatalf("MarkWebhookDelivered: %v", err)
			}

			if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
				Enabled:              true,
				EncryptedURL:         "ciphertext-b",
				ReminderLeadDays:     models.DefaultReminderLeadDays,
				ClearLastDeliveredAt: testCase.clear,
			}); err != nil {
				t.Fatalf("SaveWebhookSettings: %v", err)
			}

			after := reloadUserForWebhook(t, repo, user.ID)
			if after.WebhookURL != "ciphertext-b" {
				t.Fatalf("expected the save to write the endpoint, got %q", after.WebhookURL)
			}
			switch {
			case testCase.clear && after.WebhookLastDeliveredAt != nil:
				t.Fatalf("expected the mark cleared alongside the endpoint, got %s", after.WebhookLastDeliveredAt)
			case !testCase.clear && after.WebhookLastDeliveredAt == nil:
				t.Fatal("a save that left the destination alone cleared the delivery mark")
			}
		})
	}
}

// TestUpdateReminderLeadDaysLeavesTheDeliveryMarkStanding: the lead window says
// how early a reminder goes out, not where. It moves the revocation epoch — it
// is a delivery-configuration write — and still says nothing about whether the
// deliveries already recorded happened.
func TestUpdateReminderLeadDaysLeavesTheDeliveryMarkStanding(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-mark-lead-days@example.com")
	ctx := context.Background()

	epoch := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	stamp := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	if err := repo.MarkWebhookDelivered(ctx, user.ID, stamp, epoch); err != nil {
		t.Fatalf("MarkWebhookDelivered: %v", err)
	}

	if err := repo.UpdateReminderLeadDays(ctx, user.ID, 5); err != nil {
		t.Fatalf("UpdateReminderLeadDays: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookLastDeliveredAt == nil || !after.WebhookLastDeliveredAt.Equal(stamp) {
		t.Fatalf("expected the lead-days write to leave the mark at %s, got %v", stamp, after.WebhookLastDeliveredAt)
	}
	if after.WebhookConfigVersion == epoch {
		t.Fatal("the lead-days write no longer advances the revocation epoch: this test would no longer be about a write that moves the epoch without touching the mark")
	}
}

// TestClearAllDataClearsTheDeliveryMarkWithBothWatermarks: the wipe erases the
// endpoint, so the mark must not survive it. It goes with the watermarks because
// it is the same fact from the other side — nothing about this account's egress
// may outlive the account's data.
func TestClearAllDataClearsTheDeliveryMarkWithBothWatermarks(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "wh-mark-clear-data@example.com")
	ctx := context.Background()

	anchor := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.SaveWebhookSettings(ctx, user.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     "ciphertext-a",
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: models.DefaultReminderLeadDays,
	}); err != nil {
		t.Fatalf("SaveWebhookSettings: %v", err)
	}
	// The save advanced the epoch, so the claims below must present the new one.
	epoch := reloadUserForWebhook(t, repo, user.ID).WebhookConfigVersion
	for _, reminderType := range []string{models.WebhookReminderTypePeriod, models.WebhookReminderTypeOvulation} {
		claimed, err := repo.ClaimWebhookWatermark(ctx, user.ID, reminderType, anchor, nil, epoch)
		if err != nil {
			t.Fatalf("ClaimWebhookWatermark(%s): %v", reminderType, err)
		}
		if !claimed {
			t.Fatalf("expected to win the %s claim", reminderType)
		}
	}
	if err := repo.MarkWebhookDelivered(ctx, user.ID, time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC), epoch); err != nil {
		t.Fatalf("MarkWebhookDelivered: %v", err)
	}

	before := reloadUserForWebhook(t, repo, user.ID)
	if before.WebhookLastDeliveredAt == nil || before.WebhookPeriodLastSentCycleStart == nil || before.WebhookOvulationLastSentCycleStart == nil {
		t.Fatal("the fixture did not set all three columns: the wipe assertion below would pass vacuously")
	}

	if err := repo.ClearAllDataAndResetSettings(ctx, user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	after := reloadUserForWebhook(t, repo, user.ID)
	if after.WebhookLastDeliveredAt != nil {
		t.Fatalf("the wipe left a delivery mark for an endpoint it erased: %s", after.WebhookLastDeliveredAt)
	}
	if after.WebhookPeriodLastSentCycleStart != nil || after.WebhookOvulationLastSentCycleStart != nil {
		t.Fatal("the wipe left a watermark standing")
	}
}

// TestEveryWebhookURLWriterDecidesTheDeliveryMark is the completeness guard the
// cleanup rule rests on, and it is derived from the file rather than restated:
// a hand-written list of the writers would agree with itself while a new writer
// went unguarded.
//
// A mark saying "a delivery to your endpoint was accepted" is a claim about ONE
// destination. Any statement that writes webhook_url may be changing that
// destination, so each one has to decide the mark's fate in the same statement —
// keeping it, or clearing it. Silence is the failure mode: the write lands, the
// mark stays, and the ledger now describes an endpoint that is gone.
func TestEveryWebhookURLWriterDecidesTheDeliveryMark(t *testing.T) {
	const path = "user_repository.go"
	const urlColumn = `"webhook_url"`
	const markColumn = `"webhook_last_delivered_at"`

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	// Only map-literal KEYS count as writes. webhook_url also appears as a plain
	// argument in the read projections (LoadSettingsByID, ListAllForNotify), and
	// counting those would put every reader under a rule about writes.
	writers := map[string]bool{}
	decides := map[string]bool{}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.KeyValueExpr:
				key, ok := typed.Key.(*ast.BasicLit)
				if !ok || key.Kind != token.STRING {
					return true
				}
				switch key.Value {
				case urlColumn:
					writers[function.Name.Name] = true
				case markColumn:
					decides[function.Name.Name] = true
				}
			case *ast.IndexExpr:
				// A conditional clear is an index assignment onto the update map
				// rather than a map entry, so the column is named there instead.
				index, ok := typed.Index.(*ast.BasicLit)
				if ok && index.Kind == token.STRING && index.Value == markColumn {
					decides[function.Name.Name] = true
				}
			}
			return true
		})
	}

	// Anti-vacuity: the two writers this rule is about must both be found, or the
	// scan is looking at the wrong thing and would report success over an empty
	// set.
	for _, required := range []string{"SaveWebhookSettings", "ClearAllDataAndResetSettings"} {
		if !writers[required] {
			t.Fatalf("the scan did not find %s among the webhook_url writers (%v): it is not measuring what it claims", required, sortedNamesOf(writers))
		}
	}

	var undecided []string
	for name := range writers {
		if !decides[name] {
			undecided = append(undecided, name)
		}
	}
	sort.Strings(undecided)
	if len(undecided) > 0 {
		t.Fatalf("these writes change where deliveries go but never decide the fate of webhook_last_delivered_at, so the ledger would keep describing an endpoint that is gone: %v", undecided)
	}

	// The reverse direction. Only the endpoint writers may name the mark in an
	// update map: the one write that SETS a value goes through
	// MarkWebhookDelivered's single-column Update, which is not an update map and
	// carries its own tests, so anything found here is a second clearing path
	// nobody declared.
	for name := range decides {
		if writers[name] {
			continue
		}
		t.Fatalf("%s puts webhook_last_delivered_at in an update map without writing webhook_url: the mark is cleared only beside the endpoint it was about", name)
	}
}

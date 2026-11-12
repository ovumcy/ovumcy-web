package services

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestNormalizeCycleDayReminderInputRefusesInvisibleLabelCharacters(t *testing.T) {
	for name, label := range map[string]string{
		// Built from code points: a raw format character in a literal is
		// invisible in review and fails staticcheck ST1018.
		"control":           "vit\u0007amin",
		"bidi override":     "vitamin" + string(rune(0x202E)) + "nimativ",
		"zero-width joiner": "vit" + string(rune(0x200D)) + "amin",
	} {
		input := models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 9, Label: label, SendLabel: true}
		if _, err := NormalizeCycleDayReminderInput(input); !errors.Is(err, ErrCycleDayReminderInvalidLabel) {
			t.Fatalf("%s: want ErrCycleDayReminderInvalidLabel, got %v", name, err)
		}
	}

	got, err := NormalizeCycleDayReminderInput(models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 9, Label: "  витамин D  ", SendLabel: true})
	if err != nil || got.Label != "витамин D" || !got.SendLabel {
		t.Fatalf("control: a visible label is trimmed and kept, got %+v err=%v", got, err)
	}
}

func TestNormalizeCycleDayReminderInputBoundsDayHourAndLabel(t *testing.T) {
	cases := []struct {
		name  string
		input models.CycleDayReminderInput
		want  error
	}{
		{"day below range", models.CycleDayReminderInput{CycleDay: models.CycleDayReminderMinDay - 1, RemindAtHour: 9}, ErrCycleDayReminderInvalidDay},
		{"day above range", models.CycleDayReminderInput{CycleDay: models.CycleDayReminderMaxDay + 1, RemindAtHour: 9}, ErrCycleDayReminderInvalidDay},
		{"negative hour", models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: -1}, ErrCycleDayReminderInvalidHour},
		{"hour 24", models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 24}, ErrCycleDayReminderInvalidHour},
		{"label too long", models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 9, Label: strings.Repeat("я", models.CycleDayReminderLabelMaxRunes+1)}, ErrCycleDayReminderInvalidLabel},
		{"label not utf-8", models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 9, Label: "vit\xffamin"}, ErrCycleDayReminderInvalidLabel},
	}
	for _, testCase := range cases {
		if _, err := NormalizeCycleDayReminderInput(testCase.input); !errors.Is(err, testCase.want) {
			t.Fatalf("%s: want %v, got %v", testCase.name, testCase.want, err)
		}
	}

	atMax, err := NormalizeCycleDayReminderInput(models.CycleDayReminderInput{CycleDay: models.CycleDayReminderMaxDay, RemindAtHour: 23, Label: strings.Repeat("я", models.CycleDayReminderLabelMaxRunes)})
	if err != nil || atMax.CycleDay != models.CycleDayReminderMaxDay || atMax.RemindAtHour != 23 {
		t.Fatalf("the upper bounds are inclusive, got %+v err=%v", atMax, err)
	}
	unlabeled, err := NormalizeCycleDayReminderInput(models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 0, Label: "   ", SendLabel: true})
	if err != nil || unlabeled.Label != "" || unlabeled.SendLabel {
		t.Fatalf("a rule without a label cannot opt into sending one, got %+v err=%v", unlabeled, err)
	}
}

// fakeCycleDayReminderStoreUnique stands in for *db.UniqueConstraintError.
type fakeCycleDayReminderStoreUnique struct{}

func (fakeCycleDayReminderStoreUnique) Error() string            { return "unique constraint" }
func (fakeCycleDayReminderStoreUnique) UniqueConstraint() string { return "cycle_day_reminders" }

type fakeCycleDayReminderStoreCall struct {
	op     string
	userID uint
	ruleID uint
	input  models.CycleDayReminderInput
}

type fakeCycleDayReminderStore struct {
	rules []models.CycleDayReminder
	err   error
	calls []fakeCycleDayReminderStoreCall
}

func (store *fakeCycleDayReminderStore) ListByUser(_ context.Context, userID uint) ([]models.CycleDayReminder, error) {
	store.calls = append(store.calls, fakeCycleDayReminderStoreCall{op: "list", userID: userID})
	return store.rules, store.err
}

func (store *fakeCycleDayReminderStore) Create(_ context.Context, userID uint, input models.CycleDayReminderInput) error {
	store.calls = append(store.calls, fakeCycleDayReminderStoreCall{op: "create", userID: userID, input: input})
	return store.err
}

func (store *fakeCycleDayReminderStore) Update(_ context.Context, userID uint, ruleID uint, input models.CycleDayReminderInput) error {
	store.calls = append(store.calls, fakeCycleDayReminderStoreCall{op: "update", userID: userID, ruleID: ruleID, input: input})
	return store.err
}

func (store *fakeCycleDayReminderStore) Delete(_ context.Context, userID uint, ruleID uint) error {
	store.calls = append(store.calls, fakeCycleDayReminderStoreCall{op: "delete", userID: userID, ruleID: ruleID})
	return store.err
}

func TestCycleDayReminderServiceRefusesBeforeTheStore(t *testing.T) {
	ctx := context.Background()
	valid := models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 9}
	invalidDay := models.CycleDayReminderInput{CycleDay: 0, RemindAtHour: 9}
	invalidHour := models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 24}

	cases := []struct {
		name string
		call func(service *CycleDayReminderService) error
		want error
	}{
		{"list zero owner", func(service *CycleDayReminderService) error { _, err := service.List(ctx, 0); return err }, ErrCycleDayReminderOwner},
		{"create zero owner", func(service *CycleDayReminderService) error { return service.Create(ctx, 0, valid) }, ErrCycleDayReminderOwner},
		{"create invalid day", func(service *CycleDayReminderService) error { return service.Create(ctx, 7, invalidDay) }, ErrCycleDayReminderInvalidDay},
		{"update zero owner", func(service *CycleDayReminderService) error { return service.Update(ctx, 0, 1, valid) }, ErrCycleDayReminderOwner},
		{"update zero rule", func(service *CycleDayReminderService) error { return service.Update(ctx, 7, 0, valid) }, ErrCycleDayReminderNotFound},
		{"update invalid hour", func(service *CycleDayReminderService) error { return service.Update(ctx, 7, 1, invalidHour) }, ErrCycleDayReminderInvalidHour},
		{"delete zero owner", func(service *CycleDayReminderService) error { return service.Delete(ctx, 0, 1) }, ErrCycleDayReminderOwner},
		{"delete zero rule", func(service *CycleDayReminderService) error { return service.Delete(ctx, 7, 0) }, ErrCycleDayReminderNotFound},
	}
	for _, testCase := range cases {
		store := &fakeCycleDayReminderStore{}
		if err := testCase.call(NewCycleDayReminderService(store)); !errors.Is(err, testCase.want) {
			t.Fatalf("%s: want %v, got %v", testCase.name, testCase.want, err)
		}
		if len(store.calls) != 0 {
			t.Fatalf("%s: a refused call must not reach the store, got %+v", testCase.name, store.calls)
		}
	}
}

func TestCycleDayReminderServicePassesOwnerScopedWritesToTheStore(t *testing.T) {
	ctx := context.Background()
	store := &fakeCycleDayReminderStore{rules: []models.CycleDayReminder{{ID: 3, UserID: 7, CycleDay: 5, RemindAtHour: 9}}}
	service := NewCycleDayReminderService(store)

	listed, err := service.List(ctx, 7)
	if err != nil || len(listed) != 1 || listed[0].ID != 3 {
		t.Fatalf("List must return the store's rules, got %+v err=%v", listed, err)
	}
	if err := service.Create(ctx, 7, models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 9, Label: "  iron  ", SendLabel: true}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := service.Update(ctx, 7, 3, models.CycleDayReminderInput{CycleDay: 6, RemindAtHour: 10, SendLabel: true}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := service.Delete(ctx, 7, 3); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	want := []fakeCycleDayReminderStoreCall{
		{op: "list", userID: 7},
		{op: "create", userID: 7, input: models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 9, Label: "iron", SendLabel: true}},
		{op: "update", userID: 7, ruleID: 3, input: models.CycleDayReminderInput{CycleDay: 6, RemindAtHour: 10}},
		{op: "delete", userID: 7, ruleID: 3},
	}
	if len(store.calls) != len(want) {
		t.Fatalf("store calls: want %+v, got %+v", want, store.calls)
	}
	for i := range want {
		if store.calls[i] != want[i] {
			t.Fatalf("store call %d: want %+v (normalized input), got %+v", i, want[i], store.calls[i])
		}
	}
}

func TestCycleDayReminderServiceMapsStoreErrors(t *testing.T) {
	ctx := context.Background()
	valid := models.CycleDayReminderInput{CycleDay: 5, RemindAtHour: 9}
	other := errors.New("store unavailable")

	for _, testCase := range []struct {
		name     string
		storeErr error
		want     error
	}{
		{"unique constraint", fakeCycleDayReminderStoreUnique{}, ErrCycleDayReminderDuplicate},
		{"limit", ErrCycleDayReminderLimit, ErrCycleDayReminderLimit},
		{"other", other, other},
	} {
		service := NewCycleDayReminderService(&fakeCycleDayReminderStore{err: testCase.storeErr})
		if err := service.Create(ctx, 7, valid); !errors.Is(err, testCase.want) {
			t.Fatalf("Create %s: want %v, got %v", testCase.name, testCase.want, err)
		}
		if err := service.Update(ctx, 7, 3, valid); !errors.Is(err, testCase.want) {
			t.Fatalf("Update %s: want %v, got %v", testCase.name, testCase.want, err)
		}
	}
}

type stubEgressCycleDayReader struct {
	rules []models.CycleDayReminder
	err   error
}

func (stub stubEgressCycleDayReader) ListByUser(context.Context, uint) ([]models.CycleDayReminder, error) {
	return stub.rules, stub.err
}

func TestEgressLedgerCycleDayRulesAreReadableOnlyWhenLoaded(t *testing.T) {
	newService := func() *EgressLedgerService {
		return NewEgressLedgerService(
			&stubEgressWebhookDisplay{display: WebhookURLDisplay{Readability: WebhookURLAbsent}},
			&stubEgressFeedStatus{status: CalendarFeedStatus{}},
			true,
		)
	}
	ctx := context.Background()
	input := EgressLedgerInput{UserID: 7}

	if ledger := newService().BuildEgressLedger(ctx, input); ledger.CycleDay.Readable || ledger.CycleDay.Rules != nil {
		t.Fatalf("without a rules reader the ledger must not claim the rules were read, got %+v", ledger.CycleDay)
	}

	failing := newService().WithCycleDayReminders(stubEgressCycleDayReader{err: errors.New("load failed")})
	if ledger := failing.BuildEgressLedger(ctx, input); ledger.CycleDay.Readable || ledger.CycleDay.Rules != nil {
		t.Fatalf("a failed load must render unreadable rules, got %+v", ledger.CycleDay)
	}

	sentOn := models.CycleDayReminder{ID: 4, UserID: 7, CycleDay: 12, RemindAtHour: 20, Label: "iron", SendLabel: true}
	loaded := newService().WithCycleDayReminders(stubEgressCycleDayReader{rules: []models.CycleDayReminder{sentOn}})
	ledger := loaded.BuildEgressLedger(ctx, input)
	want := EgressCycleDayRule{ID: 4, CycleDay: 12, RemindAtHour: 20, Label: "iron", SendLabel: true}
	if !ledger.CycleDay.Readable || len(ledger.CycleDay.Rules) != 1 || ledger.CycleDay.Rules[0] != want {
		t.Fatalf("a loaded rule must be listed as the card shows it, got %+v", ledger.CycleDay)
	}
}

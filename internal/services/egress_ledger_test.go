package services

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// stubEgressWebhookDisplay answers a fixed readability projection.
type stubEgressWebhookDisplay struct {
	display WebhookURLDisplay
	userID  uint
}

func (stub *stubEgressWebhookDisplay) BuildWebhookURLDisplay(userID uint, _ string) WebhookURLDisplay {
	stub.userID = userID
	return stub.display
}

// stubEgressFeedStatus answers a fixed feed projection.
type stubEgressFeedStatus struct {
	status CalendarFeedStatus
	userID uint
}

func (stub *stubEgressFeedStatus) BuildFeedStatus(_ context.Context, userID uint) CalendarFeedStatus {
	stub.userID = userID
	return stub.status
}

// TestEgressLedgerFeedLoadFailureRendersUnknownRatherThanNone is the distinction
// the zero value used to erase. BuildFeedStatus returns the zero CalendarFeedStatus
// when the row cannot be read, and before Known existed that meant "no feed" —
// a claim about a row made by a read that never saw the row. An owner acting on
// it mints a second link beside a first one that still works.
func TestEgressLedgerFeedLoadFailureRendersUnknownRatherThanNone(t *testing.T) {
	t.Parallel()

	service := NewEgressLedgerService(
		&stubEgressWebhookDisplay{display: WebhookURLDisplay{Readability: WebhookURLAbsent}},
		// The zero value is exactly what BuildFeedStatus returns on a load error.
		&stubEgressFeedStatus{status: CalendarFeedStatus{}},
		true,
	)

	ledger := service.BuildEgressLedger(context.Background(), models.User{ID: 7})
	if ledger.Feed.State != EgressFeedUnknown {
		t.Fatalf("expected a load failure to render unknown, got %q", ledger.Feed.State)
	}
	if ledger.Section != EgressSectionNeedsAttention {
		t.Fatalf("expected an unreadable feed row to reach the heading, got %q", ledger.Section)
	}
}

// TestEgressLedgerFeedIsUnknownWhenTheRunningEpochCannotBeDerived covers the
// other unknown. A process that cannot derive its own key epoch has not measured
// a mismatch, and reporting "issued under a previous key" there would be a guess
// wearing a fact's clothes.
func TestEgressLedgerFeedIsUnknownWhenTheRunningEpochCannotBeDerived(t *testing.T) {
	t.Parallel()

	service := NewEgressLedgerService(
		&stubEgressWebhookDisplay{display: WebhookURLDisplay{Readability: WebhookURLAbsent}},
		&stubEgressFeedStatus{status: CalendarFeedStatus{Known: true, Configured: true, KeyEpoch: "stored-epoch"}},
		true,
	)

	ledger := service.BuildEgressLedger(context.Background(), models.User{ID: 8})
	if ledger.Feed.State != EgressFeedUnknown {
		t.Fatalf("expected unknown when the running epoch is unavailable, got %q", ledger.Feed.State)
	}
}

// TestEgressLedgerEvaluatesReadabilityBeforeEveryToggle is C4's order, stated as
// the case that fails under the natural reverse order. A row whose ciphertext no
// longer opens AND whose delivery flag is off must report the key problem: an
// implementation that asked "is delivery enabled?" first reports "delivery is
// switched off", and the broken key is never surfaced to anyone.
func TestEgressLedgerEvaluatesReadabilityBeforeEveryToggle(t *testing.T) {
	t.Parallel()

	service := NewEgressLedgerService(
		&stubEgressWebhookDisplay{display: WebhookURLDisplay{Readability: WebhookURLUnreadable}},
		&stubEgressFeedStatus{status: CalendarFeedStatus{Known: true}},
		true,
	)

	ledger := service.BuildEgressLedger(context.Background(), models.User{
		ID:                     9,
		WebhookEnabled:         false,
		WebhookNotifyPeriod:    false,
		WebhookNotifyOvulation: false,
	})
	if ledger.Webhook.State != EgressWebhookUnreadable {
		t.Fatalf("expected unreadable to outrank every toggle, got %q", ledger.Webhook.State)
	}
}

// TestEgressLedgerSuppressesTheDeliveryMarkWhereItCannotVouchForTheEndpoint is
// C6. The clearing rule fires on a SAVE, so a row that became unreadable and was
// never re-saved still holds a real mark from a real delivery under the old key.
func TestEgressLedgerSuppressesTheDeliveryMarkWhereItCannotVouchForTheEndpoint(t *testing.T) {
	t.Parallel()

	delivered := time.Date(2026, 7, 4, 6, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		readability WebhookURLReadability
		host        string
		outbound    bool
		wantState   EgressWebhookState
		wantMark    bool
	}{
		{"unreadable", WebhookURLUnreadable, "", true, EgressWebhookUnreadable, false},
		{"unusable", WebhookURLReadable, "", true, EgressWebhookUnusable, false},
		{"outbound disabled", WebhookURLReadable, "hooks.example.test", false, EgressWebhookOutboundDisabled, false},
		{"stored off", WebhookURLReadable, "hooks.example.test", true, EgressWebhookStoredOff, true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			service := NewEgressLedgerService(
				&stubEgressWebhookDisplay{display: WebhookURLDisplay{Readability: testCase.readability, Host: testCase.host}},
				&stubEgressFeedStatus{status: CalendarFeedStatus{Known: true}},
				testCase.outbound,
			)
			ledger := service.BuildEgressLedger(context.Background(), models.User{ID: 11, WebhookLastDeliveredAt: &delivered})

			if ledger.Webhook.State != testCase.wantState {
				t.Fatalf("expected state %q, got %q", testCase.wantState, ledger.Webhook.State)
			}
			if got := ledger.Webhook.LastDeliveredAt != nil; got != testCase.wantMark {
				t.Fatalf("delivery mark present=%t, want %t in state %q", got, testCase.wantMark, ledger.Webhook.State)
			}
		})
	}
}

// TestEgressLedgerReadsBothPathsForTheAuthenticatedOwner is the owner-scoping
// assertion for this seam. Both collaborators are supplied, and each is checked
// on its own: a nil one returns early and observes nothing.
func TestEgressLedgerReadsBothPathsForTheAuthenticatedOwner(t *testing.T) {
	t.Parallel()

	webhook := &stubEgressWebhookDisplay{display: WebhookURLDisplay{Readability: WebhookURLAbsent}}
	feed := &stubEgressFeedStatus{status: CalendarFeedStatus{Known: true}}
	service := NewEgressLedgerService(webhook, feed, true)

	const ownerID = uint(4242)
	service.BuildEgressLedger(context.Background(), models.User{ID: ownerID})

	if webhook.userID != ownerID {
		t.Fatalf("expected the endpoint projection scoped to owner %d, got %d", ownerID, webhook.userID)
	}
	if feed.userID != ownerID {
		t.Fatalf("expected the feed projection scoped to owner %d, got %d", ownerID, feed.userID)
	}
}

// TestWebhookPayloadInventoryMatchesTheDeliveredStruct pins the rendered list of
// what leaves against the struct that leaves. The list is what makes "nothing
// leaves" answerable about CONTENT rather than about pipes, so a field added to
// or removed from WebhookPayload must fail here rather than silently changing
// what goes out while the page keeps describing the old shape.
//
// It reads the wire names off the struct at runtime; a hand-written mirror would
// agree with the implementation by construction.
func TestWebhookPayloadInventoryMatchesTheDeliveredStruct(t *testing.T) {
	t.Parallel()

	structType := reflect.TypeOf(WebhookPayload{})
	actual := make([]string, 0, structType.NumField())
	for index := range structType.NumField() {
		tag := structType.Field(index).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			t.Fatalf("WebhookPayload field %s carries no wire name; the inventory cannot describe it", structType.Field(index).Name)
		}
		actual = append(actual, name)
	}

	declared := make([]string, 0, len(WebhookPayloadFields()))
	for _, field := range WebhookPayloadFields() {
		declared = append(declared, string(field))
	}

	sort.Strings(actual)
	sort.Strings(declared)
	if !reflect.DeepEqual(actual, declared) {
		t.Fatalf("the webhook payload inventory no longer describes what is delivered.\n  delivered: %v\n  described: %v", actual, declared)
	}
}

// TestCalendarFeedPayloadInventoryMatchesARenderedEvent does the same for the
// .ics path, against a real rendered event rather than a struct: the builder
// writes properties directly.
func TestCalendarFeedPayloadInventoryMatchesARenderedEvent(t *testing.T) {
	t.Parallel()

	rendered := string(renderCalendarFeedICS(
		[]calendarFeedEvent{{kind: "period", date: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}},
		"Predictions are estimates, not medical advice or a method of contraception.",
		time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	))

	inside := false
	actual := make([]string, 0, 8)
	for _, line := range strings.Split(rendered, "\r\n") {
		switch {
		case line == "BEGIN:VEVENT":
			inside = true
			continue
		case line == "END:VEVENT":
			inside = false
			continue
		case !inside || line == "":
			continue
		case strings.HasPrefix(line, " "):
			// A folded continuation of the property above it (RFC 5545 §3.1), not
			// a property of its own. Counting one would make the inventory depend
			// on how long the disclaimer happens to be.
			continue
		}
		// A property line is NAME[;params]:value.
		name := line
		if colon := strings.Index(name, ":"); colon >= 0 {
			name = name[:colon]
		}
		if semicolon := strings.Index(name, ";"); semicolon >= 0 {
			name = name[:semicolon]
		}
		actual = append(actual, strings.ToLower(name))
	}
	if len(actual) == 0 {
		t.Fatal("no event properties were rendered; the comparison below would then be measuring nothing")
	}

	declared := make([]string, 0, len(CalendarFeedPayloadFields()))
	for _, field := range CalendarFeedPayloadFields() {
		declared = append(declared, string(field))
	}

	sort.Strings(actual)
	sort.Strings(declared)
	if !reflect.DeepEqual(actual, declared) {
		t.Fatalf("the calendar feed inventory no longer describes what a rendered event carries.\n  rendered:  %v\n  described: %v", actual, declared)
	}
}

// TestBuildFeedStatusReportsUnknownOnALoadFailure covers the seam that produces
// the unknown state, at the place the error actually happens.
func TestBuildFeedStatusReportsUnknownOnALoadFailure(t *testing.T) {
	t.Parallel()

	repo := &stubCalendarFeedSettingsRepo{findErr: errors.New("load boom")}
	service := NewCalendarFeedSettingsService(repo, []byte("0123456789abcdef0123456789abcdef"))

	status := service.BuildFeedStatus(context.Background(), 13)
	if status.Known {
		t.Fatal("a failed read must not report a known row")
	}
	if status.Configured {
		t.Fatal("a failed read must not report a configured feed")
	}
}

// TestBuildSettingsEgressViewDataRefusesANonOwnerAndReRreadsTheRow covers the
// rebuild path a mutation's response is assembled from: the owner gate, the
// fresh read, and the typed failure.
//
// The gate is the second half of the owner boundary on this surface. The route
// refuses a non-owner and this refuses again, so an inverted condition here
// would let a mutation response carry a ledger the page itself would not render.
func TestBuildSettingsEgressViewDataRefusesANonOwnerAndReReadsTheRow(t *testing.T) {
	t.Parallel()

	webhook := &stubEgressWebhookDisplay{display: WebhookURLDisplay{Readability: WebhookURLReadable, Host: "hooks.example.test"}}
	feed := &stubEgressFeedStatus{status: CalendarFeedStatus{Known: true, Configured: true}}
	loader := &stubSettingsViewLoader{user: models.User{WebhookEnabled: true, WebhookNotifyPeriod: true, WebhookURL: "sealed"}}
	service := NewSettingsViewService(loader, nil, nil, NewEgressLedgerService(webhook, feed, true))

	stranger := &models.User{ID: 5, Role: "partner"}
	ledger, err := service.BuildSettingsEgressViewData(context.Background(), stranger)
	if err != nil {
		t.Fatalf("unexpected error for a non-owner: %v", err)
	}
	if ledger.Section != "" || ledger.Webhook.State != "" || ledger.Feed.State != "" {
		t.Fatalf("a non-owner received a ledger: %+v", ledger)
	}
	if loader.settingsUserID != 0 {
		t.Fatalf("a non-owner's request read the settings row for %d", loader.settingsUserID)
	}

	owner := &models.User{ID: 6, Role: models.RoleOwner}
	ledger, err = service.BuildSettingsEgressViewData(context.Background(), owner)
	if err != nil {
		t.Fatalf("unexpected error for the owner: %v", err)
	}
	if loader.settingsUserID != owner.ID {
		t.Fatalf("expected the rebuild to re-read the owner's row, got %d", loader.settingsUserID)
	}
	if ledger.Webhook.State != EgressWebhookArmed {
		t.Fatalf("expected the rebuild to describe the re-read row, got %q", ledger.Webhook.State)
	}
	if webhook.userID != owner.ID || feed.userID != owner.ID {
		t.Fatalf("the rebuild read another owner's paths: webhook=%d feed=%d", webhook.userID, feed.userID)
	}
}

// TestBuildSettingsEgressViewDataSurfacesALoadFailureAsATypedError proves the
// rebuild fails loudly rather than answering with an empty ledger, which the
// caller would render as "nothing is configured" over a row it never read.
func TestBuildSettingsEgressViewDataSurfacesALoadFailureAsATypedError(t *testing.T) {
	t.Parallel()

	service := NewSettingsViewService(
		&stubSettingsViewLoader{err: errors.New("load boom")},
		nil,
		nil,
		NewEgressLedgerService(
			&stubEgressWebhookDisplay{display: WebhookURLDisplay{Readability: WebhookURLAbsent}},
			&stubEgressFeedStatus{status: CalendarFeedStatus{Known: true}},
			true,
		),
	)

	_, err := service.BuildSettingsEgressViewData(context.Background(), &models.User{ID: 7, Role: models.RoleOwner})
	if !errors.Is(err, ErrSettingsViewLoadEgress) {
		t.Fatalf("expected ErrSettingsViewLoadEgress, got %v", err)
	}
}

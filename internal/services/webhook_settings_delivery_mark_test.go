package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// The delivery mark says "a delivery to your endpoint was accepted". It is a
// claim about ONE destination, so it may only survive a save that can be shown
// to have kept that destination. Persistence cannot make the call — every save
// re-encrypts under a fresh nonce, so the stored bytes always differ — which is
// why the verdict is computed here and travels with the write.

// storedWebhookURL encrypts a plaintext endpoint the way a previous save would
// have, so a test can seed a row whose ciphertext really opens.
func storedWebhookURL(t *testing.T, userID uint, plaintext string) string {
	t.Helper()
	ciphertext, err := security.EncryptField(plaintext, []byte(webhookTestSecretKey), aadForWebhookURL(userID))
	if err != nil {
		t.Fatalf("encrypt stored webhook url: %v", err)
	}
	return ciphertext
}

// TestWebhookSaveKeepsTheDeliveryMarkOnlyForAProvablyUnchangedDestination walks
// every shape of save the settings surfaces can produce and pins which of them
// may keep the mark. The keep cases are the point: a blanket clear on every
// write of webhook_url would pass a test that only checked the clear cases, and
// would erase the ledger entry every time an owner touched a checkbox.
func TestWebhookSaveKeepsTheDeliveryMarkOnlyForAProvablyUnchangedDestination(t *testing.T) {
	const userID = uint(7)
	const endpoint = "https://hooks.example.com/abc"

	for _, testCase := range []struct {
		name       string
		storedURL  func(t *testing.T) string
		loadErr    error
		update     WebhookSettingsUpdate
		wantClear  bool
		wantReason string
	}{
		{
			name:       "the same endpoint re-saved with toggles changed",
			storedURL:  func(t *testing.T) string { return storedWebhookURL(t, userID, endpoint) },
			update:     WebhookSettingsUpdate{Enabled: true, URL: endpoint, NotifyPeriod: false, NotifyOvulation: true, ReminderLeadDays: 3},
			wantClear:  false,
			wantReason: "the destination is provably the same one the mark was about",
		},
		{
			name:       "the same endpoint re-saved with delivery disabled",
			storedURL:  func(t *testing.T) string { return storedWebhookURL(t, userID, endpoint) },
			update:     WebhookSettingsUpdate{Enabled: false, URL: endpoint, ReminderLeadDays: 3},
			wantClear:  false,
			wantReason: "turning delivery off does not change where past deliveries went",
		},
		{
			name:       "a different endpoint",
			storedURL:  func(t *testing.T) string { return storedWebhookURL(t, userID, endpoint) },
			update:     WebhookSettingsUpdate{Enabled: true, URL: "https://hooks.example.com/other", ReminderLeadDays: 3},
			wantClear:  true,
			wantReason: "the mark would describe an endpoint this row no longer holds",
		},
		{
			name:       "the endpoint removed",
			storedURL:  func(t *testing.T) string { return storedWebhookURL(t, userID, endpoint) },
			update:     WebhookSettingsUpdate{Enabled: false, URL: "", ReminderLeadDays: 3},
			wantClear:  true,
			wantReason: "nothing is left for the mark to be about",
		},
		{
			name:       "a stored ciphertext that no longer opens",
			storedURL:  func(*testing.T) string { return "not-a-ciphertext-this-key-can-open" },
			update:     WebhookSettingsUpdate{Enabled: true, URL: endpoint, ReminderLeadDays: 3},
			wantClear:  true,
			wantReason: "after a key rotation the previous destination is unknowable, not merely different",
		},
		{
			name:       "a row that could not be read",
			storedURL:  func(*testing.T) string { return "" },
			loadErr:    errors.New("stub load failure"),
			update:     WebhookSettingsUpdate{Enabled: true, URL: endpoint, ReminderLeadDays: 3},
			wantClear:  true,
			wantReason: "an unprovable destination clears rather than fails the save",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			svc, repo := newWebhookServiceForTest()
			repo.loadUser = models.User{WebhookURL: testCase.storedURL(t)}
			repo.loadErr = testCase.loadErr

			if err := svc.SaveWebhookSettings(context.Background(), userID, testCase.update); err != nil {
				t.Fatalf("SaveWebhookSettings: %v", err)
			}
			if repo.saveCalls != 1 {
				t.Fatalf("expected the save to reach persistence once, got %d calls", repo.saveCalls)
			}
			if repo.savedColumns.ClearLastDeliveredAt != testCase.wantClear {
				t.Fatalf("expected ClearLastDeliveredAt=%t (%s), got %t", testCase.wantClear, testCase.wantReason, repo.savedColumns.ClearLastDeliveredAt)
			}
		})
	}
}

// TestWebhookFormSaveKeepsTheDeliveryMarkWhenTheURLFieldIsLeftBlank covers the
// shape the settings page actually submits. The URL field is write-only and
// renders blank, so the overwhelmingly common save carries no URL at all and
// means "leave the endpoint alone" — the one save that must never cost the
// owner their ledger entry.
func TestWebhookFormSaveKeepsTheDeliveryMarkWhenTheURLFieldIsLeftBlank(t *testing.T) {
	const userID = uint(7)
	const endpoint = "https://hooks.example.com/abc"

	svc, repo := newWebhookServiceForTest()
	repo.loadUser = models.User{WebhookURL: storedWebhookURL(t, userID, endpoint), ReminderLeadDays: 3}

	if err := svc.SaveWebhookSettingsFromForm(context.Background(), userID, WebhookSettingsFormUpdate{
		Enabled:      true,
		NotifyPeriod: true,
	}); err != nil {
		t.Fatalf("SaveWebhookSettingsFromForm: %v", err)
	}
	if repo.savedColumns.ClearLastDeliveredAt {
		t.Fatal("a blank-URL form save cleared the delivery mark: the endpoint it was about is unchanged")
	}
}

// TestWebhookFormRemoveClearsTheDeliveryMark is the same surface's other
// affordance. Removing the endpoint must take the mark with it, in the write
// that removes it.
func TestWebhookFormRemoveClearsTheDeliveryMark(t *testing.T) {
	const userID = uint(7)

	svc, repo := newWebhookServiceForTest()
	repo.loadUser = models.User{WebhookURL: storedWebhookURL(t, userID, "https://hooks.example.com/abc"), ReminderLeadDays: 3}

	if err := svc.SaveWebhookSettingsFromForm(context.Background(), userID, WebhookSettingsFormUpdate{RemoveURL: true}); err != nil {
		t.Fatalf("SaveWebhookSettingsFromForm: %v", err)
	}
	if repo.savedColumns.EncryptedURL != "" {
		t.Fatalf("expected the remove to clear the endpoint, got %q", repo.savedColumns.EncryptedURL)
	}
	if !repo.savedColumns.ClearLastDeliveredAt {
		t.Fatal("removing the endpoint left the delivery mark standing")
	}
}

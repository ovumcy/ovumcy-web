package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestRegenerateRecoveryCodeRejectsMissingPassword(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-regenerate-missing-pass@example.com")

	priorHash := loadUserRecoveryCodeHash(t, ctx)

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/regenerate-recovery-code", url.Values{}, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	assertStatusCode(t, response, http.StatusBadRequest)
	if got := readAPIError(t, response.Body); got != "invalid password" {
		t.Fatalf("expected error %q, got %q", "invalid password", got)
	}
	assertRecoveryCodeHashUnchanged(t, ctx, priorHash)
}

func TestRegenerateRecoveryCodeRejectsWrongPassword(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-regenerate-wrong-pass@example.com")

	priorHash := loadUserRecoveryCodeHash(t, ctx)

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/settings/regenerate-recovery-code", url.Values{
		"password": {"WrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	defer response.Body.Close()

	assertStatusCode(t, response, http.StatusUnauthorized)
	if got := readAPIError(t, response.Body); got != "invalid password" {
		t.Fatalf("expected error %q, got %q", "invalid password", got)
	}
	assertRecoveryCodeHashUnchanged(t, ctx, priorHash)
}

func loadUserRecoveryCodeHash(t *testing.T, ctx settingsSecurityTestContext) string {
	t.Helper()

	var current models.User
	if err := ctx.database.Select("recovery_code_hash").First(&current, ctx.user.ID).Error; err != nil {
		t.Fatalf("load recovery_code_hash: %v", err)
	}
	return strings.TrimSpace(current.RecoveryCodeHash)
}

func assertRecoveryCodeHashUnchanged(t *testing.T, ctx settingsSecurityTestContext, priorHash string) {
	t.Helper()

	current := loadUserRecoveryCodeHash(t, ctx)
	if current != priorHash {
		t.Fatalf("expected recovery_code_hash unchanged after rejected request")
	}
}

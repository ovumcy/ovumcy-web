package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestOwnerAccountConstructorsLeaveAutoPeriodFillOff is the account-construction
// leg of SECURITY.md's GDPR Art. 25 row ("auto-period-fill off by default").
// Auto-fill writes period days the owner never logged, so an account that
// starts with it armed holds inferred health data nobody asked for.
//
// Both constructors are asserted in one test on purpose. They are the complete
// set of producers of a new account — local registration and OIDC
// auto-provisioning — and the setting was on in BOTH of them; a guard covering
// one would have left the sign-in path that happens to be configured deciding
// whether the published default holds. The value is compared against
// models.DefaultAutoPeriodFill rather than a literal, so the constant, the gorm
// column default and the clear-data reset cannot drift apart silently.
//
// The persisted half of the claim lives in internal/db
// (TestNewAccountsCarryAutoPeriodFillOff, TestClearAllDataTurnsAutoPeriodFillOff):
// a constructor's field is only an intention until the schema stores it.
func TestOwnerAccountConstructorsLeaveAutoPeriodFillOff(t *testing.T) {
	if models.DefaultAutoPeriodFill {
		t.Fatal("models.DefaultAutoPeriodFill is on: SECURITY.md's Art. 25 row states auto-period-fill as off by default, so either the constant or the claim has to move")
	}

	service := NewAuthService(&stubAuthUserRepo{})
	createdAt := time.Date(2026, time.March, 2, 8, 0, 0, 0, time.UTC)

	local, _, err := service.BuildOwnerUserWithRecovery("owner@example.com", "StrongPass1", createdAt)
	if err != nil {
		t.Fatalf("BuildOwnerUserWithRecovery() unexpected error: %v", err)
	}
	oidc, err := service.BuildOIDCOwnerUser("owner@example.com", createdAt)
	if err != nil {
		t.Fatalf("BuildOIDCOwnerUser() unexpected error: %v", err)
	}

	for _, account := range []struct {
		constructor string
		user        models.User
	}{
		{constructor: "BuildOwnerUserWithRecovery", user: local},
		{constructor: "BuildOIDCOwnerUser", user: oidc},
	} {
		if account.user.AutoPeriodFill != models.DefaultAutoPeriodFill {
			t.Errorf("%s builds an account with auto-period-fill %t, want %t: a new owner would get inferred period days without opting in",
				account.constructor, account.user.AutoPeriodFill, models.DefaultAutoPeriodFill)
		}

		// Positive anchor: the same constructors still carry the cycle
		// defaults, so a build that returned an empty struct — every bool
		// false, including this one — cannot pass as a fix.
		if account.user.CycleLength != models.DefaultCycleLength || account.user.PeriodLength != models.DefaultPeriodLength {
			t.Errorf("%s no longer carries the cycle defaults (cycle=%d period=%d): the auto-fill assertion above would pass on an empty account",
				account.constructor, account.user.CycleLength, account.user.PeriodLength)
		}
	}
}

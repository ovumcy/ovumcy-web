package api

import (
	"errors"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestCycleDayReminderErrorMappingAnswersByCause(t *testing.T) {
	t.Parallel()

	storage := errors.New("disk full")
	cases := []struct {
		name   string
		spec   APIErrorSpec
		status int
		target APIErrorTarget
	}{
		{"malformed body", mapCycleDayReminderError(errCycleDayReminderMalformedBody), fiber.StatusBadRequest, settingsInvalidInputErrorSpec().Target},
		{"write not found", mapCycleDayReminderError(services.ErrCycleDayReminderNotFound), fiber.StatusNotFound, APIErrorTargetSettingsForm},
		{"write storage failure", mapCycleDayReminderError(storage), fiber.StatusInternalServerError, APIErrorTargetGlobal},
		{"delete not found", mapCycleDayReminderDeleteError(services.ErrCycleDayReminderNotFound), fiber.StatusNotFound, APIErrorTargetSettingsForm},
		{"delete storage failure", mapCycleDayReminderDeleteError(storage), fiber.StatusInternalServerError, APIErrorTargetGlobal},
	}
	for _, tc := range cases {
		if tc.spec.Status != tc.status || tc.spec.Target != tc.target {
			t.Errorf("%s: got status %d target %q, want %d %q", tc.name, tc.spec.Status, tc.spec.Target, tc.status, tc.target)
		}
	}
}

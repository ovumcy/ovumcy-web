package services

import (
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestNormalizeCycleDayReminderInputRefusesInvisibleLabelCharacters(t *testing.T) {
	for name, label := range map[string]string{
		"control":           "vit\u0007amin",
		"bidi override":     "vitamin‮nimativ",
		"zero-width joiner": "vit‍amin",
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

package security

import (
	"errors"
	"testing"
)

func TestNewCalendarFeedEventUIDsRefusesAMissingKeyOrAZeroOwner(t *testing.T) {
	if _, err := NewCalendarFeedEventUIDs(nil, 7); !errors.Is(err, ErrCalendarFeedEventUIDKeyMissing) {
		t.Fatalf("no secret key: err = %v", err)
	}
	if _, err := NewCalendarFeedEventUIDs([]byte("k"), 0); !errors.Is(err, ErrCalendarFeedEventUIDOwnerID) {
		t.Fatalf("zero owner id: err = %v", err)
	}
}

// TestCalendarFeedEventUIDSeparatesItsInputs: the UID is a function of exactly
// (key, owner, kind, date) — each changes it, and the kind/date boundary cannot
// be re-split into another pair.
func TestCalendarFeedEventUIDSeparatesItsInputs(t *testing.T) {
	mint := func(key string, owner uint) *CalendarFeedEventUIDs {
		t.Helper()
		uids, err := NewCalendarFeedEventUIDs([]byte(key), owner)
		if err != nil {
			t.Fatalf("derive: %v", err)
		}
		return uids
	}
	base := mint("k", 7).UID("period", "20260330")
	if again := mint("k", 7).UID("period", "20260330"); again != base {
		t.Fatalf("same input, different UID: %s vs %s", base, again)
	}
	for name, other := range map[string]string{
		"key":      mint("k2", 7).UID("period", "20260330"),
		"owner":    mint("k", 8).UID("period", "20260330"),
		"kind":     mint("k", 7).UID("ovulation", "20260330"),
		"date":     mint("k", 7).UID("period", "20260331"),
		"boundary": mint("k", 7).UID("period2", "0260330"),
	} {
		if other == base {
			t.Fatalf("changing the %s left the UID unchanged", name)
		}
	}
}

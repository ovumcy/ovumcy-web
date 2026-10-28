package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strconv"

	"golang.org/x/crypto/hkdf"
)

// HKDF salt label of the calendar-feed event-UID key. The owner id is the info,
// so each owner's UIDs come from a key of their own: two owners' feeds never
// share a UID for the same event, and one owner's UIDs say nothing about
// another's.
//
// The UID is opaque because the event kind is a four-value discriminator over a
// few hundred dates — an unkeyed hash of it is a dictionary lookup away from
// naming the cycle phase the neutral SUMMARY withholds. Changing the label, or
// rotating SECRET_KEY, gives every event a new UID, and a subscribed client
// re-creates each event once.
const calendarFeedEventUIDSaltLabel = "ovumcy.calendar-feed-uid.v1" // #nosec G101 -- public HKDF salt label, not a secret; the key material is SECRET_KEY.

// calendarFeedEventUIDHexLength keeps 128 bits of the HMAC: collision-free at
// feed scale, and short enough for every calendar client's UID handling.
const calendarFeedEventUIDHexLength = 32

var (
	ErrCalendarFeedEventUIDKeyMissing = errors.New("calendar feed event uid: secret key missing")
	ErrCalendarFeedEventUIDOwnerID    = errors.New("calendar feed event uid: owner id required")
)

// CalendarFeedEventUIDs mints one owner's event UIDs under a key derived once,
// at construction. The UID only names an event to a calendar client; it
// authorizes nothing, and the feed computes it only after the subscribe token
// has verified.
type CalendarFeedEventUIDs struct {
	key []byte
}

// NewCalendarFeedEventUIDs derives ownerID's UID key from secretKey. A missing
// key or a zero owner id is refused: the first would leave nothing to key the
// UID with, the second would key every ownerless render alike.
func NewCalendarFeedEventUIDs(secretKey []byte, ownerID uint) (*CalendarFeedEventUIDs, error) {
	if len(secretKey) == 0 {
		return nil, ErrCalendarFeedEventUIDKeyMissing
	}
	if ownerID == 0 {
		return nil, ErrCalendarFeedEventUIDOwnerID
	}
	key := make([]byte, sha256.Size)
	reader := hkdf.New(sha256.New, secretKey, []byte(calendarFeedEventUIDSaltLabel), []byte(strconv.FormatUint(uint64(ownerID), 10)))
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err // codecov:ignore -- hkdf over sha256 cannot short-read this size
	}
	return &CalendarFeedEventUIDs{key: key}, nil
}

// UID returns the opaque UID of the event of this kind starting on date
// (YYYYMMDD). It is a pure function of (owner, kind, date) under the current
// SECRET_KEY, so every poll names the same event alike.
func (uids *CalendarFeedEventUIDs) UID(kind string, date string) string {
	mac := hmac.New(sha256.New, uids.key)
	_, _ = mac.Write([]byte(kind))
	_, _ = mac.Write([]byte{0x00})
	_, _ = mac.Write([]byte(date))
	return hex.EncodeToString(mac.Sum(nil))[:calendarFeedEventUIDHexLength] + "@ovumcy"
}

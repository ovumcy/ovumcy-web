package services

// Calendar (.ics) feed subscription — settings success statuses (slice 4). Each
// is a single stable outcome key that SettingsStatusTranslationKey maps to the
// localized banner copy, mirroring SettingsWebhookUpdatedStatus.
//
// Generate/rotate reveal the secret subscribe URL on a dedicated one-time page
// (they do not flash a settings banner), so their statuses exist for symmetry
// and any JSON client; REVOKE returns to /settings with a flash, so its status
// is the one that drives the settings banner in the browser flow.
const (
	// SettingsCalendarFeedGeneratedStatus marks a fresh feed enable.
	SettingsCalendarFeedGeneratedStatus = "calendar_feed_generated"
	// SettingsCalendarFeedRotatedStatus marks a rotation (old URL now dead).
	SettingsCalendarFeedRotatedStatus = "calendar_feed_rotated"
	// SettingsCalendarFeedRevokedStatus marks a revoke (feed turned off).
	SettingsCalendarFeedRevokedStatus = "calendar_feed_revoked"
)

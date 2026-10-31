package models

// RestoreRevocationCounts reports what the restore fence revoked beside the
// calendar feeds on a boot that saw a restore (WEB-399), for the startup log.
type RestoreRevocationCounts struct {
	// Sessions counts the accounts whose auth_session_version was bumped:
	// every account, since every one of them may hold a cookie minted before
	// the backup point that a revocation after it had killed.
	Sessions int64
	// WebhookEndpoints counts the accounts whose stored webhook endpoint was
	// cleared.
	WebhookEndpoints int64
}

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
	// LocalPasswords counts the accounts whose local password was revoked
	// (WEB-408): the restored hash may be one a forced reset or a password
	// change after the backup point replaced, so it signs nobody in until an
	// operator sets a new one with `ovumcy reset-password`.
	LocalPasswords int64
}

package services

import "github.com/ovumcy/ovumcy-web/internal/models"

// SecondFactorGrantCurrent decides whether a pending second-factor grant — the
// state a first factor leaves behind while the TOTP challenge is outstanding —
// may still be completed for user.
//
// The grant carries the auth_session_version its first factor was verified at.
// Every credential or posture change (password change or reset, recovery-code
// regeneration, forced operator reset, TOTP enable/disable, session revocation)
// bumps that column, so a grant minted before any of them is refused: a correct
// code must not finish a sign-in whose password has since been changed or whose
// sessions the owner has since revoked. A grant carrying no version is refused
// rather than read as version 1.
//
// An account an operator has flagged MustChangePassword is refused as well: the
// forced-reset route outranks the second factor at sign-in
// (LoginService.Authenticate), and a grant minted before the flag was set must
// not mint a session the flag exists to withhold. The operator reset bumps the
// version too, so the flag check is the second of two gates, not the only one.
func SecondFactorGrantCurrent(grantedSessionVersion int, user *models.User) bool {
	if user == nil || grantedSessionVersion < 1 {
		return false
	}
	if user.MustChangePassword {
		return false
	}
	return AuthSessionVersionsMatch(grantedSessionVersion, user.AuthSessionVersion)
}

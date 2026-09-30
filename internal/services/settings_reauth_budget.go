package services

import (
	"errors"
	"strconv"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ReauthBudget is the attempt budget one password re-auth draws. Every re-auth
// runs through its verify step, and the budget is that step's only parameter:
// settings.reauth for the settings actions and the password change
// (SettingsService.SettingsReauthBudget) and totp.disable for the 2FA disable
// confirmation (TOTPService.DisableReauthBudget). SettingsService.VerifyReauth is
// verify around the current-password compare; ChangePassword wraps its own
// three-field compare in the same verify. The admission check and the failure
// booking are therefore written once, whatever the budget.
//
// Verifying and resetting are separate steps on purpose. Verify admits, compares
// and books a failure; it never clears the count. The caller calls Reset once the
// action the password authorised has actually happened, so a correct password
// whose write was refused proves nothing lasting and keeps the count it found.
type ReauthBudget struct {
	policy    *AuthAttemptPolicy
	secretKey []byte
	// keys names the two buckets an attempt draws: the client bucket and the
	// account identity. They are the budget's own, not the attempt's, because
	// the two budgets predate one another and key differently.
	keys func(attempt ReauthAttempt) (clientKey string, identity string)
	// limited is the refusal an exhausted budget answers, so each caller keeps
	// the rate-limit response its route already had.
	limited error
}

// SettingsReauthBudget is the settings.reauth budget: the one every password-gated
// settings action and the password change draw. It is read at call time because
// ConfigureReauthAttempts replaces the policy after construction.
func (service *SettingsService) SettingsReauthBudget() ReauthBudget {
	return ReauthBudget{
		policy:    service.reauthPolicy,
		secretKey: service.reauthSecretKey,
		keys: func(attempt ReauthAttempt) (string, string) {
			return attempt.clientBucket(), attempt.identity()
		},
		limited: ErrSettingsReauthRateLimited,
	}
}

// DisableReauthBudget is the totp.disable budget, the only one the 2FA disable
// confirmation draws. Its buckets keep the keying the route has always had: the
// client key as given and the decimal account id as the identity.
func (service *TOTPService) DisableReauthBudget(secretKey []byte) ReauthBudget {
	return ReauthBudget{
		policy:    service.disableAttemptPolicy,
		secretKey: secretKey,
		keys: func(attempt ReauthAttempt) (string, string) {
			return attempt.ClientKey, strconv.FormatUint(uint64(attempt.UserID), 10)
		},
		limited: ErrTOTPDisableRateLimited,
	}
}

func (budget ReauthBudget) exhausted(attempt ReauthAttempt) bool {
	clientKey, identity := budget.keys(attempt)
	return budget.policy.TooManyRecent(budget.secretKey, clientKey, identity, attempt.at())
}

func (budget ReauthBudget) bookFailure(attempt ReauthAttempt) {
	clientKey, identity := budget.keys(attempt)
	budget.policy.AddFailure(budget.secretKey, clientKey, identity, attempt.at())
}

// Reset clears the client and the account counters (see ResetAll): every re-auth
// runs inside a live session of this account, so the account counter holds only
// the owner's own typos. Call it after the authorised action commits, never from
// inside a verify.
func (budget ReauthBudget) Reset(attempt ReauthAttempt) {
	clientKey, identity := budget.keys(attempt)
	budget.policy.ResetAll(budget.secretKey, clientKey, identity)
}

// verify is the budgeted shape every re-auth shares: the budget is checked
// before the compare, so an exhausted budget refuses the correct password too;
// every refusal that spent a bcrypt then draws the budget.
func (budget ReauthBudget) verify(attempt ReauthAttempt, compare func() error) error {
	if budget.exhausted(attempt) {
		return budget.limited
	}
	if err := compare(); err != nil {
		if reauthRefusalSpentACompare(err) {
			budget.bookFailure(attempt)
		}
		return err
	}
	return nil
}

// reauthRefusalSpentACompare names the refusals that cost a bcrypt: a wrong
// password (ValidateCurrentPassword's and ValidatePasswordChange's sentinels)
// and the no-local-password refusal, which is equalized to a full compare and,
// left uncounted, would be CPU no budget caps. A blank submission or a
// new-password refusal is the caller's own input and spends nothing.
func reauthRefusalSpentACompare(err error) bool {
	return errors.Is(err, ErrSettingsPasswordInvalid) ||
		errors.Is(err, ErrSettingsInvalidCurrentPassword) ||
		errors.Is(err, ErrSettingsLocalPasswordNotSet)
}

// VerifyReauth is the verify step of every password re-auth: the budget check,
// the trimmed and equalized compare against the session user's own hash
// (ValidateCurrentPassword), and the failure booking. It does not reset the
// budget on success; the caller does that with budget.Reset once the action the
// password authorised has happened.
func (service *SettingsService) VerifyReauth(budget ReauthBudget, attempt ReauthAttempt, user *models.User, rawPassword string) error {
	return budget.verify(attempt, func() error {
		return service.ValidateCurrentPassword(user, rawPassword)
	})
}

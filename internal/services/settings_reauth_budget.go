package services

import (
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ReauthBudget is the attempt budget one password re-auth draws. Every re-auth
// runs through its verify step, and the budget is that step's only parameter:
// every password re-auth of an account draws the one settings.reauth policy —
// the settings actions and the password change through
// SettingsService.SettingsReauthBudget, the 2FA disable confirmation included
// (its route maps the refusal to its own rate-limit response). One policy
// object, so one bucket per account whichever route spends it: a stolen session
// gets no more guesses at the password hash than the sign-in form allows.
// SettingsService.VerifyReauth is
// verify around the current-password compare; ChangePassword wraps its own
// three-field compare in the same verify. The admission check, the failure
// booking and the bucket keys are therefore written once, whatever the budget.
// The 2FA enrollment code draws one more budget of the same shape, totp.enroll
// (TOTPService.EnrollCodeBudget, verified by TOTPService.VerifyEnrollmentCode):
// a code, not a password, but checked inside one account's session like these,
// so it is keyed and reset the same way.
//
// Verifying and resetting are separate steps on purpose. Verify reserves an
// attempt, compares, and keeps the reservation as a failure or gives it back; it
// never clears the count. The caller calls Reset once the
// action the password authorised has actually happened, so a correct password
// whose write was refused proves nothing lasting and keeps the count it found.
type ReauthBudget struct {
	policy    *AuthAttemptPolicy
	secretKey []byte
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
		limited:   ErrSettingsReauthRateLimited,
	}
}

// EnrollCodeBudget is the totp.enroll budget: the one the 2FA enrollment code
// draws. The enrollment's password is checked separately, against
// settings.reauth; this budget books only wrong codes.
func (service *TOTPService) EnrollCodeBudget(secretKey []byte) ReauthBudget {
	return ReauthBudget{
		policy:    service.enrollAttemptPolicy,
		secretKey: secretKey,
		limited:   ErrTOTPEnrollRateLimited,
	}
}

// VerifyEnrollmentCode checks a submitted enrollment code against the pending
// secret under budget. The budget is checked before the code, so an exhausted
// budget refuses the correct code too (ErrTOTPEnrollRateLimited); a code that
// does not verify books one failure and answers ErrTOTPEnrollCodeInvalid. Like
// verify it never resets: the caller calls budget.Reset once EnableTOTP has
// committed.
//
// A code that verifies returns the step it matched, found the way the sign-in
// challenge finds it (findValidatedTOTPStep, ±1 step of skew); the caller hands
// it to EnableTOTP, which records it as consumed.
func (service *TOTPService) VerifyEnrollmentCode(budget ReauthBudget, attempt ReauthAttempt, rawSecret string, code string) (TOTPEnrollmentStep, error) {
	reservation, admitted := budget.reserve(attempt)
	if !admitted {
		return TOTPEnrollmentStep{}, budget.limited
	}
	step, found := findValidatedTOTPStep(rawSecret, code, time.Now())
	if !found {
		return TOTPEnrollmentStep{}, ErrTOTPEnrollCodeInvalid
	}
	reservation.Refund()
	return TOTPEnrollmentStep{step: step}, nil
}

// keys names the two buckets an attempt draws. The client bucket carries the
// account (see ReauthAttempt.clientBucket), so accounts sharing one address
// never draw each other's budget.
func (budget ReauthBudget) keys(attempt ReauthAttempt) (clientKey string, identity string) {
	return attempt.clientBucket(), attempt.identity()
}

// reserve draws one attempt from both buckets, atomically with the admission
// check (see AttemptLimiter.Reserve): ok is false when the budget is spent, so
// no compare runs. The reservation stays booked when the compare fails and is
// refunded when it succeeds or never spent one.
func (budget ReauthBudget) reserve(attempt ReauthAttempt) (*AttemptReservation, bool) {
	clientKey, identity := budget.keys(attempt)
	return budget.policy.Reserve(budget.secretKey, clientKey, identity, attempt.at())
}

// Reset clears the client and the account counters (see ResetAll): every re-auth
// runs inside a live session of this account, so the account counter holds only
// the owner's own typos. Call it after the authorised action commits, never from
// inside a verify.
func (budget ReauthBudget) Reset(attempt ReauthAttempt) {
	clientKey, identity := budget.keys(attempt)
	budget.policy.ResetAll(budget.secretKey, clientKey, identity)
}

// verify is the budgeted shape every re-auth shares: an attempt is reserved
// before the compare, so an exhausted budget refuses the correct password too
// and no more than the budget's limit of compares can run at once; every refusal
// that spent a bcrypt keeps its reservation as the failure, and a correct
// password or a refusal that spent nothing gives it back.
func (budget ReauthBudget) verify(attempt ReauthAttempt, compare func() error) error {
	reservation, admitted := budget.reserve(attempt)
	if !admitted {
		return budget.limited
	}
	if err := compare(); err != nil {
		if !reauthRefusalSpentACompare(err) {
			reservation.Refund()
		}
		return err
	}
	reservation.Refund()
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

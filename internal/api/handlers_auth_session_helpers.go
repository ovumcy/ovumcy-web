package api

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func parseForgotPasswordInput(c fiber.Ctx) (forgotPasswordInput, string) {
	input := forgotPasswordInput{}
	if err := c.Bind().Body(&input); err != nil {
		return forgotPasswordInput{}, "invalid input"
	}
	input.Email = services.NormalizeAuthEmail(input.Email)
	if input.Email == "" {
		return forgotPasswordInput{}, "invalid input"
	}

	rawCode := strings.TrimSpace(input.RecoveryCode)
	if rawCode == "" {
		// Step 1 answers identically for every syntactically valid
		// address, so it must not read — or react to — a password.
		input.RecoveryCode = ""
		input.Password = ""
		return input, ""
	}

	if jsonBodyOmitsPassword(c) {
		return forgotPasswordInput{}, "recovery reset requires the account password"
	}

	code, err := services.NormalizeForgotPasswordCode(rawCode)
	if err != nil {
		return forgotPasswordInput{}, "invalid recovery code"
	}
	input.RecoveryCode = code
	// The password is passed through exactly as submitted — never trimmed,
	// never rejected here for being empty. An empty or wrong password is a
	// failed credential, decided in internal/services alongside the recovery
	// code so both operands share one failure spec and one timing profile.
	return input, ""
}

// jsonBodyOmitsPassword reports whether a step-2 JSON body carries no `password`
// member at all: the v1.9.2 shape of this endpoint, before the account password
// joined the recovery code as the first factor. Such a caller is a client
// written against the removed contract, not an owner failing a credential, so it
// is told which field the major version added instead of being told its recovery
// code is invalid — a refusal that would send an integrator hunting a phantom
// bad code.
//
// The question is answerable on the JSON transport only: in form encoding an
// omitted field and an empty one are the same wire fact, so the form path keeps
// the uniform credential refusal. It is decided here, before any account is
// looked up, so it reads no state and can reveal none — the enumeration-safe
// collapse below still covers every submitted-but-wrong credential
// (docs/SECURITY_INVARIANTS.md → Password recovery).
func jsonBodyOmitsPassword(c fiber.Ctx) bool {
	if !hasJSONBody(c) {
		return false
	}
	probe := struct {
		Password *string `json:"password"`
	}{}
	// codecov:ignore:start -- unreachable: the caller decoded this same body into
	// forgotPasswordInput before calling, and this probe is a strict subset of it
	// whose single member accepts everything that bind accepted, plus null. A
	// body that reaches here has already decoded once.
	if err := c.Bind().Body(&probe); err != nil {
		return false
	}
	// codecov:ignore:end
	return probe.Password == nil
}

func parseResetPasswordInput(c fiber.Ctx) (resetPasswordInput, string) {
	input := resetPasswordInput{}
	if err := c.Bind().Body(&input); err != nil {
		return resetPasswordInput{}, "invalid input"
	}

	password, confirmPassword, err := services.NormalizeResetPasswordInput(input.Password, input.ConfirmPassword)
	if err != nil {
		return resetPasswordInput{}, "invalid input"
	}
	input.Password = password
	input.ConfirmPassword = confirmPassword

	return input, ""
}

func redirectToPath(c fiber.Ctx, path string) error {
	if isHTMX(c) {
		c.Set("HX-Redirect", path)
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To(path)
}

// respondRecoveryCodeNextStep answers a committed rotation with the path that
// reveals its code: JSON clients are told where to go, browsers are sent there.
// The completion of a local-password enrollment is the one caller that answers
// with a same-origin document instead (completeLocalPasswordSetupReauth says why).
func respondRecoveryCodeNextStep(c fiber.Ctx, status int, nextPath string) error {
	if acceptsJSON(c) {
		return c.Status(status).JSON(fiber.Map{
			"ok":        true,
			"next_step": "recovery_code",
			"next_path": nextPath,
		})
	}

	return redirectToPath(c, nextPath)
}

// recoveryCodeRotationDelivery is what a recovery-code rotation hands back to
// the browser: the re-issued session and the one-time reveal. Both are sealed
// inside the rotating write's transaction, before it commits, and written only
// after it has (services.RecoveryCodeDelivery). Sealing is the part that can
// fail, so a failure there leaves the previous code and session standing
// instead of committing a code nobody is shown.
type recoveryCodeRotationDelivery struct {
	session  preparedSession
	reveal   sealedCookie
	nextPath string
	// failure is the error the hook itself returned, so the handler can tell a
	// delivery that could not be sealed from a write that was refused.
	failure error
}

// errRecoveryCodeRevealSeal marks a delivery that failed on the reveal rather
// than on the session.
var errRecoveryCodeRevealSeal = errors.New("recovery code reveal could not be sealed")

// newRecoveryCodeDelivery returns the hook a rotation seals its delivery
// through and the slot the hook fills. The slot is written to the response
// only once the rotation has returned without error.
func (handler *Handler) newRecoveryCodeDelivery(rememberMe bool, continuePath func(*models.User) string, surface string) (services.RecoveryCodeDelivery, *recoveryCodeRotationDelivery) {
	delivery := &recoveryCodeRotationDelivery{}
	deliver := func(user *models.User, recoveryCode string) error {
		session, err := handler.prepareAuthCookie(user, rememberMe)
		if err != nil {
			delivery.failure = err
			return err
		}
		reveal, err := handler.sealRecoveryCodeIssuanceCookie(user.ID, recoveryCode, continuePath(user), surface)
		if err != nil {
			delivery.failure = fmt.Errorf("%w: %v", errRecoveryCodeRevealSeal, err)
			return delivery.failure
		}
		delivery.session = session
		delivery.reveal = reveal
		delivery.nextPath = recoveryCodeSurfacePath(surface)
		return nil
	}
	return deliver, delivery
}

func recoveryCodeSurfacePath(surface string) string {
	if sanitizeRecoveryCodeSurface(surface) == recoveryCodeSurfaceInlineRegister {
		return "/register"
	}
	return "/recovery-code"
}

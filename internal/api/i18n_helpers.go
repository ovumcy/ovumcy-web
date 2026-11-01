package api

import (
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// lookupMessage answers the catalogue entry for key together with whether the
// catalogue had one. A caller that carries its own fallback reads the second
// value: translateMessage signals a miss by returning the key itself, which is
// a value indistinguishable from a legitimate translation, so re-deriving the
// miss by comparing the result to the key restates the convention at every
// call site — and half of those restatements spell the key a second time, where
// renaming it in the catalogue disables the fallback silently instead of
// breaking the build. A blank entry counts as a miss: a catalogue that carries
// the key with empty text is as unrenderable as one that lacks it.
func lookupMessage(messages map[string]string, key string) (string, bool) {
	if key == "" || messages == nil {
		return "", false
	}
	value, ok := messages[key]
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

// lookupErrorMessage is lookupMessage for a refusal's catalogue key, with the
// day-observation refusals rendered through lookupObservationBoundMessage.
func lookupErrorMessage(c fiber.Ctx, key string) (string, bool) {
	messages := currentMessages(c)
	if value, ok := lookupObservationBoundMessage(messages, currentLanguage(c), key); ok {
		return value, true
	}
	return lookupMessage(messages, key)
}

// The sentences that name the day-observation bound. Each is a plural family
// whose one %d is services.ManualCycleStartFutureDays, so the copy follows the
// constant and the noun agrees with it.
const (
	observationBoundCycleStartKey    = "dashboard.error.invalid_cycle_start_date"
	observationBoundPregnancyTestKey = "dashboard.error.invalid_pregnancy_test_date"
	observationBoundImportKey        = "settings.import_future_day"
)

// lookupObservationBoundMessage answers false for any other key. The switch
// spells the families at the Sprintf site, where the locale format barrier
// (locale_format_verb_parity_test.go) reads them.
func lookupObservationBoundMessage(messages map[string]string, language string, key string) (string, bool) {
	var family string
	switch key {
	case observationBoundCycleStartKey:
		family = observationBoundCycleStartKey
	case observationBoundPregnancyTestKey:
		family = observationBoundPregnancyTestKey
	case observationBoundImportKey:
		family = observationBoundImportKey
	default:
		return "", false
	}
	days := services.ManualCycleStartFutureDays()
	pattern := i18n.TranslatePlural(messages, language, family, days)
	if pattern == family || strings.TrimSpace(pattern) == "" {
		return "", false
	}
	return fmt.Sprintf(pattern, days), true
}

// translateMessage is the fallback-free form: a miss renders as the key, which
// is what a template wants when no better text exists. Anything that would
// rather substitute its own text calls lookupMessage.
func translateMessage(messages map[string]string, key string) string {
	if value, ok := lookupMessage(messages, key); ok {
		return value
	}
	return key
}

func currentLanguage(c fiber.Ctx) string {
	language, ok := c.Locals(contextLanguageKey).(string)
	if !ok || strings.TrimSpace(language) == "" {
		return ""
	}
	return language
}

func currentMessages(c fiber.Ctx) map[string]string {
	messages, ok := c.Locals(contextMessagesKey).(map[string]string)
	if !ok || messages == nil {
		return map[string]string{}
	}
	return messages
}

func (handler *Handler) withTemplateDefaults(c fiber.Ctx, data fiber.Map) fiber.Map {
	if data == nil {
		data = fiber.Map{}
	}

	messages := currentMessages(c)
	language := currentLanguage(c)
	if language == "" {
		language = handler.i18n.DefaultLanguage()
	}
	currentPath := currentPathWithQuery(c)
	supportedLanguages := handler.i18n.SupportedLanguages()

	if existingMessages, ok := data["Messages"].(map[string]string); ok && existingMessages != nil {
		messages = existingMessages
	} else {
		data["Messages"] = messages
	}

	if existingLanguage, ok := data["Lang"].(string); ok && strings.TrimSpace(existingLanguage) != "" {
		language = existingLanguage
	} else {
		data["Lang"] = language
	}

	if existingPath, ok := data["CurrentPath"].(string); !ok || strings.TrimSpace(existingPath) == "" {
		data["CurrentPath"] = currentPath
	}

	if _, ok := data["SupportedLanguageCodes"]; !ok {
		data["SupportedLanguageCodes"] = supportedLanguages
	}

	if _, ok := data["LanguageOptions"]; !ok {
		data["LanguageOptions"] = buildLanguageSwitchOptions(messages, language, supportedLanguages)
	}

	if _, ok := data["CSRFToken"]; !ok {
		data["CSRFToken"] = csrfToken(c)
	}

	// Every page and fragment rendered for a signed-in account names it: the
	// settings and onboarding forms carry the binding as a hidden field, and the
	// layout hands it to the one script that writes without a form. A page that
	// already set its own (the day forms compute theirs) keeps it.
	if _, ok := data["DayFormAccount"]; !ok {
		data["DayFormAccount"] = handler.renderedAccountBinding(c, data)
	}

	// The layout keeps the timezone sync off pages the onboarding gate would
	// refuse it from; the same predicate as the gate keeps the two in step.
	if _, ok := data["OnboardingPending"]; !ok {
		user, _ := data["CurrentUser"].(*models.User)
		data["OnboardingPending"] = services.RequiresOnboarding(user)
	}

	if _, ok := data["AssetVersion"]; !ok {
		data["AssetVersion"] = handler.assetVersion
	}

	if _, ok := data["NoDataLabel"]; !ok {
		noData, translated := lookupMessage(messages, "common.not_available")
		if !translated {
			noData = "-"
		}
		data["NoDataLabel"] = noData
	}

	return data
}

// currentPathWithQuery is the address the shared layout renders back into the
// page — the language switcher's `next` field and the outgoing privacy link —
// so the caller-supplied query is filtered down to the parameters the pages
// actually read before it can reach the markup.
func currentPathWithQuery(c fiber.Ctx) string {
	path := services.SanitizeCurrentPathQuery(string(c.Request().URI().RequestURI()))
	if path == "" {
		return c.Path()
	}
	return path
}

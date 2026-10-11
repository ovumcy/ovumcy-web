package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
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

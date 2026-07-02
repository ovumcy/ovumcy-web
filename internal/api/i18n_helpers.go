package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

func translateMessage(messages map[string]string, key string) string {
	if key == "" {
		return ""
	}
	if messages != nil {
		if value, ok := messages[key]; ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return key
}

func currentLanguage(c *fiber.Ctx) string {
	language, ok := c.Locals(contextLanguageKey).(string)
	if !ok || strings.TrimSpace(language) == "" {
		return ""
	}
	return language
}

func currentMessages(c *fiber.Ctx) map[string]string {
	messages, ok := c.Locals(contextMessagesKey).(map[string]string)
	if !ok || messages == nil {
		return map[string]string{}
	}
	return messages
}

func (handler *Handler) withTemplateDefaults(c *fiber.Ctx, data fiber.Map) fiber.Map {
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
		noData := translateMessage(messages, "common.not_available")
		if noData == "common.not_available" {
			noData = "-"
		}
		data["NoDataLabel"] = noData
	}

	return data
}

func currentPathWithQuery(c *fiber.Ctx) string {
	path := string(c.Request().URI().RequestURI())
	if path == "" {
		return c.Path()
	}
	return path
}

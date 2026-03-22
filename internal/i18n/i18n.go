package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	LangDE = "de"
	LangRU = "ru"
	LangEN = "en"
	LangES = "es"
	LangFR = "fr"
)

type Manager struct {
	defaultLanguage string
	locales         map[string]map[string]string
	supported       []string
}

func NewManager(defaultLanguage string, localesDir string) (*Manager, error) {
	manager := &Manager{
		locales: map[string]map[string]string{},
	}

	entries, err := os.ReadDir(localesDir)
	if err != nil {
		return nil, fmt.Errorf("read locales dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		language := strings.TrimSuffix(strings.ToLower(entry.Name()), filepath.Ext(entry.Name()))
		path := filepath.Join(localesDir, entry.Name())
		// #nosec G304 -- localesDir is application-controlled and entry.Name() comes from os.ReadDir(localesDir).
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read locale %s: %w", language, err)
		}

		messages := map[string]string{}
		if err := json.Unmarshal(content, &messages); err != nil {
			return nil, fmt.Errorf("parse locale %s: %w", language, err)
		}
		if len(messages) == 0 {
			return nil, fmt.Errorf("locale %s is empty", language)
		}

		manager.locales[language] = messages
		manager.supported = append(manager.supported, language)
	}

	if len(manager.supported) == 0 {
		return nil, fmt.Errorf("no locales found in %s", localesDir)
	}

	for _, language := range []string{LangDE, LangEN, LangES, LangRU, LangFR} {
		if _, ok := manager.locales[language]; !ok {
			return nil, fmt.Errorf("required locale %q missing", language)
		}
	}

	sort.Strings(manager.supported)
	manager.defaultLanguage = manager.NormalizeLanguage(defaultLanguage)
	return manager, nil
}

func (manager *Manager) DefaultLanguage() string {
	return manager.defaultLanguage
}

func (manager *Manager) SupportedLanguages() []string {
	result := make([]string, len(manager.supported))
	copy(result, manager.supported)
	return result
}

func (manager *Manager) NormalizeLanguage(raw string) string {
	normalized := normalizeLanguageTag(raw)
	if normalized == "" {
		return manager.defaultLanguage
	}
	if manager.isSupported(normalized) {
		return normalized
	}
	return manager.defaultLanguage
}

func (manager *Manager) DetectFromAcceptLanguage(raw string) string {
	for _, part := range strings.Split(raw, ",") {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}
		token = strings.TrimSpace(strings.Split(token, ";")[0])
		normalized := normalizeLanguageTag(token)
		if manager.isSupported(normalized) {
			return normalized
		}
	}
	return manager.defaultLanguage
}

func (manager *Manager) Messages(language string) map[string]string {
	defaultMessages := manager.locales[manager.defaultLanguage]
	targetLanguage := manager.NormalizeLanguage(language)
	targetMessages := manager.locales[targetLanguage]

	result := make(map[string]string, len(defaultMessages)+len(targetMessages))
	for key, value := range defaultMessages {
		result[key] = value
	}
	for key, value := range targetMessages {
		result[key] = value
	}
	return result
}

func (manager *Manager) Translate(language string, key string) string {
	messages := manager.Messages(language)
	if value, ok := messages[key]; ok && strings.TrimSpace(value) != "" {
		return value
	}
	return key
}

func (manager *Manager) Translatef(language string, key string, args ...any) string {
	return fmt.Sprintf(manager.Translate(language, key), args...)
}

func (manager *Manager) isSupported(language string) bool {
	if language == "" {
		return false
	}
	_, ok := manager.locales[language]
	return ok
}

func normalizeLanguageTag(raw string) string {
	language := strings.ToLower(strings.TrimSpace(raw))
	if language == "" {
		return ""
	}
	language = strings.ReplaceAll(language, "_", "-")
	if separator := strings.Index(language, "-"); separator >= 0 {
		language = language[:separator]
	}
	return language
}

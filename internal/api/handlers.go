package api

import (
	"errors"
	"strings"
	"time"

	"github.com/terraincognita07/ovumcy/internal/i18n"
)

func NewHandler(secret string, templateDir string, location *time.Location, i18nManager *i18n.Manager, cookieSecure bool, dependencies Dependencies) (*Handler, error) {
	secret = strings.TrimSpace(secret)
	if location == nil {
		location = time.Local
	}
	if secret == "" {
		return nil, errors.New("secret key is required")
	}
	if i18nManager == nil {
		return nil, errors.New("i18n manager is required")
	}
	if err := dependencies.validate(); err != nil {
		return nil, err
	}

	funcMap := newTemplateFuncMap()

	templates, err := parsePageTemplates(templateDir, funcMap, pageTemplates)
	if err != nil {
		return nil, err
	}

	partials, err := parsePartialTemplates(templateDir, funcMap, partialTemplateFiles)
	if err != nil {
		return nil, err
	}

	handler := &Handler{
		secretKey:    []byte(secret),
		location:     location,
		cookieSecure: cookieSecure,
		i18n:         i18nManager,
		templates:    templates,
		partials:     partials,
	}
	return handler.withDependencies(dependencies), nil
}

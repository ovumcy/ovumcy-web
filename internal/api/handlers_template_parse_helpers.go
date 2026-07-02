package api

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/templates"
)

func parsePageTemplates(funcMap template.FuncMap, pages []string) (map[string]*template.Template, error) {
	parsed := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		tmpl, err := template.New("base").Funcs(funcMap).ParseFS(
			templates.Files,
			"base.html",
			"components/*.html",
			page+".html",
		)
		if err != nil {
			return nil, fmt.Errorf("parse page template %s: %w", page, err)
		}
		parsed[page] = tmpl
	}
	return parsed, nil
}

func parsePartialTemplates(funcMap template.FuncMap, partialFiles []string) (map[string]*template.Template, error) {
	partials := make(map[string]*template.Template, len(partialFiles))
	for _, partial := range partialFiles {
		name := strings.TrimSuffix(partial, ".html")
		tmpl, err := template.New(name).Funcs(funcMap).ParseFS(
			templates.Files,
			"base.html",
			"components/*.html",
			partial,
		)
		if err != nil {
			return nil, fmt.Errorf("parse partial %s: %w", partial, err)
		}
		partials[name] = tmpl
	}
	return partials, nil
}

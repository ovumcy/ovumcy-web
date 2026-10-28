package api

import (
	"encoding/json"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

func formatTemplateLocalizedDate(language string, value time.Time, style string) string {
	switch style {
	case "short":
		return services.LocalizedDateShort(language, value)
	default:
		return services.LocalizedDateDisplay(language, value)
	}
}

// formatTemplateFloat is the machine-readable form, with a "." decimal, for an
// attribute such as <progress value>; anything a person reads goes through
// formatTemplateNumber or formatTemplateQuantity.
func formatTemplateFloat(value float64) string {
	return services.FormatLocalizedNumber("en", value)
}

func formatTemplateNumber(language string, value any) string {
	return services.FormatLocalizedNumber(language, templateNumber(value))
}

func formatTemplateQuantity(language string, value any, unit string) string {
	return services.FormatLocalizedQuantity(language, templateNumber(value), unit)
}

// templateNumber lets one template function take the int and float64 fields a
// stats page mixes.
func templateNumber(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

// templateToJSON serializes a value for embedding in HTML data-* attributes
// (base.html data-supported-languages, stats.html data-chart). It returns a
// plain string — NOT template.JS/template.HTML — so html/template applies full
// contextual escaping in the attribute context. Browser-side consumers read
// the attribute via getAttribute/dataset (chart-lite.js), which entity-decodes
// before JSON.parse, so the escaping round-trips transparently.
//
// Do not "optimize" this back to template.JS: a typed-string return would
// bypass the attribute escaper and make safety depend solely on json.Marshal's
// escaping behavior (the retired #nosec G203 pattern).
func templateToJSON(value any) string {
	serialized, err := json.Marshal(value)
	if err != nil {
		return "null"
	}
	return string(serialized)
}

package api

import (
	"encoding/json"
	"fmt"
	"math"
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

func formatTemplateFloat(value float64) string {
	rounded := math.Round(value*10) / 10
	if math.Abs(rounded-math.Round(rounded)) < 1e-9 {
		return fmt.Sprintf("%.0f", rounded)
	}
	return fmt.Sprintf("%.1f", rounded)
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

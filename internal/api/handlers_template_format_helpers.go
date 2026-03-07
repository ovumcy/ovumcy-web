package api

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"time"

	"github.com/terraincognita07/ovumcy/internal/services"
)

func formatTemplateDate(value time.Time, layout string) string {
	if value.IsZero() {
		return ""
	}
	return value.Format(layout)
}

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

func templateToJSON(value any) template.JS {
	serialized, err := json.Marshal(value)
	if err != nil {
		return template.JS("null")
	}
	// #nosec G203 -- json.Marshal escapes script-breaking characters before embedding a JS literal in the template.
	return template.JS(serialized)
}

package i18n

import "embed"

// localeFiles stores the locale JSON files embedded into the binary so the
// runtime image needs no on-disk locales directory.
//
//go:embed locales/*.json
var localeFiles embed.FS

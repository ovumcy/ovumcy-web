// Package staticassets exposes the browser static assets (CSS, JS, icons,
// fonts, manifest) embedded into the binary so the runtime image needs no
// on-disk web/static directory. The embed directive must live in the web/
// tree because go:embed cannot reach files outside its own package directory.
package staticassets

import "embed"

// Files stores the static assets under web/static embedded into the binary,
// rooted at the "static/" path prefix.
//
//go:embed static
var Files embed.FS

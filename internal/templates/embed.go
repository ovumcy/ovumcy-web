package templates

import "embed"

// Files stores the HTML templates embedded into the binary so the runtime
// image needs no on-disk templates directory.
//
//go:embed *.html components/*.html
var Files embed.FS

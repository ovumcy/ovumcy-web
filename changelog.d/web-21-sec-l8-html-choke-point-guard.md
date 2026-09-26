none

Test-only fix: SEC-L8 (WEB-21) claimed the only conversion to
`html/template.HTML` outside a test is the one inside `trustedEscapedHTML` in
`internal/httpx/markup.go`, but nothing checked that claim — a future
conversion added anywhere else in the module would bypass `html/template`'s
contextual auto-escaping with no guard noticing. `TestTemplateHTMLConversionIsConfinedToTheMarkupChokePoint`
type-checks the module's shipped, non-test packages and resolves every call
expression whose target type IS `html/template.HTML` by declaration (via
`go/types`, not by matching the source text), asserting the one allowed site
by name and failing with `file:line` for any other.

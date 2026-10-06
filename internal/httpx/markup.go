package httpx

import (
	"fmt"
	"html/template"
)

// StatusErrorMarkup renders the shared HTMX status-error wrapper. When
// errorKey is non-empty, the wrapper exposes it as data-flash-key + a
// data-flash-status="error" attribute so backend regressions and Playwright
// can assert the policy-selected key without matching the localized message.
func StatusErrorMarkup(message string, errorKey string) string {
	if errorKey == "" {
		return fmt.Sprintf("<div class=\"status-error\">%s</div>", template.HTMLEscapeString(message))
	}
	return fmt.Sprintf(
		"<div class=\"status-error\" data-flash-key=\"%s\" data-flash-status=\"error\">%s</div>",
		template.HTMLEscapeString(errorKey),
		template.HTMLEscapeString(message),
	)
}

// Kinds a success status may declare in its data-status-kind attribute. The
// client reads the kind, never the localized copy: a neutral status is a bare
// save confirmation a surface with its own save indicator does not repeat, and
// a persistent one is never cleared on a timer — it stays until dismissed.
const (
	StatusKindNeutral    = "neutral"
	StatusKindPersistent = "persistent"
)

// DismissibleStatusOKMarkup renders the shared HTMX dismissible status-ok wrapper.
func DismissibleStatusOKMarkup(message string, closeLabel string) string {
	return DismissibleStatusOKMarkupOfKind(message, closeLabel, "")
}

// DismissibleStatusOKMarkupOfKind renders the dismissible status-ok wrapper
// declaring kind in data-status-kind; an empty kind declares none.
func DismissibleStatusOKMarkupOfKind(message string, closeLabel string, kind string) string {
	kindAttribute := ""
	if kind != "" {
		kindAttribute = fmt.Sprintf(" data-status-kind=\"%s\"", template.HTMLEscapeString(kind))
	}
	return fmt.Sprintf(
		"<div class=\"status-ok\"%s><div class=\"toast-body\"><span class=\"toast-message-wrap\"><span class=\"toast-icon\" aria-hidden=\"true\">✓</span><span class=\"toast-message\">%s</span></span><button type=\"button\" class=\"toast-close\" data-dismiss-status aria-label=\"%s\">×</button></div></div>",
		kindAttribute,
		template.HTMLEscapeString(message),
		template.HTMLEscapeString(closeLabel),
	)
}

// DismissibleStatusOKTemplateHTML returns trusted dismissible success markup after escaping message content.
func DismissibleStatusOKTemplateHTML(message string, closeLabel string) template.HTML {
	return trustedEscapedHTML(DismissibleStatusOKMarkup(message, closeLabel))
}

// #nosec G203 -- shared status markup is built from already escaped strings in this package.
func trustedEscapedHTML(markup string) template.HTML {
	return template.HTML(markup)
}

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

// DismissibleStatusOKMarkup renders the shared HTMX dismissible status-ok wrapper.
func DismissibleStatusOKMarkup(message string, closeLabel string) string {
	return fmt.Sprintf(
		"<div class=\"status-ok\"><div class=\"toast-body\"><span class=\"toast-message-wrap\"><span class=\"toast-icon\" aria-hidden=\"true\">✓</span><span class=\"toast-message\">%s</span></span><button type=\"button\" class=\"toast-close\" data-dismiss-status aria-label=\"%s\">×</button></div></div>",
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

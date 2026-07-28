package api

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
)

type SecurityEventField struct {
	Key   string
	Value string
}

// LogSecurityEvent emits one audit line when the operator enabled the
// audit stream (Dependencies.AuditLogEnabled, from AUDIT_LOG_ENABLED).
// Exported for the entrypoint middleware (CSRF error handler, rate-limit
// handlers); handler code uses the unexported helpers below. The flag is
// carried per Handler instead of package state, so tests construct apps
// with the stream on or off without mutating globals.
func (handler *Handler) LogSecurityEvent(c fiber.Ctx, action string, outcome string, fields ...SecurityEventField) {
	if !handler.auditLogEnabled {
		return
	}
	emitSecurityEvent(c, action, outcome, fields...)
}

func emitSecurityEvent(c fiber.Ctx, action string, outcome string, fields ...SecurityEventField) {
	if c == nil {
		return
	}

	extraFields := make([]SecurityEventField, 0, len(fields))
	for _, field := range fields {
		key := normalizeSecurityEventKey(field.Key)
		if key == "" {
			continue
		}
		extraFields = append(extraFields, SecurityEventField{
			Key:   key,
			Value: strings.TrimSpace(field.Value),
		})
	}
	sort.Slice(extraFields, func(left int, right int) bool {
		return extraFields[left].Key < extraFields[right].Key
	})

	parts := []string{
		fmt.Sprintf("action=%q", strings.TrimSpace(action)),
		fmt.Sprintf("outcome=%q", strings.TrimSpace(outcome)),
		fmt.Sprintf("method=%q", c.Method()),
		fmt.Sprintf("path=%q", SafeRequestLogPath(c)),
		fmt.Sprintf("format=%q", securityEventRequestFormat(c)),
	}

	if user, ok := currentUser(c); ok && user != nil {
		parts = append(parts,
			fmt.Sprintf("user_id=%q", strconv.FormatUint(uint64(user.ID), 10)),
			fmt.Sprintf("role=%q", strings.TrimSpace(user.Role)),
		)
	}

	for _, field := range extraFields {
		parts = append(parts, fmt.Sprintf("%s=%q", field.Key, field.Value))
	}

	log.Printf("security event: %s", strings.Join(parts, " "))
}

func normalizeSecurityEventKey(key string) string {
	normalized := strings.ToLower(strings.TrimSpace(key))
	if normalized == "" {
		return ""
	}
	return strings.ReplaceAll(normalized, " ", "_")
}

func securityEventRequestFormat(c fiber.Ctx) string {
	switch {
	case isHTMX(c):
		return "htmx"
	case acceptsJSON(c):
		return "json"
	default:
		return "html"
	}
}

func securityEventField(key string, value string) SecurityEventField {
	return SecurityEventField{Key: key, Value: value}
}

func (handler *Handler) logSecurityEvent(c fiber.Ctx, action string, outcome string, fields ...SecurityEventField) {
	handler.LogSecurityEvent(c, action, outcome, fields...)
}

func (handler *Handler) logSecurityError(c fiber.Ctx, action string, spec APIErrorSpec, fields ...SecurityEventField) {
	combined := make([]SecurityEventField, 0, len(fields)+1)
	combined = append(combined, fields...)
	if strings.TrimSpace(spec.Key) != "" {
		combined = append(combined, securityEventField("reason", spec.Key))
	}
	handler.logSecurityEvent(c, action, securityEventOutcomeForSpec(spec), combined...)
}

// The audit stream carries two health domains, and they are deliberately
// distinct values rather than one value with a modifier. They answer different
// incident questions: health_data selects the actions that CHANGED or DESTROYED
// an owner's tracked data, health_egress the actions that carried it OUT of the
// instance. Widening health_data to cover both would make the erasure question
// return every routine CSV download — the same loss of precision that made the
// erasure actions unfindable before they were tagged. A review that wants both
// at once matches the shared `health_` prefix in one clause.
const (
	healthDataDomain   = "health_data"
	healthEgressDomain = "health_egress"
)

// healthDomainFields builds the domain (plus optional target) prefix shared by
// both health audit classes. It is the single place either domain value is
// written, so a new surface cannot invent a third spelling of a field operators
// filter on.
func healthDomainFields(domain string, target string, extra ...SecurityEventField) []SecurityEventField {
	fields := make([]SecurityEventField, 0, len(extra)+2)
	fields = append(fields, securityEventField("domain", domain))
	if normalizedTarget := strings.TrimSpace(target); normalizedTarget != "" {
		fields = append(fields, securityEventField("target", normalizedTarget))
	}
	return append(fields, extra...)
}

func (handler *Handler) logHealthDomainEvent(c fiber.Ctx, domain string, action string, outcome string, target string, extra ...SecurityEventField) {
	handler.logSecurityEvent(c, action, outcome, healthDomainFields(domain, target, extra...)...)
}

func (handler *Handler) logHealthDomainError(c fiber.Ctx, domain string, action string, spec APIErrorSpec, target string, extra ...SecurityEventField) {
	handler.logSecurityError(c, action, spec, healthDomainFields(domain, target, extra...)...)
}

func (handler *Handler) logHealthDataMutation(c fiber.Ctx, action string, outcome string, target string, extra ...SecurityEventField) {
	handler.logHealthDomainEvent(c, healthDataDomain, action, outcome, target, extra...)
}

func (handler *Handler) logHealthDataMutationError(c fiber.Ctx, action string, spec APIErrorSpec, target string, extra ...SecurityEventField) {
	handler.logHealthDomainError(c, healthDataDomain, action, spec, target, extra...)
}

// healthMutationKind names one audited health-data mutation: the security
// event action plus its target field. Handlers declare kinds as file-level
// constants and pass them to the helpers below, so a re-typed string
// literal cannot silently mis-tag an audit line (the compiler has no
// opinion on "health.symptom_craete").
type healthMutationKind struct {
	action string
	target string
}

// logMutationSuccess and logMutationError take the extra fields a specific
// handler wants to add (the restore's counts), so a handler that needs one is
// not pushed off the typed path and into assembling `domain` by hand — which is
// how the JSON restore came to be the one health-data line outside this file.
func (handler *Handler) logMutationSuccess(c fiber.Ctx, kind healthMutationKind, extra ...SecurityEventField) {
	handler.logHealthDataMutation(c, kind.action, "success", kind.target, extra...)
}

func (handler *Handler) logMutationError(c fiber.Ctx, kind healthMutationKind, spec APIErrorSpec, extra ...SecurityEventField) {
	handler.logHealthDataMutationError(c, kind.action, spec, kind.target, extra...)
}

// failMutation is the common tail of mutation handlers: log the
// denied/failed audit event and respond with the mapped error.
func (handler *Handler) failMutation(c fiber.Ctx, kind healthMutationKind, spec APIErrorSpec) error {
	handler.logMutationError(c, kind, spec)
	return handler.respondMappedError(c, spec)
}

// healthEgressKind names one audited health-data egress: an action that carries
// an owner's health data out of the instance, or that hands a person a secret
// granting standing access to it. It is the read-side counterpart of
// healthMutationKind and follows the same rule — declared once per handler file
// so no event-name or target literal is left at a call site.
//
// A kind may also carry one constant detail field (the export format). That
// detail belongs to the kind rather than to each call site: written out by hand
// it was present on five branches of the CSV handler and absent from the two in
// the shared prologue, so a rejected export could not be attributed to a format.
type healthEgressKind struct {
	action string
	target string
	detail SecurityEventField
}

func (kind healthEgressKind) detailFields() []SecurityEventField {
	if strings.TrimSpace(kind.detail.Key) == "" {
		return nil
	}
	return []SecurityEventField{kind.detail}
}

func (handler *Handler) logEgressSuccess(c fiber.Ctx, kind healthEgressKind) {
	handler.logHealthDomainEvent(c, healthEgressDomain, kind.action, "success", kind.target, kind.detailFields()...)
}

func (handler *Handler) logEgressError(c fiber.Ctx, kind healthEgressKind, spec APIErrorSpec) {
	handler.logHealthDomainError(c, healthEgressDomain, kind.action, spec, kind.target, kind.detailFields()...)
}

// failEgress is the common tail of egress handlers: log the denied/failed audit
// event and respond with the mapped error.
func (handler *Handler) failEgress(c fiber.Ctx, kind healthEgressKind, spec APIErrorSpec) error {
	handler.logEgressError(c, kind, spec)
	return handler.respondMappedError(c, spec)
}

func securityEventOutcomeForSpec(spec APIErrorSpec) string {
	if spec.Status >= fiber.StatusInternalServerError {
		return "failure"
	}
	return "denied"
}

package api

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
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
func (handler *Handler) LogSecurityEvent(c *fiber.Ctx, action string, outcome string, fields ...SecurityEventField) {
	if !handler.auditLogEnabled {
		return
	}
	emitSecurityEvent(c, action, outcome, fields...)
}

func emitSecurityEvent(c *fiber.Ctx, action string, outcome string, fields ...SecurityEventField) {
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

func securityEventRequestFormat(c *fiber.Ctx) string {
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

func (handler *Handler) logSecurityEvent(c *fiber.Ctx, action string, outcome string, fields ...SecurityEventField) {
	handler.LogSecurityEvent(c, action, outcome, fields...)
}

func (handler *Handler) logSecurityError(c *fiber.Ctx, action string, spec APIErrorSpec, fields ...SecurityEventField) {
	combined := make([]SecurityEventField, 0, len(fields)+1)
	combined = append(combined, fields...)
	if strings.TrimSpace(spec.Key) != "" {
		combined = append(combined, securityEventField("reason", spec.Key))
	}
	handler.logSecurityEvent(c, action, securityEventOutcomeForSpec(spec), combined...)
}

func (handler *Handler) logHealthDataMutation(c *fiber.Ctx, action string, outcome string, target string) {
	fields := []SecurityEventField{securityEventField("domain", "health_data")}
	if normalizedTarget := strings.TrimSpace(target); normalizedTarget != "" {
		fields = append(fields, securityEventField("target", normalizedTarget))
	}
	handler.logSecurityEvent(c, action, outcome, fields...)
}

func (handler *Handler) logHealthDataMutationError(c *fiber.Ctx, action string, spec APIErrorSpec, target string) {
	fields := []SecurityEventField{securityEventField("domain", "health_data")}
	if normalizedTarget := strings.TrimSpace(target); normalizedTarget != "" {
		fields = append(fields, securityEventField("target", normalizedTarget))
	}
	handler.logSecurityError(c, action, spec, fields...)
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

func (handler *Handler) logMutationSuccess(c *fiber.Ctx, kind healthMutationKind) {
	handler.logHealthDataMutation(c, kind.action, "success", kind.target)
}

func (handler *Handler) logMutationError(c *fiber.Ctx, kind healthMutationKind, spec APIErrorSpec) {
	handler.logHealthDataMutationError(c, kind.action, spec, kind.target)
}

// failMutation is the common tail of mutation handlers: log the
// denied/failed audit event and respond with the mapped error.
func (handler *Handler) failMutation(c *fiber.Ctx, kind healthMutationKind, spec APIErrorSpec) error {
	handler.logMutationError(c, kind, spec)
	return handler.respondMappedError(c, spec)
}

func securityEventOutcomeForSpec(spec APIErrorSpec) string {
	if spec.Status >= fiber.StatusInternalServerError {
		return "failure"
	}
	return "denied"
}

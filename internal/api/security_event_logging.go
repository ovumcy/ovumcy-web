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

func LogSecurityEvent(c *fiber.Ctx, action string, outcome string, fields ...SecurityEventField) {
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
		fmt.Sprintf("path=%q", c.Path()),
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
	LogSecurityEvent(c, action, outcome, fields...)
}

func (handler *Handler) logSecurityError(c *fiber.Ctx, action string, spec APIErrorSpec, fields ...SecurityEventField) {
	combined := make([]SecurityEventField, 0, len(fields)+1)
	combined = append(combined, fields...)
	if strings.TrimSpace(spec.Key) != "" {
		combined = append(combined, securityEventField("reason", spec.Key))
	}
	handler.logSecurityEvent(c, action, securityEventOutcomeForSpec(spec), combined...)
}

func securityEventOutcomeForSpec(spec APIErrorSpec) string {
	if spec.Status >= fiber.StatusInternalServerError {
		return "failure"
	}
	return "denied"
}

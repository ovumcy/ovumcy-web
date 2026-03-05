package api

import "github.com/gofiber/fiber/v2"

type APIErrorCategory string

const (
	APIErrorCategoryValidation   APIErrorCategory = "validation"
	APIErrorCategoryNotFound     APIErrorCategory = "not_found"
	APIErrorCategoryConflict     APIErrorCategory = "conflict"
	APIErrorCategoryUnauthorized APIErrorCategory = "unauthorized"
	APIErrorCategoryForbidden    APIErrorCategory = "forbidden"
	APIErrorCategoryRateLimited  APIErrorCategory = "rate_limited"
	APIErrorCategoryInternal     APIErrorCategory = "internal"
)

type APIErrorTarget string

const (
	APIErrorTargetGlobal       APIErrorTarget = "global"
	APIErrorTargetAuthForm     APIErrorTarget = "auth_form"
	APIErrorTargetSettingsForm APIErrorTarget = "settings_form"
)

type APIErrorSpec struct {
	Category APIErrorCategory
	Status   int
	Key      string
	Target   APIErrorTarget
}

func (spec APIErrorSpec) IsFormError() bool {
	return spec.Target == APIErrorTargetAuthForm || spec.Target == APIErrorTargetSettingsForm
}

func globalErrorSpec(status int, category APIErrorCategory, key string) APIErrorSpec {
	return APIErrorSpec{
		Category: category,
		Status:   status,
		Key:      key,
		Target:   APIErrorTargetGlobal,
	}
}

func authFormErrorSpec(status int, category APIErrorCategory, key string) APIErrorSpec {
	return APIErrorSpec{
		Category: category,
		Status:   status,
		Key:      key,
		Target:   APIErrorTargetAuthForm,
	}
}

func settingsFormErrorSpec(status int, category APIErrorCategory, key string) APIErrorSpec {
	return APIErrorSpec{
		Category: category,
		Status:   status,
		Key:      key,
		Target:   APIErrorTargetSettingsForm,
	}
}

func respondGlobalMappedError(c *fiber.Ctx, spec APIErrorSpec) error {
	return apiError(c, spec.Status, spec.Key)
}

func (handler *Handler) respondMappedError(c *fiber.Ctx, spec APIErrorSpec) error {
	switch spec.Target {
	case APIErrorTargetAuthForm:
		return handler.respondAuthError(c, spec.Status, spec.Key)
	case APIErrorTargetSettingsForm:
		return handler.respondSettingsError(c, spec.Status, spec.Key)
	default:
		return apiError(c, spec.Status, spec.Key)
	}
}

package api

import "github.com/gofiber/fiber/v2"

func unauthorizedErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "unauthorized")
}

func onboardingRequiredErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "onboarding required")
}

func ownerAccessRequiredErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "owner access required")
}

func setupStateLoadErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load setup state")
}

func invalidMonthErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid month")
}

func notFoundErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "not found")
}

func templateNotFoundErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "template not found")
}

func templateRenderErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to render template")
}

func partialNotFoundErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "partial not found")
}

func partialRenderErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to render partial")
}

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestApiErrorJSONEmitsErrorDetailEnvelope enforces the Phase 2C contract:
// JSON error responses carry both the legacy top-level `error` string key
// (backward compatibility) and the richer `error_detail` object with
// `key`, `category`, and `target` so that programmatic clients can branch
// on the error class without hardcoding key strings.
func TestApiErrorJSONEmitsErrorDetailEnvelope(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/api/test/global-validation", func(c fiber.Ctx) error {
		return respondGlobalMappedError(c, globalErrorSpec(
			fiber.StatusBadRequest,
			APIErrorCategoryValidation,
			"invalid input",
		))
	})

	request := httptest.NewRequest(http.MethodGet, "/api/test/global-validation", nil)
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("global validation request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	payload := struct {
		Error       string `json:"error"`
		ErrorDetail struct {
			Key      string `json:"key"`
			Category string `json:"category"`
			Target   string `json:"target"`
		} `json:"error_detail"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response body: %v (body=%s)", err, string(body))
	}

	if payload.Error != "invalid input" {
		t.Fatalf("expected top-level error %q, got %q", "invalid input", payload.Error)
	}
	if payload.ErrorDetail.Key != "invalid input" {
		t.Fatalf("expected error_detail.key %q, got %q", "invalid input", payload.ErrorDetail.Key)
	}
	if payload.ErrorDetail.Category != string(APIErrorCategoryValidation) {
		t.Fatalf("expected error_detail.category %q, got %q", string(APIErrorCategoryValidation), payload.ErrorDetail.Category)
	}
	if payload.ErrorDetail.Target != string(APIErrorTargetGlobal) {
		t.Fatalf("expected error_detail.target %q, got %q", string(APIErrorTargetGlobal), payload.ErrorDetail.Target)
	}
}

// TestApiErrorJSONErrorDetailReflectsTarget confirms the target field changes
// across error spec targets (global, auth_form, settings_form), giving JSON
// clients enough context to render form-vs-global errors without hardcoding
// the key string set.
func TestApiErrorJSONErrorDetailReflectsTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		spec           APIErrorSpec
		expectTarget   string
		expectCategory string
		expectStatus   int
	}{
		{
			name:           "global not_found",
			spec:           globalErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "not found"),
			expectTarget:   string(APIErrorTargetGlobal),
			expectCategory: string(APIErrorCategoryNotFound),
			expectStatus:   http.StatusNotFound,
		},
		{
			name:           "auth_form unauthorized",
			spec:           authFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid credentials"),
			expectTarget:   string(APIErrorTargetAuthForm),
			expectCategory: string(APIErrorCategoryUnauthorized),
			expectStatus:   http.StatusUnauthorized,
		},
		{
			name:           "settings_form validation",
			spec:           settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password"),
			expectTarget:   string(APIErrorTargetSettingsForm),
			expectCategory: string(APIErrorCategoryValidation),
			expectStatus:   http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			app := fiber.New()
			spec := tc.spec
			app.Get("/api/test/case", func(c fiber.Ctx) error {
				return apiError(c, spec)
			})

			request := httptest.NewRequest(http.MethodGet, "/api/test/case", nil)
			request.Header.Set("Accept", "application/json")

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != tc.expectStatus {
				t.Fatalf("expected status %d, got %d", tc.expectStatus, response.StatusCode)
			}

			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}

			payload := struct {
				Error       string `json:"error"`
				ErrorDetail struct {
					Key      string `json:"key"`
					Category string `json:"category"`
					Target   string `json:"target"`
				} `json:"error_detail"`
			}{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode body: %v (body=%s)", err, string(body))
			}

			if payload.ErrorDetail.Target != tc.expectTarget {
				t.Fatalf("expected target %q, got %q", tc.expectTarget, payload.ErrorDetail.Target)
			}
			if payload.ErrorDetail.Category != tc.expectCategory {
				t.Fatalf("expected category %q, got %q", tc.expectCategory, payload.ErrorDetail.Category)
			}
		})
	}
}

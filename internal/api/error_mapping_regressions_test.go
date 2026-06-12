package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestCommonErrorSpecs(t *testing.T) {
	testCases := []struct {
		name string
		got  APIErrorSpec
		want APIErrorSpec
	}{
		{
			name: "unauthorized",
			got:  unauthorizedErrorSpec(),
			want: globalErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "unauthorized"),
		},
		{
			name: "onboarding required",
			got:  onboardingRequiredErrorSpec(),
			want: globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "onboarding required"),
		},
		{
			name: "owner access required",
			got:  ownerAccessRequiredErrorSpec(),
			want: globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "owner access required"),
		},
		{
			name: "setup state load",
			got:  setupStateLoadErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load setup state"),
		},
		{
			name: "invalid month",
			got:  invalidMonthErrorSpec(),
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid month"),
		},
		{
			name: "not found",
			got:  notFoundErrorSpec(),
			want: globalErrorSpec(fiber.StatusNotFound, APIErrorCategoryNotFound, "not found"),
		},
		{
			name: "template not found",
			got:  templateNotFoundErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "template not found"),
		},
		{
			name: "template render",
			got:  templateRenderErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to render template"),
		},
		{
			name: "partial render",
			got:  partialRenderErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to render partial"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", testCase.got, testCase.want)
			}
		})
	}
}

func TestMapExportRangeError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "invalid from",
			err:  services.ErrExportFromDateInvalid,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid from date"),
		},
		{
			name: "invalid to",
			err:  services.ErrExportToDateInvalid,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid to date"),
		},
		{
			name: "invalid range",
			err:  services.ErrExportRangeInvalid,
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid range"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapExportRangeError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestOnboardingErrorSpecs(t *testing.T) {
	testCases := []struct {
		name string
		got  APIErrorSpec
		want APIErrorSpec
	}{
		{
			name: "validation",
			got:  onboardingValidationErrorSpec("invalid input"),
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid input"),
		},
		{
			name: "save step",
			got:  onboardingSaveStepErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to save onboarding step"),
		},
		{
			name: "steps required",
			got:  onboardingStepsRequiredErrorSpec(),
			want: globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "complete onboarding steps first"),
		},
		{
			name: "finish",
			got:  onboardingFinishErrorSpec(),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to finish onboarding"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", testCase.got, testCase.want)
			}
		})
	}
}

func TestMapSettingsPasswordChangeError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "invalid input",
			err:  services.ErrSettingsPasswordChangeInvalidInput,
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "invalid settings input"),
		},
		{
			name: "password mismatch",
			err:  services.ErrSettingsPasswordMismatch,
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "password mismatch"),
		},
		{
			name: "invalid current password",
			err:  services.ErrSettingsInvalidCurrentPassword,
			want: settingsFormErrorSpec(fiber.StatusUnauthorized, APIErrorCategoryUnauthorized, "invalid current password"),
		},
		{
			name: "local password required",
			err:  services.ErrSettingsLocalPasswordNotSet,
			want: settingsLocalPasswordRequiredErrorSpec(),
		},
		{
			name: "new password must differ",
			err:  services.ErrSettingsNewPasswordMustDiffer,
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "new password must differ"),
		},
		{
			name: "weak password",
			err:  services.ErrSettingsWeakPassword,
			want: settingsFormErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "weak password"),
		},
		{
			name: "hash failed",
			err:  services.ErrSettingsPasswordHashFailed,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to secure password"),
		},
		{
			name: "recovery code failed",
			err:  services.ErrSettingsRecoveryCodeGenerateFailed,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to secure password"),
		},
		{
			name: "update failed",
			err:  services.ErrSettingsPasswordUpdateFailed,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update password"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update password"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapSettingsPasswordChangeError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapRecoveryCodeRegenerationError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "generate failed",
			err:  services.ErrRecoveryCodeGenerate,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to create recovery code"),
		},
		{
			name: "update failed",
			err:  services.ErrRecoveryCodeUpdate,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update recovery code"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to update recovery code"),
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if got := mapRecoveryCodeRegenerationError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

// Rate-limit responder coverage. These exercise the spec constructors plus
// RespondAuthRateLimited / RespondAPIRateLimited / retryAfterSeconds, which
// the global and per-route rate limiters use to translate a 429 into the
// shape the requesting client expects (JSON envelope, auth flash, settings
// flash, or global API error).

func TestAuthRateLimitErrorSpecFallsBackToCanonicalKey(t *testing.T) {
	got := authRateLimitErrorSpec("")
	want := authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many requests")
	if got != want {
		t.Fatalf("empty key: got %#v want %#v", got, want)
	}

	withKey := authRateLimitErrorSpec("  too many login attempts  ")
	wantWithKey := authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many login attempts")
	if withKey != wantWithKey {
		t.Fatalf("with key: got %#v want %#v", withKey, wantWithKey)
	}
}

func TestSettingsAndGlobalRateLimitErrorSpecsAreCanonical(t *testing.T) {
	if got, want := settingsRateLimitErrorSpec(), settingsFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many requests"); got != want {
		t.Fatalf("settings rate limit: got %#v want %#v", got, want)
	}
	if got, want := globalRateLimitErrorSpec(), globalErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many requests"); got != want {
		t.Fatalf("global rate limit: got %#v want %#v", got, want)
	}
}

func TestRetryAfterSecondsParsesHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   int
	}{
		{name: "missing header", header: "", want: 0},
		{name: "valid integer", header: "30", want: 30},
		{name: "whitespace padded", header: "  45  ", want: 45},
		{name: "zero rejected", header: "0", want: 0},
		{name: "negative rejected", header: "-5", want: 0},
		{name: "non-integer rejected", header: "Wed, 21 Oct 2026 07:28:00 GMT", want: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			app := fiber.New()
			var observed int
			app.Get("/probe", func(c *fiber.Ctx) error {
				if tt.header != "" {
					c.Response().Header.Set(fiber.HeaderRetryAfter, tt.header)
				}
				observed = retryAfterSeconds(c)
				return c.SendStatus(fiber.StatusNoContent)
			})
			_, _ = app.Test(httptest.NewRequest(http.MethodGet, "/probe", nil), -1)
			if observed != tt.want {
				t.Fatalf("retryAfterSeconds(%q) = %d, want %d", tt.header, observed, tt.want)
			}
		})
	}
}

// TestRespondAPIRateLimitedRoutesByRequestShape locks the contract for
// callers that already set Retry-After: a JSON-accepting client must see
// {"error": key, "retry_after_seconds": N} so it can back off; a browser
// client falls through to the path-aware mapped error (auth flash, settings
// flash, or global) via respondMappedError. If a future refactor returned
// raw status fragments for JSON clients, the front-end retry queue would
// silently lose its back-off hint.
func TestRespondAPIRateLimitedRoutesByRequestShape(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		accept         string
		wantStatus     int
		wantErrorKey   string
		wantRetryAfter int
	}{
		{name: "auth form path with JSON accept", path: "/api/v1/sessions", accept: "application/json", wantStatus: fiber.StatusTooManyRequests, wantErrorKey: "too many requests", wantRetryAfter: 12},
		{name: "settings path with JSON accept", path: "/api/v1/users/current", accept: "application/json", wantStatus: fiber.StatusTooManyRequests, wantErrorKey: "too many requests", wantRetryAfter: 7},
		{name: "global path with JSON accept", path: "/api/v1/days", accept: "application/json", wantStatus: fiber.StatusTooManyRequests, wantErrorKey: "too many requests", wantRetryAfter: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			handler := &Handler{}
			app := fiber.New()
			app.Get(tt.path, func(c *fiber.Ctx) error {
				if tt.wantRetryAfter > 0 {
					c.Response().Header.Set(fiber.HeaderRetryAfter, strconv.Itoa(tt.wantRetryAfter))
				}
				return handler.RespondAPIRateLimited(c)
			})

			request := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.accept != "" {
				request.Header.Set("Accept", tt.accept)
			}
			response, err := app.Test(request, -1)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != tt.wantStatus {
				t.Fatalf("status: got %d want %d", response.StatusCode, tt.wantStatus)
			}
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			payload := map[string]any{}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("unmarshal JSON envelope %q: %v", body, err)
			}
			if payload["error"] != tt.wantErrorKey {
				t.Fatalf("error key: got %v want %q", payload["error"], tt.wantErrorKey)
			}
			retry, hasRetry := payload["retry_after_seconds"]
			if tt.wantRetryAfter > 0 {
				if !hasRetry {
					t.Fatalf("expected retry_after_seconds in payload, got %v", payload)
				}
				if int(retry.(float64)) != tt.wantRetryAfter {
					t.Fatalf("retry_after_seconds: got %v want %d", retry, tt.wantRetryAfter)
				}
			} else if hasRetry {
				t.Fatalf("did not expect retry_after_seconds without Retry-After header, got %v", retry)
			}
		})
	}
}

// TestRespondAPIRateLimitedWithoutJSONAcceptFallsBackToMappedError locks
// the browser-client path of the rate-limit responder: without an
// application/json Accept header, the JSON envelope branch is skipped and
// the response goes through respondMappedError. For the global API path
// that means a global rate-limit error spec is emitted rather than a JSON
// retry-after payload. Without this lock a regression that silently
// removed the JSON-accept gate could leak the envelope to browser clients.
func TestRespondAPIRateLimitedWithoutJSONAcceptFallsBackToMappedError(t *testing.T) {
	handler := &Handler{}
	app := fiber.New()
	app.Get("/api/v1/days", func(c *fiber.Ctx) error {
		return handler.RespondAPIRateLimited(c)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/days", nil)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("expected 429 from HTML fallback, got %d", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if strings.Contains(string(body), `"retry_after_seconds"`) {
		t.Fatalf("did not expect JSON envelope in HTML fallback response, got %q", body)
	}
}

// TestRespondAuthRateLimitedFallsBackThroughAuthFlash locks the browser
// path for the auth form variant. Without a JSON Accept header, the
// response goes through the auth flash + redirect plumbing rather than the
// JSON envelope, matching what the rate-limited login form sees.
func TestRespondAuthRateLimitedFallsBackThroughAuthFlash(t *testing.T) {
	handler := &Handler{
		secretKey:    []byte(testHandlerSecretKey),
		cookieSecure: true,
	}
	app := fiber.New()
	app.Post("/api/v1/sessions", func(c *fiber.Ctx) error {
		return handler.RespondAuthRateLimited(c, "too many login attempts")
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader("email=test"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != fiber.StatusSeeOther {
		t.Fatalf("expected 303 redirect for HTML rate-limited auth form, got %d", response.StatusCode)
	}
	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil || flashCookie.Value == "" {
		t.Fatal("expected flash cookie carrying the rate-limited auth error")
	}
	payload := decodeFlashCookieForTest(t, flashCookie.Value)
	if payload.AuthError != "too many login attempts" {
		t.Fatalf("expected flash auth_error %q, got %q", "too many login attempts", payload.AuthError)
	}
}

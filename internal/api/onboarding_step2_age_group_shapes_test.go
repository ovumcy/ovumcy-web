package api

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestOnboardingStep2AgeGroupPresenceAcrossRequestShapes closes the leaks in
// API-2 (handlers_onboarding_input_helpers.go): the presence guard for the
// removed `age_group` field used to answer "absent" for any shape it did not
// know how to probe, which meant a client using that shape was told 200
// while the field it sent was silently discarded — the exact
// removed-field-reads-as-success failure
// TestOnboardingStep2NamesTheAgeGroupItNoLongerAccepts (in
// api_v1_declared_breaks_contract_test.go) already closed for a form body and
// a JSON body carrying a string. Shapes that slipped past an earlier guard:
//   - the URL's own query string on a non-JSON request (the guard only
//     inspected `PostArgs`, never `QueryArgs`, though the fields this
//     endpoint DOES accept are read via `FormValue`, which searches
//     `QueryArgs` first);
//   - multipart/form-data (fasthttp only populates `PostArgs` for an exact
//     `application/x-www-form-urlencoded` Content-Type; a multipart field
//     never reaches it);
//   - a non-string JSON value (`3`, `null`): binding `age_group` into a
//     `*string` probe fails with a TYPE error, which an earlier guard read
//     identically to the key being absent;
//   - the URL's own query string on a JSON request: the query-string check
//     lived only in the non-JSON branch, so `?age_group=x` with
//     `Content-Type: application/json` and a body that never names the key
//     was still answered 200;
//   - a JSON key differing only in case (`Age_Group`, `AGE_GROUP`):
//     `Bind().Body` matches an object key to a struct field with a
//     case-insensitive fallback, so a body shaped like that used to bind and
//     get refused; a bare map lookup by the exact key does not.
//
// Each row below submits a normal, otherwise-valid step-2 body and either
// carries `age_group` in the row's shape or omits it entirely. A present
// field must always be refused by name (400, "onboarding does not accept an
// age group") with nothing written; an absent field must always succeed
// (200) and persist the submitted cycle/period — proving the fix does not
// turn into a false positive that refuses ordinary submissions in that shape.
func TestOnboardingStep2AgeGroupPresenceAcrossRequestShapes(t *testing.T) {
	const cycleLength = 27
	const periodLength = 4

	type testCase struct {
		name         string
		present      bool
		buildRequest func(t *testing.T, path string) *http.Request
	}

	cases := []testCase{
		{
			name:    "query-string/absent",
			present: false,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return formEncodedRequest(t, path, url.Values{
					"cycle_length":  {fmt.Sprint(cycleLength)},
					"period_length": {fmt.Sprint(periodLength)},
				})
			},
		},
		{
			name:    "query-string/present",
			present: true,
			buildRequest: func(t *testing.T, path string) *http.Request {
				// age_group rides the URL query string; the body itself never
				// names it, which is exactly the shape PostArgs cannot see.
				queried := path + "?age_group=40-45"
				return formEncodedRequest(t, queried, url.Values{
					"cycle_length":  {fmt.Sprint(cycleLength)},
					"period_length": {fmt.Sprint(periodLength)},
				})
			},
		},
		{
			name:    "multipart/absent",
			present: false,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return multipartRequest(t, path, map[string]string{
					"cycle_length":  fmt.Sprint(cycleLength),
					"period_length": fmt.Sprint(periodLength),
				})
			},
		},
		{
			name:    "multipart/present",
			present: true,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return multipartRequest(t, path, map[string]string{
					"cycle_length":  fmt.Sprint(cycleLength),
					"period_length": fmt.Sprint(periodLength),
					"age_group":     "40-45",
				})
			},
		},
		{
			name:    "form/absent",
			present: false,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return formEncodedRequest(t, path, url.Values{
					"cycle_length":  {fmt.Sprint(cycleLength)},
					"period_length": {fmt.Sprint(periodLength)},
				})
			},
		},
		{
			name:    "form/present",
			present: true,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return formEncodedRequest(t, path, url.Values{
					"cycle_length":  {fmt.Sprint(cycleLength)},
					"period_length": {fmt.Sprint(periodLength)},
					"age_group":     {"40-45"},
				})
			},
		},
		{
			name:    "json-string/absent",
			present: false,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return jsonRequest(t, path, fmt.Sprintf(`{"cycle_length":%d,"period_length":%d}`, cycleLength, periodLength))
			},
		},
		{
			name:    "json-string/present",
			present: true,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return jsonRequest(t, path, fmt.Sprintf(`{"cycle_length":%d,"period_length":%d,"age_group":"40-45"}`, cycleLength, periodLength))
			},
		},
		{
			// The core of the bug: a JSON number fails to bind into the
			// *string probe with a type error, which the pre-fix guard
			// mistook for the key being absent.
			name:    "json-number/present",
			present: true,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return jsonRequest(t, path, fmt.Sprintf(`{"cycle_length":%d,"period_length":%d,"age_group":3}`, cycleLength, periodLength))
			},
		},
		{
			// Same failure mode as json-number: `null` still names the key.
			name:    "json-null/present",
			present: true,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return jsonRequest(t, path, fmt.Sprintf(`{"cycle_length":%d,"period_length":%d,"age_group":null}`, cycleLength, periodLength))
			},
		},
		{
			// The query string belongs to neither transport: a JSON request
			// whose BODY never names age_group can still carry it in the URL.
			// A guard that only checks QueryArgs on the non-JSON branch misses
			// this entirely.
			name:    "json-body/query-string-present",
			present: true,
			buildRequest: func(t *testing.T, path string) *http.Request {
				queried := path + "?age_group=40-45"
				return jsonRequest(t, queried, fmt.Sprintf(`{"cycle_length":%d,"period_length":%d}`, cycleLength, periodLength))
			},
		},
		{
			// A struct-tag JSON probe would have matched this via
			// encoding/json's case-insensitive fallback; a bare map lookup by
			// the exact key does not, unless it compares case-insensitively
			// itself.
			name:    "json-string/mixed-case-key-present",
			present: true,
			buildRequest: func(t *testing.T, path string) *http.Request {
				return jsonRequest(t, path, fmt.Sprintf(`{"cycle_length":%d,"period_length":%d,"Age_Group":"40-45"}`, cycleLength, periodLength))
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "step2-shape-"+sanitizeSubtestEmail(testCase.name)+"@example.com", "StrongPass1", false)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

			request := testCase.buildRequest(t, "/api/v1/onboarding/steps/2")
			request.Header.Set("Accept", "application/json")
			request.Header.Set("Cookie", authCookie)

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("step2 request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			var stored models.User
			if err := database.First(&stored, user.ID).Error; err != nil {
				t.Fatalf("load user: %v", err)
			}

			if testCase.present {
				if response.StatusCode != http.StatusBadRequest {
					t.Fatalf("age_group present (%s): status = %d, want 400 (body %q)", testCase.name, response.StatusCode, mustReadBodyString(t, response.Body))
				}
				if key := errorKeyFromEnvelope(t, response.Body); key != "onboarding does not accept an age group" {
					t.Fatalf("age_group present (%s): error key = %q, want the named refusal", testCase.name, key)
				}
				if stored.CycleLength == cycleLength || stored.PeriodLength == periodLength {
					t.Fatalf("age_group present (%s): refused body still saved cycle=%d period=%d", testCase.name, stored.CycleLength, stored.PeriodLength)
				}
				return
			}

			if response.StatusCode != http.StatusOK {
				t.Fatalf("age_group absent (%s): status = %d, want 200 (body %q)", testCase.name, response.StatusCode, mustReadBodyString(t, response.Body))
			}
			if stored.CycleLength != cycleLength || stored.PeriodLength != periodLength {
				t.Fatalf("age_group absent (%s): expected cycle=%d period=%d to persist, got cycle=%d period=%d", testCase.name, cycleLength, periodLength, stored.CycleLength, stored.PeriodLength)
			}
		})
	}
}

func formEncodedRequest(t *testing.T, path string, values url.Values) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func jsonRequest(t *testing.T, path string, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

// multipartRequest encodes the given fields as a multipart/form-data body —
// the shape fasthttp never mirrors into PostArgs, so a presence guard reading
// only PostArgs is structurally blind to it.
func multipartRequest(t *testing.T, path string, fields map[string]string) *http.Request {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write multipart field %s: %v", key, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &buffer)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

// sanitizeSubtestEmail turns a subtest name (which contains '/') into a
// address-local-part-safe fragment so each row seeds its own user.
func sanitizeSubtestEmail(name string) string {
	return strings.NewReplacer("/", "-").Replace(name)
}

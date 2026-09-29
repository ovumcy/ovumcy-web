package api

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestCSRFTokenIsNeverReadFromTheQueryString pins the extractor's sources to
// the two the spec declares: the `csrf_token` field of a form body and the
// X-CSRF-Token header. A token in the URL is logged, cached and sent in a
// Referer, and — because the query used to be searched before the body — one
// planted there shadowed the token the form actually carried.
func TestCSRFTokenIsNeverReadFromTheQueryString(t *testing.T) {
	multipartWith := func(t *testing.T, fields map[string]string) (string, string) {
		t.Helper()
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		for key, value := range fields {
			if err := writer.WriteField(key, value); err != nil {
				t.Fatalf("multipart field: %v", err)
			}
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("multipart close: %v", err)
		}
		return buffer.String(), writer.FormDataContentType()
	}

	for _, tc := range []struct {
		name string
		// build returns the target query suffix, the body, the content type and
		// the X-CSRF-Token header value, given the valid token.
		build      func(t *testing.T, token string) (query, body, contentType, header string)
		wantStatus int
	}{
		{
			name: "token only in the query",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "?csrf_token=" + url.QueryEscape(token), "email=a%40example.com&password=Wrong1", "application/x-www-form-urlencoded", ""
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "token only in the query of a json request",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "?csrf_token=" + url.QueryEscape(token), `{"email":"a@example.com","password":"Wrong1"}`, "application/json", ""
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "token only in a json body member",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "", `{"email":"a@example.com","password":"Wrong1","csrf_token":"` + token + `"}`, "application/json", ""
			},
			wantStatus: http.StatusForbidden,
		},
		// The two bodies below name the token the way the body binder reads it
		// for the extractor's tagged struct: the field's own name, because the
		// struct carries a form tag only. A body type the API never declared
		// must not be a token source whatever the binder would make of it.
		{
			name: "token only in an xml body",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "", `<login><email>a@example.com</email><password>Wrong1</password><Token>` + token + `</Token></login>`, "application/xml", ""
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "token only in a vendor +json body member",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "", `{"email":"a@example.com","password":"Wrong1","csrf_token":"` + token + `","Token":"` + token + `"}`, "application/vnd.api+json", ""
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "form content type with a charset parameter",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "", url.Values{"email": {"a@example.com"}, "password": {"Wrong1"}, "csrf_token": {token}}.Encode(), "application/x-www-form-urlencoded; charset=UTF-8", ""
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "urlencoded body token beside a wrong query token",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "?csrf_token=not-the-token", url.Values{"email": {"a@example.com"}, "password": {"Wrong1"}, "csrf_token": {token}}.Encode(), "application/x-www-form-urlencoded", ""
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "urlencoded body token",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "", url.Values{"email": {"a@example.com"}, "password": {"Wrong1"}, "csrf_token": {token}}.Encode(), "application/x-www-form-urlencoded", ""
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "mixed-case form content type",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "", url.Values{"email": {"a@example.com"}, "password": {"Wrong1"}, "csrf_token": {token}}.Encode(), "Application/X-WWW-Form-Urlencoded", ""
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "multipart body token",
			build: func(t *testing.T, token string) (string, string, string, string) {
				body, contentType := multipartWith(t, map[string]string{"email": "a@example.com", "password": "Wrong1", "csrf_token": token})
				return "", body, contentType, ""
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "header token with a json body",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "", `{"email":"a@example.com","password":"Wrong1"}`, "application/json", token
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "header token beside a wrong query token",
			build: func(_ *testing.T, token string) (string, string, string, string) {
				return "?csrf_token=not-the-token", `{"email":"a@example.com","password":"Wrong1"}`, "application/json", token
			},
			wantStatus: http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, _ := newOnboardingTestAppWithCSRF(t)
			token, cookieHeader := extractCSRFCookieAndToken(t, app)

			query, body, contentType, header := tc.build(t, token)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions"+query, strings.NewReader(body))
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Cookie", cookieHeader)
			if header != "" {
				req.Header.Set("X-CSRF-Token", header)
			}
			resp, err := app.Test(req, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			// A request that clears CSRF reaches the sign-in handler, which
			// answers the deliberately wrong password with 401.
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

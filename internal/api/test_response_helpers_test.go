package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func responseCookieValue(cookies []*http.Cookie, name string) string {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

func responseCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func readAPIError(t *testing.T, body io.Reader) string {
	t.Helper()

	payload := map[string]string{}
	bytes, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if err := json.Unmarshal(bytes, &payload); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return payload["error"]
}

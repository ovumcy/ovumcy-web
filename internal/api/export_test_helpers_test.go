package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newExportRequestForTest(t *testing.T, target string, authCookie string) *http.Request {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("Cookie", authCookie)
	sendRequestTimezoneForTest(request, testRequestTimezone)
	return request
}

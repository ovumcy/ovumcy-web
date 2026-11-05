package main

import (
	"regexp"
	"strings"
	"testing"
)

// TestRequestLoggerFormatCarriesNoClientIdentity pins the operator access log
// to the fields the privacy documentation promises: time, status, latency,
// method, the redacted route template and the redacted error. A client address,
// user agent, raw URL or any header or query value is not among them. The
// calendar-feed telemetry regression in internal/api keeps a copy of this
// format, so a change here that adds a field must be decided on both sides.
func TestRequestLoggerFormatCarriesNoClientIdentity(t *testing.T) {
	allowed := map[string]bool{
		"time": true, "status": true, "latency": true, "method": true,
		"request_path": true, "safe_error": true,
	}
	tags := regexp.MustCompile(`\$\{([^}]*)\}`).FindAllStringSubmatch(requestLoggerFormat, -1)
	if len(tags) == 0 {
		t.Fatal("the request-log format has no tags, so the pin measures nothing")
	}
	for _, tag := range tags {
		name := strings.SplitN(tag[1], ":", 2)[0]
		if !allowed[name] {
			t.Fatalf("the request-log format carries ${%s}; only %v are by design", tag[1], allowed)
		}
	}
	for _, forbidden := range []string{"${ip}", "${ips}", "${ua}", "${url}", "${path}", "${reqHeader", "${header", "${queryParams}"} {
		if strings.Contains(requestLoggerFormat, forbidden) {
			t.Fatalf("the request-log format carries %s", forbidden)
		}
	}
}

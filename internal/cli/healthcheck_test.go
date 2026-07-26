package cli

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProbeHealthEndpointSucceedsOn200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	if err := probeHealthEndpoint(server.URL+healthcheckPath, time.Second); err != nil {
		t.Fatalf("expected nil for 200 response, got %v", err)
	}
}

func TestProbeHealthEndpointFailsOn500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	err := probeHealthEndpoint(server.URL+healthcheckPath, time.Second)
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected status code in error, got %v", err)
	}
}

func TestProbeHealthEndpointFailsOnUnreachableHost(t *testing.T) {
	err := probeHealthEndpoint("http://127.0.0.1:1/healthz", 200*time.Millisecond)
	if err == nil {
		t.Fatal("expected error when connecting to unreachable port, got nil")
	}
}

func TestProbeHealthEndpointHonorsTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	start := time.Now()
	err := probeHealthEndpoint(server.URL+healthcheckPath, 100*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 400*time.Millisecond {
		t.Fatalf("expected probe to abort within timeout, took %v", elapsed)
	}
}

func TestRunHealthcheckCommandHitsConfiguredPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	var requested string
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			requested = request.URL.Path
			writer.WriteHeader(http.StatusOK)
		}),
		ReadHeaderTimeout: time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	defer func() {
		_ = server.Close()
	}()

	parsed, err := url.Parse("http://" + listener.Addr().String())
	if err != nil {
		t.Fatalf("parse listener addr: %v", err)
	}
	port := parsed.Port()

	if err := RunHealthcheckCommand(port, time.Second); err != nil {
		t.Fatalf("expected healthcheck to succeed, got %v", err)
	}
	if requested != healthcheckPath {
		t.Fatalf("expected request path %q, got %q", healthcheckPath, requested)
	}
}

// TestRunReadycheckCommandProbesTheReadinessPath pins the one thing that must
// differ between the two CLI probes: the readiness command asks /readyz, and
// the healthcheck command (asserted above) keeps asking /healthz. Swapping them
// would put the container healthcheck on the storage-dependent probe and turn a
// database blip into a restart loop.
func TestRunReadycheckCommandProbesTheReadinessPath(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	var requested string
	server := &http.Server{
		Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			requested = request.URL.Path
			writer.WriteHeader(http.StatusOK)
		}),
		ReadHeaderTimeout: time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	defer func() {
		_ = server.Close()
	}()

	parsed, err := url.Parse("http://" + listener.Addr().String())
	if err != nil {
		t.Fatalf("parse listener addr: %v", err)
	}

	if err := RunReadycheckCommand(parsed.Port(), time.Second); err != nil {
		t.Fatalf("expected readycheck to succeed, got %v", err)
	}
	if requested != readycheckPath {
		t.Fatalf("expected request path %q, got %q", readycheckPath, requested)
	}
}

// TestRunReadycheckCommandFailsOnAnUnreadyResponse covers the exit code the
// operator actually reads: a 503 from /readyz must leave the command non-zero.
func TestRunReadycheckCommandFailsOnAnUnreadyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte(`{"status":"unavailable"}`))
	}))
	defer server.Close()

	err := probeHealthEndpoint(server.URL+readycheckPath, time.Second)
	if err == nil {
		t.Fatal("expected an error for a 503 readiness response, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected the status code in the error, got %v", err)
	}
	// Both CLI probes share this function, so the failure has to name the path
	// it probed — otherwise a readiness failure reads as a liveness failure.
	if !strings.Contains(err.Error(), readycheckPath) {
		t.Fatalf("expected the probed path in the error, got %v", err)
	}
}

func TestLocalProbeURLFallsBackToTheImageDefaultPort(t *testing.T) {
	if got := localProbeURL(healthcheckPath, "  "); got != "http://127.0.0.1:"+defaultHealthcheckPort+healthcheckPath {
		t.Fatalf("expected the default port for a blank PORT, got %q", got)
	}
	if got := localProbeURL(readycheckPath, "9123"); got != "http://127.0.0.1:9123"+readycheckPath {
		t.Fatalf("expected the configured port to be used, got %q", got)
	}
}

func TestProbeTimeoutOrDefaultReplacesNonPositiveTimeouts(t *testing.T) {
	if got := probeTimeoutOrDefault(0); got != defaultHealthcheckTimeout {
		t.Fatalf("expected the default timeout for 0, got %v", got)
	}
	if got := probeTimeoutOrDefault(3 * time.Second); got != 3*time.Second {
		t.Fatalf("expected an explicit timeout to be preserved, got %v", got)
	}
}

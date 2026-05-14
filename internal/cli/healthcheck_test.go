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

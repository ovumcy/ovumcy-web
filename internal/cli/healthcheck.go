package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	defaultHealthcheckPort    = "8080"
	defaultHealthcheckTimeout = 5 * time.Second
	healthcheckPath           = "/healthz"
	readycheckPath            = "/readyz"
)

// RunHealthcheckCommand probes the local server's /healthz endpoint and returns
// nil on a successful 2xx response. It is designed for use as a container
// healthcheck in scratch-based runtime images where no external HTTP client
// (curl/wget) is available.
func RunHealthcheckCommand(port string, timeout time.Duration) error {
	return probeHealthEndpoint(localProbeURL(healthcheckPath, port), probeTimeoutOrDefault(timeout))
}

// RunReadycheckCommand probes the local server's /readyz endpoint the same way,
// so an operator on a shell-free scratch image can ask whether the process can
// actually serve traffic rather than only whether it is alive. It is
// deliberately NOT the container healthcheck: that stays on /healthz so a
// transient storage failure cannot turn into a restart loop.
func RunReadycheckCommand(port string, timeout time.Duration) error {
	return probeHealthEndpoint(localProbeURL(readycheckPath, port), probeTimeoutOrDefault(timeout))
}

// localProbeURL addresses the probe at the loopback listener, falling back to
// the image's default port when PORT is unset.
func localProbeURL(path string, port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		port = defaultHealthcheckPort
	}
	return "http://127.0.0.1:" + port + path
}

func probeTimeoutOrDefault(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultHealthcheckTimeout
	}
	return timeout
}

func probeHealthEndpoint(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build healthcheck request: %w", err)
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true,
			DialContext: (&net.Dialer{
				Timeout: timeout,
			}).DialContext,
		},
	}
	response, err := client.Do(request)
	if err != nil {
		// The message names the probed path, not the command: this function
		// now serves both healthcheck and readycheck, and an operator reading
		// a failure mid-incident needs to know which probe answered.
		return fmt.Errorf("probe %s failed: %w", url, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("probe %s returned status %d", url, response.StatusCode)
	}
	return nil
}

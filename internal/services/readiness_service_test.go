package services

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubStorageProbe records the context each probe ran under so a test can
// inspect the deadline the service imposed, and can be told to fail or to block
// until its context is cancelled.
type stubStorageProbe struct {
	err           error
	blockUntilCtx bool
	calls         int
	lastDeadline  time.Time
	hadDeadline   bool
}

func (probe *stubStorageProbe) Ping(ctx context.Context) error {
	probe.calls++
	probe.lastDeadline, probe.hadDeadline = ctx.Deadline()
	if probe.blockUntilCtx {
		<-ctx.Done()
		return ctx.Err()
	}
	return probe.err
}

func TestReadinessServiceReportsReadyWhenStorageAnswers(t *testing.T) {
	probe := &stubStorageProbe{}
	service := NewReadinessService(probe)

	if err := service.CheckStorage(context.Background()); err != nil {
		t.Fatalf("CheckStorage() unexpected error: %v", err)
	}
	if probe.calls != 1 {
		t.Fatalf("expected exactly one storage probe, got %d", probe.calls)
	}
}

// TestReadinessServiceSurfacesTheStorageError pins that the verdict comes from
// the storage layer and is returned unchanged. The transport layer decides what
// (nothing) of it reaches a response body; the service must not pre-swallow it,
// or a caller could never tell a failed probe from a passing one.
func TestReadinessServiceSurfacesTheStorageError(t *testing.T) {
	storageErr := errors.New("storage unreachable")
	service := NewReadinessService(&stubStorageProbe{err: storageErr})

	if err := service.CheckStorage(context.Background()); !errors.Is(err, storageErr) {
		t.Fatalf("expected the storage error to surface, got %v", err)
	}
}

// TestReadinessServiceBoundsAStalledProbe is the reason the service owns a
// deadline at all: a storage engine that accepts the query and never answers
// would otherwise hold the readiness request open indefinitely, which reads to
// an orchestrator as a hang rather than as "not ready".
func TestReadinessServiceBoundsAStalledProbe(t *testing.T) {
	probe := &stubStorageProbe{blockUntilCtx: true}
	service := NewReadinessService(probe)
	service.ConfigureProbeTimeout(20 * time.Millisecond)

	start := time.Now()
	err := service.CheckStorage(context.Background())
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the probe deadline to fire, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("expected the probe to abort at its own deadline, took %v", elapsed)
	}
}

// TestReadinessServiceKeepsItsDefaultTimeoutForNonPositiveValues covers the
// guard on the configuration seam: a miswired caller passing zero must not be
// able to remove the bound the previous test relies on.
func TestReadinessServiceKeepsItsDefaultTimeoutForNonPositiveValues(t *testing.T) {
	probe := &stubStorageProbe{}
	service := NewReadinessService(probe)
	service.ConfigureProbeTimeout(0)

	if err := service.CheckStorage(context.Background()); err != nil {
		t.Fatalf("CheckStorage() unexpected error: %v", err)
	}
	if !probe.hadDeadline {
		t.Fatal("expected the probe context to carry a deadline")
	}
	if remaining := time.Until(probe.lastDeadline); remaining > DefaultReadinessProbeTimeout {
		t.Fatalf("expected the default probe timeout to stay in force, got %v remaining", remaining)
	}
}

// TestReadinessServiceInheritsTheCallerCancellation proves the request context
// is threaded rather than replaced: a client that disconnects mid-probe must
// cancel the storage work it started.
func TestReadinessServiceInheritsTheCallerCancellation(t *testing.T) {
	service := NewReadinessService(&stubStorageProbe{blockUntilCtx: true})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.CheckStorage(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the caller cancellation to reach the probe, got %v", err)
	}
}

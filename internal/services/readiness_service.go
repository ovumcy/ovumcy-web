package services

import (
	"context"
	"time"
)

// StorageProbe is the narrow persistence port the readiness check needs: prove
// the storage engine answers, return nothing. It is deliberately not one of the
// data repositories — a readiness probe must never be able to read owner data.
type StorageProbe interface {
	Ping(ctx context.Context) error
}

// DefaultReadinessProbeTimeout bounds a single storage probe. A readiness
// endpoint that blocks is indistinguishable from one that never answers, so the
// probe fails fast rather than holding the request open behind a stalled
// engine; the caller reads a refused probe as "not ready", never as an error to
// surface.
const DefaultReadinessProbeTimeout = time.Second

// ReadinessService answers whether the process can serve traffic, as opposed to
// merely being alive. Today that is exactly one question — does persistence
// answer — because storage is the only dependency whose loss leaves the process
// running and every request failing.
type ReadinessService struct {
	storage      StorageProbe
	probeTimeout time.Duration
}

func NewReadinessService(storage StorageProbe) *ReadinessService {
	return &ReadinessService{storage: storage, probeTimeout: DefaultReadinessProbeTimeout}
}

// ConfigureProbeTimeout overrides the per-probe deadline. A non-positive value
// is ignored so a miswired caller cannot remove the bound entirely.
func (service *ReadinessService) ConfigureProbeTimeout(timeout time.Duration) {
	if timeout <= 0 {
		return
	}
	service.probeTimeout = timeout
}

// CheckStorage reports the storage probe's verdict: nil when the engine
// answered within the deadline, otherwise the underlying error. The error is
// for the caller's own logging decision — it carries driver detail and must
// never reach a response body.
func (service *ReadinessService) CheckStorage(ctx context.Context) error {
	probeCtx, cancel := context.WithTimeout(ctx, service.probeTimeout)
	defer cancel()
	return service.storage.Ping(probeCtx)
}

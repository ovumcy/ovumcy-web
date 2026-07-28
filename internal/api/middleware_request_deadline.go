package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

// RequestBudget bounds how long one request may spend inside the app.
//
// Without it there is no deadline anywhere in the request path. Fiber v3's
// c.Context() returns context.Background() until something calls SetContext, so
// every ctx threaded handler → service → repository carried neither a deadline
// nor a cancellation, and database/sql waits for a free connection forever.
// fasthttp does not cancel a handler when the client goes away either, so work
// the caller has abandoned keeps running: a burst of 120 concurrent day writes
// held a stand unready for 16.6 minutes after the last client had given up,
// answering 500 with latencies past 14 minutes, while /healthz stayed green at
// 1 ms and the container kept reporting healthy.
//
// The budget is a constant rather than an operator knob for the same reason a
// READ_BUFFER_SIZE knob is declined: it can be set wrong in both directions.
// 60s is sized by the widest legitimate request and by nothing else — a
// full-size JSON restore, services.MaxImportEntries records inside the 16 MiB
// body limit — which it leaves far inside the bound.
//
// In particular it is NOT derived from the server's WriteTimeout, which carries
// the same 60s and bounds something else entirely. fasthttp arms the write
// deadline AFTER the handler returns (fasthttp v1.72.0 server.go: s.Handler(ctx)
// at 2618, SetWriteDeadline at 2637), so WriteTimeout caps writing a finished
// response to the socket, never producing it. A handler that runs for minutes
// still delivers: the abandoned writes that motivated this guard answered with
// latencies past 14m40s under exactly that WriteTimeout, which is why an
// in-process budget had to exist at all.
//
// The two constants are therefore independent — lowering WriteTimeout does not
// shrink this budget, raising it does not license a longer one — and are
// deliberately not pinned to each other, unlike the BodyLimit ↔
// services.MaxImportEntries pair, where the second really does size the first.
// What is pinned instead is the fasthttp behaviour this reasoning rests on:
// TestWriteTimeoutBoundsTheResponseWriteNotTheHandler (cmd/ovumcy) fails on an
// upgrade that moves the deadline ahead of the handler, rather than letting
// WriteTimeout quietly change meaning underneath the comment.
const RequestBudget = 60 * time.Second

// responseHeaderBaselines pools the response-header snapshot RequestDeadlineGuard
// takes before it hands a request to its handler.
//
// The snapshot has to be a copy: fasthttp reuses the header's backing buffers,
// so the []byte a visitor sees before c.Next() no longer holds the same value
// after it. Pooling keeps that copy off the allocator on a path every single
// request walks — a full CopyTo of the response headers the composition root
// sets (eight security headers plus the CSRF cookie) measures ~150ns and zero
// allocations per request from the pool, against ~1.4us and 20 allocations for
// a fresh header each time. Entries are Reset before they go back, so a pooled
// header does not sit on the previous request's Set-Cookie between uses.
var responseHeaderBaselines = sync.Pool{New: func() any { return new(fasthttp.ResponseHeader) }}

// RequestDeadlineGuard gives every request a deadline and answers the one it
// cannot finish inside as a mapped 503.
//
// It is registered ahead of the routes so the deadline reaches every handler,
// including the ones that never read a body. The answer is decided after
// c.Next() rather than by inspecting each domain error, mirroring
// requestBodyLimitGuard: a deadline that expires surfaces deep in a repository
// call as an opaque driver error and would otherwise be mapped by whichever
// domain happened to catch it — the day upsert turns it into
// "failed to update day", a 500. One place, one status, and no domain needs to
// learn about deadlines.
//
// 503 rather than 500 because the condition is transient and the caller's
// request was not malformed: retrying later is exactly the right response.
//
// Answering also means discarding what the handler had already put on the
// response — its headers as much as its status and body. See the rollback
// below.
//
// budget is a parameter so the expiry path is reachable in a test without a
// wall-clock wait; the composition root passes RequestBudget. A non-positive
// value falls back to it rather than being honoured, so a miswired caller
// cannot remove the bound — the same guard ReadinessService puts on its own
// probe timeout.
func RequestDeadlineGuard(budget time.Duration) fiber.Handler {
	if budget <= 0 {
		budget = RequestBudget
	}
	return func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), budget)
		defer cancel()
		c.SetContext(ctx)

		baseline := responseHeaderBaselines.Get().(*fasthttp.ResponseHeader)
		c.Response().Header.CopyTo(baseline)
		defer func() {
			baseline.Reset()
			responseHeaderBaselines.Put(baseline)
		}()

		err := c.Next()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			// Roll the response headers back to the moment the guard took over,
			// then answer. Replacing only the status and the body left every
			// header the handler had already set standing under the new status:
			// a sign-in or a step-up whose work committed a millisecond after
			// the budget ran out still shipped Set-Cookie and Location beside a
			// 503 that says the request was abandoned. request_timeout invites a
			// retry, so the client acts on "nothing happened" while holding the
			// session it was just told it never got.
			//
			// The line between the handler's headers and the app's own is where
			// this guard sits, not a list of names. Everything the composition
			// root contributes — the security headers, the CSRF cookie — is
			// already on the response when the guard runs and survives the
			// rollback; everything added during c.Next() goes. A denylist of
			// Set-Cookie and Location would need hand-extending for each header
			// a later handler learns to set: the calendar feed's own
			// Cache-Control alone would leave a 503 privately cacheable for an
			// hour.
			baseline.CopyTo(&c.Response().Header)
			return RespondRequestTimeout(c)
		}
		return err
	}
}

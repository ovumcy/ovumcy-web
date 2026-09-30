package bootstrap

import (
	"testing"
	"time"
)

// TestPassContextCarriesTheStorageBudget is the direct assertion on the helper
// the boot passes and the operator CLI share: it must hand back a context that
// expires, and at the declared budget rather than at a literal of its own.
// Removing the WithTimeout leaves no deadline; replacing the constant with a
// smaller or larger literal falls outside the window below.
func TestPassContextCarriesTheStorageBudget(t *testing.T) {
	t.Parallel()

	ctx, cancel := PassContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("a storage pass must run under a deadline; this context has none, so a database that never answers hangs the caller with no refusal")
	}
	remaining := time.Until(deadline)
	const slack = 30 * time.Second
	if remaining > PassStorageBudget {
		t.Fatalf("deadline is %s away, further than the declared budget of %s", remaining, PassStorageBudget)
	}
	if remaining < PassStorageBudget-slack {
		t.Fatalf("deadline is only %s away against a declared budget of %s; the helper is not using the constant it documents", remaining, PassStorageBudget)
	}
}

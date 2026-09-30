package bootstrap

import (
	"context"
	"time"
)

// PassStorageBudget bounds how long any one boot repair or sentinel pass may
// wait on storage before the boot stops waiting for it. The operator CLI runs
// the same schema check under it, so a stalled database cannot hang a
// subcommand any longer than it can hang the boot.
//
// The passes run after migrations and before any listener exists, each on its
// own context. Without a deadline a database that accepts the call and then
// never answers — a SQLite file locked by another process, a Postgres endpoint
// that connects and stalls — leaves the process alive and silent forever: no
// listener, no refusal, and a container healthcheck that can only keep
// reporting "starting". That is strictly worse than failing, because an
// operator can read a refusal and cannot read a hang.
//
// The value is deliberately far above any plausible pass, because the failure
// this introduces is the opposite one: a budget set near the real cost turns a
// slow start into a broken start. The heaviest pass, the luteal recompute, reads
// every owner's day logs once, and on the SQLite baseline that is a handful of
// accounts over a few thousand rows with the write lock uncontended, since
// nothing is serving yet. Five minutes is around two orders of magnitude of headroom, so reaching
// it means storage is stuck rather than slow — which is the case each pass's own
// failure policy should then decide, and they decide it differently: the must*
// wrappers stop the boot, the luteal recompute logs and lets the server start.
//
// The budget is PER PASS and the passes run in sequence, so the boot's own worst
// case is this value times the number of passes that call PassContext, not
// this value alone. Size a healthcheck start period or a deployment timeout
// against that product, not against this constant.
//
// It also makes every pass one that can stop half-done, which each pass must
// already survive, and each does: the schema check only reads; the restore
// fence records its token only after disarming, so an interrupted run meets the
// same mismatch next boot; the feed sentinel records its epoch only after
// disarming, so an interrupted run re-detects the rotation next boot; the
// email repair leaves its marker unwritten and every rewrite is idempotent; the
// luteal recompute counts a cut-off row as a failure, which withholds the marker
// for the same reason. A pass added here that writes its marker first, or whose
// per-row work is not idempotent, cannot take this budget as given — the budget
// assumes interruptibility that such a pass would not have.
const PassStorageBudget = 5 * time.Minute

// PassContext returns the bounded context a storage pass runs under, together
// with the cancel its caller must defer. It exists so the boot passes and the
// operator CLI cannot drift apart on the budget or on whether they have one at
// all — an inline copy of the same WithTimeout in each is the shape that drifts.
func PassContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), PassStorageBudget)
}

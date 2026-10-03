package services

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// evictEveryN triggers a full-map stale-key sweep every N admitted reservations.
	// Keys are partly attacker-influenced (identity:<hmac>), so without periodic
	// eviction the map grows unboundedly until process restart. N=128 keeps the
	// sweep rare enough to be O(1) amortised while bounding residual memory.
	evictEveryN = 128

	// evictAboveSize caps the tracked keys PER SCOPE that are not enforcing a
	// lockout. It is also the headroom the sweep trigger allows above the pinned
	// population the previous sweep could not remove (AttemptLimiter.sweepAbove),
	// and after the stale sweep each scope is trimmed back to this many unpinned entries —
	// a stale sweep alone cannot shrink the map when an attacker keeps minting
	// fresh keys inside the window. Two things the cap deliberately does not
	// count: a lockout (see attemptEntry.blocked), because evicting one lifts
	// it, and another scope's entries, because the cheapest flood — the login
	// form accepts any email — must not be able to erase a TOTP or recovery
	// budget. What bounds the rest is the attacker's own spend: a lockout costs
	// `limit` refused requests inside one window, and the scopes are the fixed
	// handful bootstrap wires.
	evictAboveSize = 1024
)

// AttemptBudget is what a policy judges its keys by. The limiter stores it on
// every entry recorded under that policy, because it is shared by policies with
// different budgets (login 8/15m, recovery 8/1h, totp 5/15m): a sweep or a
// trim that judged every entry by the calling policy's window would erase a
// budget that was still live under its own.
type AttemptBudget struct {
	Scope  string
	Limit  int
	Window time.Duration
}

type attemptEntry struct {
	// times is ordered oldest-first; pruneLocked keeps only the failures inside
	// the caller's window, so the newest is always last.
	times  []time.Time
	budget AttemptBudget
	// gen identifies this incarnation of the entry. It is assigned when the key
	// first gets an attempt after having none, and kept while attempts are
	// added to it, so a refund can tell the entry its reservation booked into
	// from one a reset or a window lapse removed and a later failure re-created.
	gen uint64
}

func (entry attemptEntry) newest() time.Time {
	return entry.times[len(entry.times)-1]
}

// expired reports whether the entry's own window has lapsed since its newest
// failure, so nothing in it can still count toward a lockout.
func (entry attemptEntry) expired(now time.Time) bool {
	return !entry.newest().Add(entry.budget.Window).After(now)
}

// blocked reports whether the entry currently enforces a lockout: at least
// `Limit` failures inside its own window. A blocked entry is pinned — neither
// the stale sweep nor the size-cap trim removes it — because evicting it would
// let the next attempt through, and minting fresh keys is the cheapest thing an
// attacker can do to this map.
func (entry attemptEntry) blocked(now time.Time) bool {
	if entry.budget.Limit < 1 {
		return false
	}
	threshold := now.Add(-entry.budget.Window)
	live := 0
	for _, value := range entry.times {
		if value.After(threshold) {
			live++
		}
	}
	return live >= entry.budget.Limit
}

type AttemptLimiter struct {
	mu        sync.Mutex
	attempts  map[string]attemptEntry
	addCallsN int // counts admitted reservations for eviction pacing
	nextGen   uint64
	// sweepAbove is the map size that re-triggers a sweep ahead of the call
	// counter. It is recomputed after every sweep as "the entries the sweep kept,
	// plus the room left under the cap in the fullest scope". What a sweep keeps
	// includes every live lockout, pinned for its whole window, and up to a cap's
	// worth of unpinned entries in EACH scope. Fixed at the absolute
	// evictAboveSize, or at the pinned count plus one cap, the trigger would stay
	// true while nothing is over its cap — past the first lockout beyond the cap,
	// or with two scopes partly full — and every later add would pay a full O(n)
	// sweep and an O(n) blocked() pass under the single mutex. The headroom is
	// the fullest scope's remaining room, so no scope can pass its cap before the
	// trigger fires: the cap on unpinned entries holds at every observable moment.
	sweepAbove int
}

func NewAttemptLimiter() *AttemptLimiter {
	return &AttemptLimiter{
		attempts:   make(map[string]attemptEntry),
		sweepAbove: evictAboveSize,
	}
}

func NormalizeLimiterKey(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "unknown"
	}
	return key
}

// AttemptReservation is the provisional attempt Reserve booked under every key
// of one request. The attempt is counted from the moment it is reserved, so a
// request that is still comparing a credential already holds its slot. The
// caller settles it once the compare is over: a compare that failed leaves the
// reservation booked (it is that failure), anything else — a correct credential,
// or a request refused before it spent a compare — gives the slot back with
// Refund.
type AttemptReservation struct {
	limiter *AttemptLimiter
	holds   []attemptHold
	at      time.Time
	// settled is guarded by limiter.mu: a reservation is refunded at most once.
	settled bool
}

// attemptHold names one booked attempt: the key and the generation of the entry
// it was appended to, so a refund never reaches an entry that was cleared and
// re-created since (see attemptEntry.gen).
type attemptHold struct {
	key string
	gen uint64
}

// Reserve is the one admission path of a budgeted credential check. Under a
// single hold of the mutex it refuses when ANY key already carries `Limit`
// attempts inside the window — recording nothing, so a refusal never feeds a
// bucket — and otherwise books one provisional attempt at `now` under every key
// and admits the request. Counting and booking in one critical section is what
// makes the limit a bound on the compares that run: with limit L, at most L
// reservations are admitted per key inside one window while its entry is
// tracked, however many requests arrive at once. A check followed by a separate
// booking after the compare leaves every request that arrives inside the
// compare unbooked. The bound is not absolute: the size-cap trim
// (enforceSizeCapLocked) may evict an entry still short of its limit when one
// scope sees more than evictAboveSize distinct fresh keys, which resets that
// key's count. Only an entry already at its limit is pinned. That is the
// documented trade-off for keeping the map bounded under a flood of minted keys.
//
// ok is false when the budget is spent; the reservation is nil then.
func (limiter *AttemptLimiter) Reserve(keys []string, now time.Time, budget AttemptBudget) (*AttemptReservation, bool) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	normalized := normalizeLimiterKeys(keys)
	if limiter.atLimitLocked(normalized, now, budget.Limit, budget.Window) {
		return nil, false
	}
	return &AttemptReservation{
		limiter: limiter,
		holds:   limiter.bookLocked(normalized, now, budget),
		at:      now,
	}, true
}

// Refund gives the reserved slot back: it removes the one attempt this
// reservation booked from each key. It is the success path of a compare, and of
// a request refused before it spent one. It is idempotent, and it is bounded by
// what the reservation itself booked: an entry that was cleared since (a reset
// after a committed action) or has aged out of its window holds nothing to
// remove and is left alone, and a failure booked by another request is never
// the one removed because the entry's generation must match. Safe on a nil
// reservation.
func (reservation *AttemptReservation) Refund() {
	if reservation == nil {
		return
	}
	limiter := reservation.limiter
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if reservation.settled {
		return
	}
	reservation.settled = true

	for _, hold := range reservation.holds {
		entry, ok := limiter.attempts[hold.key]
		if !ok || entry.gen != hold.gen {
			continue
		}
		index := -1
		for position, value := range entry.times {
			if value.Equal(reservation.at) {
				index = position
				break
			}
		}
		if index < 0 {
			continue
		}
		remaining := make([]time.Time, 0, len(entry.times)-1)
		remaining = append(remaining, entry.times[:index]...)
		remaining = append(remaining, entry.times[index+1:]...)
		if len(remaining) == 0 {
			delete(limiter.attempts, hold.key)
			continue
		}
		entry.times = remaining
		limiter.attempts[hold.key] = entry
	}
}

// atLimitLocked reports whether any of the (normalized) keys already carries
// `limit` attempts inside the window. Must be called with limiter.mu held.
func (limiter *AttemptLimiter) atLimitLocked(keys []string, now time.Time, limit int, window time.Duration) bool {
	for _, key := range keys {
		if len(limiter.pruneLocked(key, now, window)) >= limit {
			return true
		}
	}
	return false
}

// bookLocked appends one attempt at `now` under every (normalized) key, judged
// by budget, paces the eviction sweep and returns what it booked. Must be
// called with limiter.mu held.
func (limiter *AttemptLimiter) bookLocked(keys []string, now time.Time, budget AttemptBudget) []attemptHold {
	holds := make([]attemptHold, 0, len(keys))
	for _, key := range keys {
		pruned := limiter.pruneLocked(key, now, budget.Window)
		gen := limiter.attempts[key].gen
		if len(pruned) == 0 {
			limiter.nextGen++
			gen = limiter.nextGen
		}
		limiter.attempts[key] = attemptEntry{
			times:  append(pruned, now),
			budget: budget,
			gen:    gen,
		}
		holds = append(holds, attemptHold{key: key, gen: gen})
	}

	limiter.addCallsN++
	limiter.maybeEvictStaleLocked(now)
	return holds
}

// maybeEvictStaleLocked performs an opportunistic full-map sweep to remove
// entries whose own window has lapsed, then trims every scope back to the
// evictAboveSize cap. It fires when either the call counter reaches evictEveryN
// or the map grows past sweepAbove. Both triggers are reset by the sweep, so
// neither a pinned population however large nor several scopes each under their
// cap makes the size trigger true on its own: what still does is a scope at its
// cap, which is the flood the trim answers. Must be called with limiter.mu held.
func (limiter *AttemptLimiter) maybeEvictStaleLocked(now time.Time) {
	if limiter.addCallsN < evictEveryN && len(limiter.attempts) < limiter.sweepAbove {
		return
	}
	limiter.addCallsN = 0

	for key, entry := range limiter.attempts {
		if len(entry.times) == 0 {
			delete(limiter.attempts, key) // codecov:ignore -- defensive; pruneLocked never stores an empty slice in the map
			continue
		}
		if entry.expired(now) {
			delete(limiter.attempts, key)
		}
	}

	headroom := limiter.enforceSizeCapLocked(now)
	limiter.sweepAbove = len(limiter.attempts) + headroom
}

// enforceSizeCapLocked bounds every scope at evictAboveSize entries that are
// not enforcing a lockout, evicting the ones with the oldest most-recent
// failure first. Evicting the coldest loses the least: they are the closest to
// ageing out naturally, while the key an attacker is working on is among the
// freshest and is evicted last — so lifting one partial budget costs a full
// scope's worth of fresher keys, again for every guess. It returns the room
// left under the cap in the fullest scope, which is what the next sweep trigger
// gets as headroom. Must be called with limiter.mu held.
func (limiter *AttemptLimiter) enforceSizeCapLocked(now time.Time) int {
	if len(limiter.attempts) <= evictAboveSize {
		// No scope can hold more entries than the whole map.
		return evictAboveSize - len(limiter.attempts)
	}

	type candidate struct {
		key    string
		newest time.Time
	}
	perScope := make(map[string][]candidate)
	for key, entry := range limiter.attempts {
		if entry.blocked(now) {
			continue
		}
		perScope[entry.budget.Scope] = append(perScope[entry.budget.Scope], candidate{key: key, newest: entry.newest()})
	}
	fullest := 0
	for _, candidates := range perScope {
		excess := len(candidates) - evictAboveSize
		fullest = max(fullest, min(len(candidates), evictAboveSize))
		if excess <= 0 {
			continue
		}
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].newest.Before(candidates[j].newest)
		})
		for _, entry := range candidates[:excess] {
			delete(limiter.attempts, entry.key)
		}
	}
	return evictAboveSize - fullest
}

func (limiter *AttemptLimiter) ResetAll(keys []string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	for _, key := range normalizeLimiterKeys(keys) {
		delete(limiter.attempts, key)
	}
}

func normalizeLimiterKeys(keys []string) []string {
	if len(keys) == 0 {
		return []string{NormalizeLimiterKey("")}
	}

	seen := make(map[string]struct{}, len(keys))
	normalized := make([]string, 0, len(keys))
	for _, key := range keys {
		candidate := NormalizeLimiterKey(key)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		normalized = append(normalized, candidate)
	}
	if len(normalized) == 0 {
		return []string{NormalizeLimiterKey("")}
	}
	return normalized
}

// pruneLocked drops the failures outside the caller's window and returns what
// is left, keeping the entry's recorded budget. Keys are scope-prefixed by the
// policy that owns them, so the caller's window is the entry's own.
func (limiter *AttemptLimiter) pruneLocked(key string, now time.Time, window time.Duration) []time.Time {
	entry, ok := limiter.attempts[key]
	if !ok || len(entry.times) == 0 {
		return []time.Time{}
	}

	threshold := now.Add(-window)
	pruned := make([]time.Time, 0, len(entry.times))
	for _, value := range entry.times {
		if value.After(threshold) {
			pruned = append(pruned, value)
		}
	}

	if len(pruned) == 0 {
		delete(limiter.attempts, key)
		return []time.Time{}
	}

	entry.times = pruned
	limiter.attempts[key] = entry
	return pruned
}

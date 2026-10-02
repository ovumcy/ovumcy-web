package services

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// burstSize is how many requests one burst throws at a single account; it is
// well above every limit below so a limit that does not bind shows as extra
// compares, not as luck.
const burstSize = 48

// runBurst releases n callers at once and returns what each got back.
func runBurst(n int, call func(index int) error) []error {
	results := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for index := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[index] = call(index)
		}()
	}
	close(start)
	wg.Wait()
	return results
}

func countErrors(results []error, target error) int {
	count := 0
	for _, err := range results {
		if errors.Is(err, target) {
			count++
		}
	}
	return count
}

// slowCompare stands in for a bcrypt compare: long enough that every request of
// a burst is past its admission before the first compare returns.
func slowCompare() { time.Sleep(10 * time.Millisecond) }

// countingLoginAuth counts the credential comparisons a login performs and
// fails every one of them.
type countingLoginAuth struct {
	compares atomic.Int64
	err      error
}

func (fake *countingLoginAuth) AuthenticateCredentials(context.Context, string, string) (models.User, error) {
	fake.compares.Add(1)
	slowCompare()
	if fake.err != nil {
		return models.User{}, fake.err
	}
	return models.User{ID: 1}, nil
}

func TestAttemptLimiterReserveAdmitsExactlyTheLimitUnderABurst(t *testing.T) {
	const limit = 5
	limiter := NewAttemptLimiter()
	budget := AttemptBudget{Scope: "burst", Limit: limit, Window: time.Hour}
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	var admitted atomic.Int64
	runBurst(burstSize, func(int) error {
		if _, ok := limiter.Reserve([]string{"burst:identity:one"}, now, budget); ok {
			admitted.Add(1)
		}
		return nil
	})

	if got := admitted.Load(); got != limit {
		t.Fatalf("admitted %d of %d concurrent reservations, want exactly the limit %d", got, burstSize, limit)
	}
}

func TestLoginAuthenticateRunsNoMoreComparesThanTheLimitUnderABurst(t *testing.T) {
	const limit = 3
	auth := &countingLoginAuth{err: ErrAuthInvalidCreds}
	service := NewLoginService(auth, &stubLoginResetTokenIssuer{}, NewAttemptLimiter())
	service.ConfigureAttemptLimits(limit, time.Hour)
	secretKey := []byte("burst-secret-key-0123456789abcdef")

	// One account, a different address per request: only the account bucket
	// can bound this.
	results := runBurst(burstSize, func(index int) error {
		_, err := service.Authenticate(context.Background(), secretKey, "198.51.100."+string(rune('A'+index)), "owner@example.com", "wrong", loginServiceTestTTL, loginServiceTestNow)
		return err
	})

	if got := auth.compares.Load(); got > limit {
		t.Fatalf("%d credential comparisons ran against one account with limit %d", got, limit)
	}
	if got := countErrors(results, ErrAuthInvalidCreds); got != limit {
		t.Fatalf("%d requests were answered with a failed credential, want %d", got, limit)
	}
	if got := countErrors(results, ErrAuthLoginRateLimited); got != burstSize-limit {
		t.Fatalf("%d requests were rate limited, want %d", got, burstSize-limit)
	}
}

func TestReauthVerifyRunsNoMoreComparesThanTheLimitUnderABurst(t *testing.T) {
	const limit = 4
	secretKey := []byte("burst-secret-key-0123456789abcdef")
	settings := NewSettingsService(nil)
	settings.ConfigureReauthAttempts(secretKey, NewAttemptLimiter(), limit, time.Hour)
	budget := settings.SettingsReauthBudget()
	var compares atomic.Int64

	results := runBurst(burstSize, func(index int) error {
		attempt := ReauthAttempt{ClientKey: "203.0.113." + string(rune('A'+index)), UserID: 9, Now: time.Now()}
		return budget.verify(attempt, func() error {
			compares.Add(1)
			slowCompare()
			return ErrSettingsPasswordInvalid
		})
	})

	if got := compares.Load(); got > limit {
		t.Fatalf("%d re-auth comparisons ran against one account with limit %d", got, limit)
	}
	if got := countErrors(results, ErrSettingsPasswordInvalid); got != limit {
		t.Fatalf("%d requests were answered with a failed password, want %d", got, limit)
	}
	if got := countErrors(results, ErrSettingsReauthRateLimited); got != burstSize-limit {
		t.Fatalf("%d requests were rate limited, want %d", got, burstSize-limit)
	}
}

func TestTOTPReserveAttemptRunsNoMoreComparesThanTheLimitUnderABurst(t *testing.T) {
	secretKey := []byte("burst-secret-key-0123456789abcdef")
	service := NewTOTPService(&stubTOTPUserRepo{}, secretKey, NewAttemptLimiter())
	var compares atomic.Int64

	results := runBurst(burstSize, func(index int) error {
		reservation, err := service.ReserveAttempt(secretKey, "192.0.2."+string(rune('A'+index)), 7, time.Now())
		if err != nil {
			return err
		}
		_ = reservation // a wrong code: the reservation stays booked
		compares.Add(1)
		slowCompare()
		return nil
	})

	if got := compares.Load(); got > DefaultTOTPAttemptsLimit {
		t.Fatalf("%d sign-in code comparisons ran against one account with limit %d", got, DefaultTOTPAttemptsLimit)
	}
	if got := countErrors(results, ErrTOTPRateLimited); got != burstSize-DefaultTOTPAttemptsLimit {
		t.Fatalf("%d requests were rate limited, want %d", got, burstSize-DefaultTOTPAttemptsLimit)
	}
}

func TestVerifyEnrollmentCodeRunsNoMoreComparesThanTheLimitUnderABurst(t *testing.T) {
	secretKey := []byte("burst-secret-key-0123456789abcdef")
	service := NewTOTPService(&stubTOTPUserRepo{}, secretKey, NewAttemptLimiter())
	budget := service.EnrollCodeBudget(secretKey)

	results := runBurst(burstSize, func(index int) error {
		attempt := ReauthAttempt{ClientKey: "192.0.2." + string(rune('A'+index)), UserID: 7, Now: time.Now()}
		_, err := service.VerifyEnrollmentCode(budget, attempt, "JBSWY3DPEHPK3PXP", "abcdef")
		return err
	})

	// Every ErrTOTPEnrollCodeInvalid is one comparison that ran and failed.
	if got := countErrors(results, ErrTOTPEnrollCodeInvalid); got != DefaultTOTPEnrollAttemptsLimit {
		t.Fatalf("%d enrollment codes were compared, want the limit %d", got, DefaultTOTPEnrollAttemptsLimit)
	}
	if got := countErrors(results, ErrTOTPEnrollRateLimited); got != burstSize-DefaultTOTPEnrollAttemptsLimit {
		t.Fatalf("%d requests were rate limited, want %d", got, burstSize-DefaultTOTPEnrollAttemptsLimit)
	}
}

func TestLogoutAttemptsAreBoundedUnderABurst(t *testing.T) {
	service := NewAuthService(nil)
	service.ConfigureLogoutAttemptLimits(6, time.Hour)
	secretKey := []byte("burst-secret-key-0123456789abcdef")

	var limited atomic.Int64
	runBurst(burstSize, func(index int) error {
		if service.CheckAndRecordLogoutAttempt(secretKey, "198.51.100."+string(rune('A'+index)), "31", loginServiceTestNow) {
			limited.Add(1)
		}
		return nil
	})

	if got := limited.Load(); got != burstSize-6 {
		t.Fatalf("%d logouts were refused, want %d", got, burstSize-6)
	}
}

func TestLoginAuthenticateGivesTheSlotBackWhenTheCompareDidNotFail(t *testing.T) {
	const limit = 3
	auth := &countingLoginAuth{err: ErrAuthInvalidCreds}
	service := NewLoginService(auth, &stubLoginResetTokenIssuer{}, NewAttemptLimiter())
	service.ConfigureAttemptLimits(limit, time.Hour)
	secretKey := []byte("slot-secret-key-0123456789abcdefg")
	authenticate := func() error {
		_, err := service.Authenticate(context.Background(), secretKey, "198.51.100.1", "owner@example.com", "pw", loginServiceTestTTL, loginServiceTestNow)
		return err
	}

	for range limit - 1 {
		if err := authenticate(); !errors.Is(err, ErrAuthInvalidCreds) {
			t.Fatalf("wrong password = %v, want ErrAuthInvalidCreds", err)
		}
	}
	// A correct password and a storage error are not failures: neither may keep
	// the slot it reserved.
	auth.err = nil
	if err := authenticate(); err != nil {
		t.Fatalf("correct password = %v, want success", err)
	}
	auth.err = errors.New("storage unavailable")
	if err := authenticate(); err == nil || errors.Is(err, ErrAuthLoginRateLimited) {
		t.Fatalf("storage error = %v, want it passed through", err)
	}
	auth.err = ErrAuthInvalidCreds
	if err := authenticate(); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("the limit-th wrong password = %v, want ErrAuthInvalidCreds: a success or an error kept its slot", err)
	}
	if err := authenticate(); !errors.Is(err, ErrAuthLoginRateLimited) {
		t.Fatalf("attempt past the limit = %v, want ErrAuthLoginRateLimited", err)
	}
}

func TestStartRecoveryGivesTheSlotBackWhenTheLookupErrors(t *testing.T) {
	repo := &stubAuthUserRepo{emailErr: errors.New("storage unavailable")}
	service := NewPasswordResetService(NewAuthService(repo), NewAttemptLimiter())
	service.ConfigureRecoveryAttemptLimits(2, time.Hour)
	secretKey := []byte("slot-secret-key-0123456789abcdefg")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	start := func(code string) error {
		_, err := service.StartRecovery(context.Background(), secretKey, "10.0.0.1", "owner@example.com", code, "StrongPass1", now, 30*time.Minute)
		return err
	}

	// A lookup that fails says nothing about the operands: none of these may
	// keep the slot it reserved.
	for range 6 {
		if err := start("OVUM-ABCD-2345-EFGH"); err == nil || errors.Is(err, ErrPasswordRecoveryRateLimited) {
			t.Fatalf("lookup error = %v, want it passed through", err)
		}
	}
	for range 2 {
		if err := start("invalid"); !errors.Is(err, ErrPasswordRecoveryCodeInvalid) {
			t.Fatalf("malformed code = %v, want ErrPasswordRecoveryCodeInvalid: the lookup errors kept their slots", err)
		}
	}
	if err := start("invalid"); !errors.Is(err, ErrPasswordRecoveryRateLimited) {
		t.Fatalf("attempt past the limit = %v, want ErrPasswordRecoveryRateLimited", err)
	}
}

func TestReauthVerifyGivesTheSlotBackOnACorrectPassword(t *testing.T) {
	fixture := newReauthBudgetFixture(t)
	budget := fixture.settings.SettingsReauthBudget()
	fixture.spend(t, budget, DefaultSettingsReauthAttemptsLimit-1)

	if err := fixture.settings.VerifyReauth(budget, fixture.attempt, fixture.user, reauthBudgetFixturePassword); err != nil {
		t.Fatalf("correct password = %v, want success", err)
	}
	fixture.spend(t, budget, 1)
	if err := fixture.settings.VerifyReauth(budget, fixture.attempt, fixture.user, "WrongPassword1"); !errors.Is(err, ErrSettingsReauthRateLimited) {
		t.Fatalf("attempt past the limit = %v, want ErrSettingsReauthRateLimited: the correct password kept its slot or a failure was lost", err)
	}
}

func TestAttemptReservationRefundIsBoundedByWhatItBooked(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	keys := []string{"refund:identity:one"}
	budget := AttemptBudget{Scope: "refund", Limit: 3, Window: time.Hour}
	reserve := func(limiter *AttemptLimiter, at time.Time) *AttemptReservation {
		t.Helper()
		reservation, ok := limiter.Reserve(keys, at, budget)
		if !ok {
			t.Fatal("reservation refused below the limit")
		}
		return reservation
	}
	count := func(limiter *AttemptLimiter, at time.Time) int {
		for probe := 1; probe <= budget.Limit+1; probe++ {
			if !limiter.TooManyRecentAny(keys, at, probe, budget.Window) {
				return probe - 1
			}
		}
		return budget.Limit + 1
	}

	t.Run("one slot back", func(t *testing.T) {
		limiter := NewAttemptLimiter()
		first := reserve(limiter, now)
		reserve(limiter, now)
		first.Refund()
		if got := count(limiter, now); got != 1 {
			t.Fatalf("count after refunding one of two = %d, want 1", got)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		limiter := NewAttemptLimiter()
		first := reserve(limiter, now)
		reserve(limiter, now)
		first.Refund()
		first.Refund()
		first.Refund()
		if got := count(limiter, now); got != 1 {
			t.Fatalf("count after refunding the same reservation three times = %d, want 1 (a repeat erased a real failure)", got)
		}
	})

	t.Run("nil reservation", func(t *testing.T) {
		var reservation *AttemptReservation
		reservation.Refund()
	})

	t.Run("never below zero", func(t *testing.T) {
		limiter := NewAttemptLimiter()
		only := reserve(limiter, now)
		only.Refund()
		only.Refund()
		if got := count(limiter, now); got != 0 {
			t.Fatalf("count after the only reservation was refunded = %d, want 0", got)
		}
		if _, ok := limiter.Reserve(keys, now, budget); !ok {
			t.Fatal("a refunded budget refused a fresh attempt")
		}
	})

	t.Run("a reset entry is not reached", func(t *testing.T) {
		limiter := NewAttemptLimiter()
		stale := reserve(limiter, now)
		limiter.ResetAll(keys)
		reserve(limiter, now) // same instant, a new incarnation of the entry
		stale.Refund()
		if got := count(limiter, now); got != 1 {
			t.Fatalf("count after a refund of a reservation a reset had cleared = %d, want 1: it erased the failure booked since", got)
		}
	})

	t.Run("a lapsed window is not reached", func(t *testing.T) {
		limiter := NewAttemptLimiter()
		lapsed := reserve(limiter, now)
		later := now.Add(2 * budget.Window)
		reserve(limiter, later)
		lapsed.Refund()
		if got := count(limiter, later); got != 1 {
			t.Fatalf("count after refunding a reservation older than the window = %d, want 1", got)
		}
	})

	t.Run("an attempt aged out of a live entry is not reached", func(t *testing.T) {
		limiter := NewAttemptLimiter()
		aged := reserve(limiter, now)
		reserve(limiter, now.Add(30*time.Minute))
		later := now.Add(70 * time.Minute)
		reserve(limiter, later) // prunes the first attempt out of the same entry
		aged.Refund()
		if got := count(limiter, later); got != 2 {
			t.Fatalf("count after refunding an attempt the window had dropped = %d, want 2", got)
		}
	})

	t.Run("a failure at another instant stays", func(t *testing.T) {
		limiter := NewAttemptLimiter()
		reserve(limiter, now)
		refunded := reserve(limiter, now.Add(time.Minute))
		refunded.Refund()
		if got := count(limiter, now.Add(time.Minute)); got != 1 {
			t.Fatalf("count = %d, want 1: the refund removed an attempt other than its own", got)
		}
	})
}

func TestAttemptLimiterReserveRecordsNothingWhenAnyKeyIsSpent(t *testing.T) {
	limiter := NewAttemptLimiter()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	budget := AttemptBudget{Scope: "refuse", Limit: 1, Window: time.Hour}
	limiter.AddFailureAll([]string{"refuse:identity:spent"}, now, budget)

	if reservation, ok := limiter.Reserve([]string{"refuse:client:fresh", "refuse:identity:spent"}, now, budget); ok || reservation != nil {
		t.Fatalf("Reserve with one spent key = (%v, %v), want a refusal and no reservation", reservation, ok)
	}
	if limiter.TooManyRecentAny([]string{"refuse:client:fresh"}, now, 1, time.Hour) {
		t.Fatal("a refused reservation booked an attempt under the key that still had room")
	}
}

package services

// setup_service_coverage_test.go — covers SetupService.RequiresInitialSetup,
// which had no direct test (gremlins reported the error branch on line 19 and
// the count==0 return on line 22 as NOT COVERED).

import (
	"context"
	"errors"
	"testing"
)

// setupserviceCovUserRepo is a minimal SetupUserRepository whose CountUsers
// result and error are injectable.
type setupserviceCovUserRepo struct {
	count int64
	err   error
}

func (r setupserviceCovUserRepo) CountUsers(context.Context) (int64, error) {
	return r.count, r.err
}

func TestSetupserviceCovRequiresSetupWhenNoUsers(t *testing.T) {
	// Line 22: zero users → initial setup is required.
	service := NewSetupService(setupserviceCovUserRepo{count: 0})
	required, err := service.RequiresInitialSetup(context.Background())
	if err != nil {
		t.Fatalf("RequiresInitialSetup() unexpected error: %v", err)
	}
	if !required {
		t.Fatal("RequiresInitialSetup() = false with zero users, want true")
	}
}

func TestSetupserviceCovNoSetupWhenUsersExist(t *testing.T) {
	// Line 22: any existing user → setup already done. Distinguishes the
	// `usersCount == 0` comparison from a mutated constant/operator.
	for _, count := range []int64{1, 2, 100} {
		service := NewSetupService(setupserviceCovUserRepo{count: count})
		required, err := service.RequiresInitialSetup(context.Background())
		if err != nil {
			t.Fatalf("RequiresInitialSetup() unexpected error for count=%d: %v", count, err)
		}
		if required {
			t.Fatalf("RequiresInitialSetup() = true with %d users, want false", count)
		}
	}
}

func TestSetupserviceCovPropagatesCountError(t *testing.T) {
	// Line 19–20: a repository error is returned and setup is reported false.
	wantErr := errors.New("count failed")
	service := NewSetupService(setupserviceCovUserRepo{count: 0, err: wantErr})
	required, err := service.RequiresInitialSetup(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("RequiresInitialSetup() error = %v, want %v", err, wantErr)
	}
	if required {
		t.Fatal("RequiresInitialSetup() = true on error, want false")
	}
}

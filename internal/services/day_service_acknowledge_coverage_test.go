package services

// day_service_acknowledge_coverage_test.go — covers DayService.AcknowledgePeriodTip,
// which had no direct test (gremlins reported the nil guard on line 411 as
// NOT COVERED). Reuses the dayserviceCov* stubs from day_service_coverage_test.go.

import (
	"context"
	"errors"
	"testing"
)

func TestDayserviceCovAcknowledgePeriodTipUpdatesUser(t *testing.T) {
	service := dayserviceCovNewService(&dayLogRepositoryStub{}, &dayserviceCovUserStub{})
	if err := service.AcknowledgePeriodTip(context.Background(), 7); err != nil {
		t.Fatalf("AcknowledgePeriodTip() unexpected error: %v", err)
	}
}

func TestDayserviceCovAcknowledgePeriodTipNilServiceReturnsNil(t *testing.T) {
	// Calling on a nil receiver must short-circuit. A `||` → `&&` mutation on the
	// guard would dereference service.users here and panic.
	var service *DayService
	if err := service.AcknowledgePeriodTip(context.Background(), 7); err != nil {
		t.Fatalf("AcknowledgePeriodTip() on nil service = %v, want nil", err)
	}
}

func TestDayserviceCovAcknowledgePeriodTipNilUsersReturnsNil(t *testing.T) {
	service := &DayService{}
	if err := service.AcknowledgePeriodTip(context.Background(), 7); err != nil {
		t.Fatalf("AcknowledgePeriodTip() with nil users = %v, want nil", err)
	}
}

func TestDayserviceCovAcknowledgePeriodTipPropagatesUpdateError(t *testing.T) {
	wantErr := errors.New("update failed")
	service := dayserviceCovNewService(&dayLogRepositoryStub{}, &dayserviceCovUserStub{updateErr: wantErr})
	if err := service.AcknowledgePeriodTip(context.Background(), 7); !errors.Is(err, wantErr) {
		t.Fatalf("AcknowledgePeriodTip() error = %v, want %v", err, wantErr)
	}
}

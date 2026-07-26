package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubRenormalizerAppState struct {
	values map[string]string
	getErr error
	setErr error
}

func (s *stubRenormalizerAppState) Get(_ context.Context, key string) (string, bool, error) {
	if s.getErr != nil {
		return "", false, s.getErr
	}
	value, ok := s.values[key]
	return value, ok, nil
}

func (s *stubRenormalizerAppState) Set(_ context.Context, key string, value string) error {
	if s.setErr != nil {
		return s.setErr
	}
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

type renormalizeCall struct {
	userID uint
	from   string
	to     string
}

type stubRenormalizerUserStore struct {
	summaries []models.OperatorUserSummary
	taken     map[string]bool
	calls     []renormalizeCall
	listErr   error
	existsErr error
	updateErr error
}

func (s *stubRenormalizerUserStore) ListOperatorUserSummaries(_ context.Context) ([]models.OperatorUserSummary, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.summaries, nil
}

func (s *stubRenormalizerUserStore) ExistsByNormalizedEmailExcludingUser(_ context.Context, email string, _ uint) (bool, error) {
	if s.existsErr != nil {
		return false, s.existsErr
	}
	return s.taken[email], nil
}

func (s *stubRenormalizerUserStore) RenormalizeUserEmail(_ context.Context, userID uint, from string, to string) (bool, error) {
	if s.updateErr != nil {
		return false, s.updateErr
	}
	s.calls = append(s.calls, renormalizeCall{userID: userID, from: from, to: to})
	// The rewritten address is now taken for every LATER row in the same pass,
	// which is exactly how the real unique index behaves — this is what makes
	// the oldest-wins collision assertion honest.
	if s.taken == nil {
		s.taken = map[string]bool{}
	}
	s.taken[to] = true
	return true, nil
}

// TestAuthEmailRenormalizerRewritesDecoratedRowsOldestFirst pins the repair
// semantics: a decorated row is reduced to its bare parsed address, a
// case-only row is lowered (the self-exclusion keeps it from colliding with
// itself), the second account on the same mailbox is skipped as a conflict
// (oldest wins — list order), a quoted-local row is counted unrenormalizable,
// and the done-marker is written after the pass.
func TestAuthEmailRenormalizerRewritesDecoratedRowsOldestFirst(t *testing.T) {
	appState := &stubRenormalizerAppState{}
	users := &stubRenormalizerUserStore{
		summaries: []models.OperatorUserSummary{
			{ID: 1, Email: "clean@example.com"},
			{ID: 2, Email: "john doe <shared@example.com>"},
			{ID: 3, Email: "SHARED2@EXAMPLE.COM"},
			{ID: 4, Email: "<shared@example.com>"},
			{ID: 5, Email: `"a b"@example.com`},
		},
	}
	renormalizer := NewAuthEmailRenormalizer(appState, users)

	outcome, err := renormalizer.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.AlreadyDone {
		t.Fatal("first run must not report AlreadyDone")
	}
	if outcome.Renormalized != 2 || outcome.SkippedConflicts != 1 || outcome.SkippedUnrenormalizable != 1 {
		t.Fatalf("unexpected outcome %+v", outcome)
	}
	if len(users.calls) != 2 {
		t.Fatalf("expected exactly 2 rewrites, got %+v", users.calls)
	}
	if users.calls[0] != (renormalizeCall{userID: 2, from: "john doe <shared@example.com>", to: "shared@example.com"}) {
		t.Fatalf("first rewrite wrong: %+v", users.calls[0])
	}
	if users.calls[1] != (renormalizeCall{userID: 3, from: "SHARED2@EXAMPLE.COM", to: "shared2@example.com"}) {
		t.Fatalf("second rewrite wrong: %+v", users.calls[1])
	}
	if appState.values[models.AppStateKeyAuthEmailRenormalizeV1] != "done" {
		t.Fatal("done-marker must be written after a complete pass")
	}
}

// TestAuthEmailRenormalizerSecondRunIsANoOp pins the marker contract.
func TestAuthEmailRenormalizerSecondRunIsANoOp(t *testing.T) {
	appState := &stubRenormalizerAppState{
		values: map[string]string{models.AppStateKeyAuthEmailRenormalizeV1: "done"},
	}
	users := &stubRenormalizerUserStore{
		summaries: []models.OperatorUserSummary{{ID: 1, Email: "x <y@z.com>"}},
	}
	renormalizer := NewAuthEmailRenormalizer(appState, users)

	outcome, err := renormalizer.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !outcome.AlreadyDone || outcome.Renormalized != 0 {
		t.Fatalf("expected AlreadyDone no-op, got %+v", outcome)
	}
	if len(users.calls) != 0 {
		t.Fatal("marker present: nothing may be rewritten")
	}
}

// TestAuthEmailRenormalizerAbortsWithoutMarkerOnStorageFailure pins the retry
// contract: any storage failure aborts the pass with the marker unwritten so
// the next boot re-runs it.
func TestAuthEmailRenormalizerAbortsWithoutMarkerOnStorageFailure(t *testing.T) {
	cases := []struct {
		name  string
		state *stubRenormalizerAppState
		users *stubRenormalizerUserStore
	}{
		{"get error", &stubRenormalizerAppState{getErr: errors.New("db closed")}, &stubRenormalizerUserStore{}},
		{"list error", &stubRenormalizerAppState{}, &stubRenormalizerUserStore{listErr: errors.New("db closed")}},
		{"exists error", &stubRenormalizerAppState{}, &stubRenormalizerUserStore{
			summaries: []models.OperatorUserSummary{{ID: 2, Email: "x <y@z.com>"}},
			existsErr: errors.New("db closed"),
		}},
		{"update error", &stubRenormalizerAppState{}, &stubRenormalizerUserStore{
			summaries: []models.OperatorUserSummary{{ID: 2, Email: "x <y@z.com>"}},
			updateErr: errors.New("db closed"),
		}},
		{"marker write error", &stubRenormalizerAppState{setErr: errors.New("read-only fs")}, &stubRenormalizerUserStore{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			renormalizer := NewAuthEmailRenormalizer(testCase.state, testCase.users)
			if _, err := renormalizer.Run(context.Background()); err == nil {
				t.Fatal("expected the failure to propagate")
			}
			if testCase.state.values[models.AppStateKeyAuthEmailRenormalizeV1] == "done" {
				t.Fatal("a failed pass must not write the done-marker")
			}
		})
	}
}

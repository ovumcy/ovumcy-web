package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubDashboardStatsProvider struct {
	stats CycleStats
	err   error

	// Captured user arguments — used to prove the cycle-stats reads, which
	// derive the predictions the dashboard renders, carry the session owner.
	rangeUsers   []*models.User
	fromLogUsers []*models.User
}

func (stub *stubDashboardStatsProvider) BuildCycleStatsForRange(ctx context.Context, user *models.User, _ time.Time, _ time.Time, _ time.Time, _ *time.Location) (CycleStats, []models.DailyLog, error) {
	stub.rangeUsers = append(stub.rangeUsers, user)
	if stub.err != nil {
		return CycleStats{}, nil, stub.err
	}
	return stub.stats, nil, nil
}

func (stub *stubDashboardStatsProvider) BuildCycleStatsFromLogs(user *models.User, _ []models.DailyLog, _ time.Time, _ *time.Location) CycleStats {
	stub.fromLogUsers = append(stub.fromLogUsers, user)
	return stub.stats
}

type stubDashboardDayLogProvider struct {
	logEntry models.DailyLog
	symptoms []models.SymptomType
	err      error

	// Captured user arguments — used to prove reads carry the session owner.
	observedUsers []*models.User
}

func (stub *stubDashboardDayLogProvider) FetchDayLogForOwner(ctx context.Context, user *models.User, _ time.Time, _ *time.Location) (models.DailyLog, []models.SymptomType, error) {
	stub.observedUsers = append(stub.observedUsers, user)
	if stub.err != nil {
		return models.DailyLog{}, nil, stub.err
	}
	symptoms := make([]models.SymptomType, len(stub.symptoms))
	copy(symptoms, stub.symptoms)
	return stub.logEntry, symptoms, nil
}

type stubDashboardDayStateProvider struct {
	hasData bool
	err     error
	logs    []models.DailyLog

	// Captured userID arguments — used to prove reads are owner-scoped.
	dayStateUserIDs []uint
	allLogsUserIDs  []uint
}

func (stub *stubDashboardDayStateProvider) DayHasDataForDate(ctx context.Context, userID uint, _ time.Time, _ *time.Location) (bool, error) {
	stub.dayStateUserIDs = append(stub.dayStateUserIDs, userID)
	if stub.err != nil {
		return false, stub.err
	}
	return stub.hasData, nil
}

func (stub *stubDashboardDayStateProvider) FetchAllLogsForUser(ctx context.Context, userID uint) ([]models.DailyLog, error) {
	stub.allLogsUserIDs = append(stub.allLogsUserIDs, userID)
	if stub.err != nil {
		return nil, stub.err
	}
	logs := make([]models.DailyLog, len(stub.logs))
	copy(logs, stub.logs)
	return logs, nil
}

func TestBuildDashboardViewData(t *testing.T) {
	user := &models.User{ID: 1, Role: models.RoleOwner, CycleLength: 28}
	today := mustParseDashboardServiceDay(t, "2026-02-21")
	stats := CycleStats{
		CurrentCycleDay:   5,
		MedianCycleLength: 28,
	}

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{stats: stats},
		&stubDashboardDayLogProvider{
			logEntry: models.DailyLog{
				Date:       today,
				IsPeriod:   false,
				Flow:       models.FlowNone,
				Notes:      "note",
				SymptomIDs: []uint{3},
			},
			symptoms: []models.SymptomType{{ID: 3, Name: "Headache"}},
		},
		&stubDashboardDayStateProvider{},
	)

	viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if viewData.Today.Format("2006-01-02") != "2026-02-21" {
		t.Fatalf("expected Today=2026-02-21, got %s", viewData.Today.Format("2006-01-02"))
	}
	if !viewData.IsOwner {
		t.Fatalf("expected IsOwner=true")
	}
	if !viewData.TodayHasData {
		t.Fatalf("expected TodayHasData=true")
	}
	if len(viewData.Symptoms) != 1 {
		t.Fatalf("expected one symptom in view data, got %d", len(viewData.Symptoms))
	}
	if !viewData.SelectedSymptomID[3] {
		t.Fatalf("expected selected symptom id=3")
	}
	if !viewData.AllowManualCycleStart {
		t.Fatalf("expected AllowManualCycleStart=true for today")
	}
}

func TestBuildDashboardViewDataReturnsTypedErrors(t *testing.T) {
	user := &models.User{ID: 2, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-02-21")

	// Non-owner with <2 symptoms doesn't need entry-context logs, so stats
	// still take the single ranged-query path (BuildCycleStatsForRange).
	nonOwner := &models.User{ID: 20, Role: "viewer"}
	statsErrService := NewDashboardViewService(
		&stubDashboardStatsProvider{err: errors.New("stats fail")},
		&stubDashboardDayLogProvider{},
		&stubDashboardDayStateProvider{},
	)
	if _, err := statsErrService.BuildDashboardViewData(context.Background(), nonOwner, "en", now, time.UTC); !errors.Is(err, ErrDashboardViewLoadStats) {
		t.Fatalf("expected ErrDashboardViewLoadStats, got %v", err)
	}

	// Owner view needs entry-context logs, so stats are derived from the
	// FetchAllLogsForUser fetch; a failure there surfaces as
	// ErrDashboardViewLoadLogs instead of ErrDashboardViewLoadStats.
	logsErrService := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardDayLogProvider{},
		&stubDashboardDayStateProvider{err: errors.New("logs fail")},
	)
	if _, err := logsErrService.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC); !errors.Is(err, ErrDashboardViewLoadLogs) {
		t.Fatalf("expected ErrDashboardViewLoadLogs, got %v", err)
	}

	dayErrService := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardDayLogProvider{err: errors.New("day fail")},
		&stubDashboardDayStateProvider{},
	)
	if _, err := dayErrService.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC); !errors.Is(err, ErrDashboardViewLoadTodayLog) {
		t.Fatalf("expected ErrDashboardViewLoadTodayLog, got %v", err)
	}
}

func TestBuildDayEditorViewData(t *testing.T) {
	user := &models.User{ID: 3, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-02-21")
	day := mustParseDashboardServiceDay(t, "2026-02-22")

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardDayLogProvider{
			logEntry: models.DailyLog{
				Date:       day,
				IsPeriod:   true,
				Flow:       models.FlowLight,
				SymptomIDs: []uint{7},
			},
			symptoms: []models.SymptomType{{ID: 7, Name: "Bloating"}},
		},
		&stubDashboardDayStateProvider{hasData: true},
	)

	viewData, err := service.BuildDayEditorViewData(context.Background(), user, "en", day, now, time.UTC)
	if err != nil {
		t.Fatalf("BuildDayEditorViewData() unexpected error: %v", err)
	}
	if !viewData.IsFutureDate {
		t.Fatalf("expected IsFutureDate=true")
	}
	if !viewData.HasDayData {
		t.Fatalf("expected HasDayData=true")
	}
	if viewData.DateString != "2026-02-22" {
		t.Fatalf("expected DateString=2026-02-22, got %q", viewData.DateString)
	}
	if !viewData.SelectedSymptomID[7] {
		t.Fatalf("expected selected symptom id=7")
	}
	if !viewData.AllowManualCycleStart {
		t.Fatalf("expected AllowManualCycleStart=true for tomorrow")
	}
	if !viewData.ShowFutureCycleStartNotice {
		t.Fatalf("expected ShowFutureCycleStartNotice=true for tomorrow")
	}
}

func TestBuildDashboardViewDataSuggestsManualCycleStartAfterLongGap(t *testing.T) {
	user := &models.User{ID: 5, Role: models.RoleOwner, CycleLength: 28}
	today := mustParseDashboardServiceDay(t, "2026-02-21")

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardDayLogProvider{
			logEntry: models.DailyLog{
				Date:     today,
				IsPeriod: true,
				Flow:     models.FlowLight,
			},
			symptoms: []models.SymptomType{{ID: 3, Name: "Headache"}},
		},
		&stubDashboardDayStateProvider{
			logs: []models.DailyLog{
				{Date: mustParseDashboardServiceDay(t, "2026-02-01"), IsPeriod: true, CycleStart: true},
				{Date: today, IsPeriod: true},
			},
		},
	)

	viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if !viewData.ShowCycleStartSuggestion {
		t.Fatalf("expected ShowCycleStartSuggestion=true after a long gap")
	}
}

func TestBuildDashboardViewDataShowsHighFertilityBadgeForEggWhiteMucus(t *testing.T) {
	user := &models.User{ID: 6, Role: models.RoleOwner, CycleLength: 28, TrackCervicalMucus: true}
	today := mustParseDashboardServiceDay(t, "2026-02-21")

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardDayLogProvider{
			logEntry: models.DailyLog{
				Date:          today,
				CervicalMucus: models.CervicalMucusEggWhite,
			},
		},
		&stubDashboardDayStateProvider{},
	)

	viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if !viewData.ShowHighFertilityBadge {
		t.Fatalf("expected high-fertility badge for egg-white mucus")
	}
}

func TestBuildDashboardViewDataAddsPredictionFactorHintForVariablePatterns(t *testing.T) {
	user := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 32}
	today := mustParseDashboardServiceDay(t, "2026-04-25")

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{stats: CycleStats{
			CompletedCycleCount: 3,
			MedianCycleLength:   32,
			MinCycleLength:      24,
			MaxCycleLength:      44,
			LastPeriodStart:     mustParseDashboardServiceDay(t, "2026-04-20"),
			NextPeriodStart:     mustParseDashboardServiceDay(t, "2026-05-21"),
		}},
		&stubDashboardDayLogProvider{
			logEntry: models.DailyLog{Date: today},
			symptoms: []models.SymptomType{{ID: 3, Name: "Headache"}},
		},
		&stubDashboardDayStateProvider{
			logs: []models.DailyLog{
				{Date: mustParseDashboardServiceDay(t, "2026-01-01"), IsPeriod: true},
				{Date: mustParseDashboardServiceDay(t, "2026-01-03"), CycleFactorKeys: []string{models.CycleFactorStress}},
				{Date: mustParseDashboardServiceDay(t, "2026-01-25"), IsPeriod: true},
				{Date: mustParseDashboardServiceDay(t, "2026-01-28"), CycleFactorKeys: []string{models.CycleFactorTravel}},
				{Date: mustParseDashboardServiceDay(t, "2026-03-10"), IsPeriod: true},
				{Date: mustParseDashboardServiceDay(t, "2026-03-12"), CycleFactorKeys: []string{models.CycleFactorStress}},
				{Date: mustParseDashboardServiceDay(t, "2026-04-20"), IsPeriod: true},
			},
		},
	)

	viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if !viewData.HasPredictionFactorHint {
		t.Fatalf("expected dashboard prediction factor hint")
	}
	if len(viewData.PredictionFactorHintKeys) != 2 || viewData.PredictionFactorHintKeys[0] != models.CycleFactorStress {
		t.Fatalf("expected stable dashboard factor hint order, got %#v", viewData.PredictionFactorHintKeys)
	}
	if !viewData.HasPredictionExplanationSecondary || viewData.PredictionExplanationSecondaryKey != "prediction.explainer.factor_context" {
		t.Fatalf("expected shared factor explanation copy, got %#v", viewData)
	}
}

func TestBuildDashboardViewDataAddsSharedIrregularSparseExplanation(t *testing.T) {
	user := &models.User{ID: 8, Role: models.RoleOwner, CycleLength: 32, IrregularCycle: true}
	today := mustParseDashboardServiceDay(t, "2026-02-10")

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{stats: CycleStats{
			CompletedCycleCount: 2,
			LastPeriodStart:     mustParseDashboardServiceDay(t, "2026-02-01"),
			NextPeriodStart:     mustParseDashboardServiceDay(t, "2026-03-05"),
			MedianCycleLength:   32,
		}},
		&stubDashboardDayLogProvider{
			logEntry: models.DailyLog{Date: today},
			symptoms: []models.SymptomType{{ID: 3, Name: "Headache"}},
		},
		&stubDashboardDayStateProvider{},
	)

	viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if !viewData.HasPredictionExplanationPrimary || viewData.PredictionExplanationPrimaryKey != "prediction.explainer.irregular_sparse" {
		t.Fatalf("expected shared irregular sparse explanation, got %#v", viewData)
	}
}

func TestBuildDayEditorViewDataReturnsTypedErrors(t *testing.T) {
	user := &models.User{ID: 4, Role: models.RoleOwner}
	now := mustParseDashboardServiceDay(t, "2026-02-21")
	day := mustParseDashboardServiceDay(t, "2026-02-22")

	dayStateErrService := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardDayLogProvider{},
		&stubDashboardDayStateProvider{err: errors.New("state fail")},
	)
	if _, err := dayStateErrService.BuildDayEditorViewData(context.Background(), user, "en", day, now, time.UTC); !errors.Is(err, ErrDashboardViewLoadDayState) {
		t.Fatalf("expected ErrDashboardViewLoadDayState, got %v", err)
	}

	dayLogErrService := NewDashboardViewService(
		&stubDashboardStatsProvider{},
		&stubDashboardDayLogProvider{err: errors.New("day log fail")},
		&stubDashboardDayStateProvider{},
	)
	if _, err := dayLogErrService.BuildDayEditorViewData(context.Background(), user, "en", day, now, time.UTC); !errors.Is(err, ErrDashboardViewLoadDayLog) {
		t.Fatalf("expected ErrDashboardViewLoadDayLog, got %v", err)
	}
}

// TestDashboardViewProvidersReadOnlyTheSessionOwner pins owner propagation on
// the dashboard's whole read path. Every provider call these two builders make
// reads special-category health data, so each must carry the acting session
// owner and nothing else: the day-log fetch takes the user itself, the day-state
// reads take its id. The stubs record what they were handed, and the account id
// is deliberately not 1 — an unscoped or hard-coded read would otherwise agree
// with the fixture by accident. Each provider is asserted to have been reached
// at least once, so a run that recorded nothing fails here instead of passing
// as "no mismatch found".
func TestDashboardViewProvidersReadOnlyTheSessionOwner(t *testing.T) {
	const sessionOwnerID uint = 4242

	user := &models.User{ID: sessionOwnerID, Role: models.RoleOwner, CycleLength: 28}
	now := mustParseDashboardServiceDay(t, "2026-02-21")
	day := mustParseDashboardServiceDay(t, "2026-02-20")

	dayLogs := &stubDashboardDayLogProvider{
		logEntry: models.DailyLog{Date: now, Notes: "owner-note"},
		symptoms: []models.SymptomType{{ID: 3, Name: "Headache"}},
	}
	days := &stubDashboardDayStateProvider{
		hasData: true,
		logs: []models.DailyLog{
			{Date: mustParseDashboardServiceDay(t, "2026-02-01"), IsPeriod: true, CycleStart: true},
			{Date: now, IsPeriod: true},
		},
	}
	stats := &stubDashboardStatsProvider{stats: CycleStats{MedianCycleLength: 28}}
	service := NewDashboardViewService(stats, dayLogs, days)

	if _, err := service.BuildDashboardViewData(context.Background(), user, "en", now, time.UTC); err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}
	if _, err := service.BuildDayEditorViewData(context.Background(), user, "en", day, now, time.UTC); err != nil {
		t.Fatalf("BuildDayEditorViewData() unexpected error: %v", err)
	}

	if len(dayLogs.observedUsers) == 0 {
		t.Fatal("expected the day-log provider to be reached; it recorded no call")
	}
	for index, captured := range dayLogs.observedUsers {
		if captured == nil {
			t.Fatalf("day-log call %d received no user at all", index)
		}
		if captured.ID != sessionOwnerID {
			t.Fatalf("day-log call %d read owner id %d, want the session owner %d", index, captured.ID, sessionOwnerID)
		}
	}

	if len(days.dayStateUserIDs) == 0 {
		t.Fatal("expected the day-state provider to be reached; it recorded no DayHasDataForDate call")
	}
	for index, capturedID := range days.dayStateUserIDs {
		if capturedID != sessionOwnerID {
			t.Fatalf("DayHasDataForDate call %d read owner id %d, want the session owner %d", index, capturedID, sessionOwnerID)
		}
	}

	if len(days.allLogsUserIDs) == 0 {
		t.Fatal("expected the day-state provider to be reached; it recorded no FetchAllLogsForUser call")
	}
	for index, capturedID := range days.allLogsUserIDs {
		if capturedID != sessionOwnerID {
			t.Fatalf("FetchAllLogsForUser call %d read owner id %d, want the session owner %d", index, capturedID, sessionOwnerID)
		}
	}

	// The cycle-stats reads decide what the dashboard predicts, so a session
	// mixed up here shows one account's phase and fertile window on another's
	// dashboard — the same boundary, one layer further in. An owner view
	// derives its stats from the entry-context logs, so this path reaches
	// BuildCycleStatsFromLogs and never BuildCycleStatsForRange.
	if len(stats.fromLogUsers) == 0 {
		t.Fatal("expected the stats provider to be reached; it recorded no BuildCycleStatsFromLogs call")
	}
	for index, captured := range stats.fromLogUsers {
		if captured == nil {
			t.Fatalf("BuildCycleStatsFromLogs call %d received no user at all", index)
		}
		if captured.ID != sessionOwnerID {
			t.Fatalf("BuildCycleStatsFromLogs call %d read owner id %d, want the session owner %d", index, captured.ID, sessionOwnerID)
		}
	}

	// The ranged stats read is the other half of the same seam and is reached
	// only by a session that needs no entry-context logs, so it gets its own
	// session user rather than being left unobserved.
	const rangeSessionID uint = 7373

	rangeStats := &stubDashboardStatsProvider{stats: CycleStats{MedianCycleLength: 28}}
	rangeSession := &models.User{ID: rangeSessionID, Role: "viewer", CycleLength: 28}
	rangeService := NewDashboardViewService(rangeStats, &stubDashboardDayLogProvider{}, &stubDashboardDayStateProvider{})
	if _, err := rangeService.BuildDashboardViewData(context.Background(), rangeSession, "en", now, time.UTC); err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error on the ranged stats path: %v", err)
	}
	if len(rangeStats.rangeUsers) == 0 {
		t.Fatal("expected the stats provider to be reached; it recorded no BuildCycleStatsForRange call")
	}
	for index, captured := range rangeStats.rangeUsers {
		if captured == nil {
			t.Fatalf("BuildCycleStatsForRange call %d received no user at all", index)
		}
		if captured.ID != rangeSessionID {
			t.Fatalf("BuildCycleStatsForRange call %d read session id %d, want %d", index, captured.ID, rangeSessionID)
		}
	}
}

func TestFirstMissingTrackedDayIgnoresDaysBeforeTrackingStart(t *testing.T) {
	today := mustParseDashboardServiceDay(t, "2026-02-21")
	trackingStart := mustParseDashboardServiceDay(t, "2026-02-18")
	logs := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-02-18"), Notes: "logged"},
		{Date: mustParseDashboardServiceDay(t, "2026-02-19"), Notes: "logged"},
		{Date: mustParseDashboardServiceDay(t, "2026-02-20"), Notes: "logged"},
	}

	missedDay, show := firstMissingTrackedDay(logs, today, 14, trackingStart, time.UTC)
	if show {
		t.Fatalf("did not expect missed-days link, got missed day %s", missedDay.Format("2006-01-02"))
	}
}

func TestFirstMissingTrackedDayFindsTrackedGap(t *testing.T) {
	today := mustParseDashboardServiceDay(t, "2026-02-21")
	trackingStart := mustParseDashboardServiceDay(t, "2026-02-10")
	logs := []models.DailyLog{
		{Date: mustParseDashboardServiceDay(t, "2026-02-10"), Notes: "logged"},
		{Date: mustParseDashboardServiceDay(t, "2026-02-14"), Notes: "logged"},
		{Date: mustParseDashboardServiceDay(t, "2026-02-15"), Notes: "logged"},
	}

	missedDay, show := firstMissingTrackedDay(logs, today, 14, trackingStart, time.UTC)
	if !show {
		t.Fatal("expected missed-days link for tracked gap")
	}
	if missedDay.Format("2006-01-02") != "2026-02-11" {
		t.Fatalf("expected first missed tracked day 2026-02-11, got %s", missedDay.Format("2006-01-02"))
	}
}

// TestResolveDashboardTimingFrameGatesTheOvulationEstimateOnEverySuppression
// pins which cycle states the goal-aware ovulation item survives. The frame
// answers a question about the goal, but the estimate it adds is a prediction:
// it must disappear wherever the next-period window does — an unpredictable
// cycle, a pregnancy pause, and a cycle overdue past reference + 7, where the
// projection has nothing left to say — and also before the first completed
// cycle, where the bridge line stands in for it. The temperature's placement
// answers the goal question alone and is unmoved by any of them.
//
// The cases feed a (user, stats) pair through BuildDashboardCycleContext rather
// than hand-building the context, because the frame no longer recombines the
// suppression signals: the ovulation estimate reads the FertilitySuppressed
// decision the context resolved once. A hand-built context can set
// PredictionDisabled without the decision that follows from it, which is a state
// the builder never emits — asserting against one would pin the frame to a
// fixture rather than to the policy.
func TestResolveDashboardTimingFrameGatesTheOvulationEstimateOnEverySuppression(t *testing.T) {
	today := mustParseDashboardServiceDay(t, dashboardSuppressionDay)
	visibility := dashboardOwnerVisibility{ShowBBTField: true}

	for name, testCase := range map[string]struct {
		mutateUser   func(*models.User)
		mutateStats  func(*CycleStats)
		wantEstimate bool
		wantBridge   bool
	}{
		"stable cycle": {
			wantEstimate: true,
		},
		"predictions suppressed": {
			mutateUser:   func(user *models.User) { user.UnpredictableCycle = true },
			wantEstimate: false,
		},
		"pregnancy pause": {
			mutateStats:  func(stats *CycleStats) { stats.PregnancyPaused = true },
			wantEstimate: false,
		},
		"cycle overdue": {
			mutateStats:  func(stats *CycleStats) { stats.CurrentCycleDay = 54 },
			wantEstimate: false,
		},
		"awaiting the first completed cycle": {
			mutateStats:  func(stats *CycleStats) { stats.CompletedCycleCount = 0 },
			wantEstimate: false,
			wantBridge:   true,
		},
		"awaiting the first cycle with predictions off": {
			mutateUser:   func(user *models.User) { user.UnpredictableCycle = true },
			mutateStats:  func(stats *CycleStats) { stats.CompletedCycleCount = 0 },
			wantEstimate: false,
			wantBridge:   false,
		},
		// The two rows below are the bridge's own boundary, and they are here
		// because a shared boolean moved under it. The bridge names NO date —
		// it says the window arrives after the first cycle closes — so a paused
		// projection has nothing to withhold from it, and an overdue first cycle
		// is exactly when the owner most needs to be told that. Both the
		// irregular and the regular account reach it: they differ only in which
		// branch of buildDashboardPredictionDisplay claims the display, which is
		// not a fact about the copy.
		"awaiting the first cycle, overdue": {
			mutateStats: func(stats *CycleStats) {
				stats.CompletedCycleCount = 0
				stats.CurrentCycleDay = 54
			},
			wantEstimate: false,
			wantBridge:   true,
		},
		"awaiting the first cycle, overdue, irregular": {
			mutateUser: func(user *models.User) { user.IrregularCycle = true },
			mutateStats: func(stats *CycleStats) {
				stats.CompletedCycleCount = 0
				stats.CurrentCycleDay = 54
			},
			wantEstimate: false,
			wantBridge:   true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			user := &models.User{ID: 13, Role: models.RoleOwner, UsageGoal: models.UsageGoalTrying, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
			if testCase.mutateUser != nil {
				testCase.mutateUser(user)
			}
			stats := dashboardSuppressionStats(t)
			if testCase.mutateStats != nil {
				testCase.mutateStats(&stats)
			}
			frame := resolveDashboardTimingFrame(user, BuildDashboardCycleContext(user, stats, today, time.UTC), visibility)
			if frame.ShowOvulationEstimate != testCase.wantEstimate {
				t.Fatalf("expected ovulation estimate=%v, got %v", testCase.wantEstimate, frame.ShowOvulationEstimate)
			}
			if frame.ShowFirstCycleBridge != testCase.wantBridge {
				t.Fatalf("expected the first-cycle bridge line=%v, got %v", testCase.wantBridge, frame.ShowFirstCycleBridge)
			}
			if !frame.BBTInVisibleTier {
				t.Fatalf("expected the temperature field to stay in the visible tier for this goal")
			}
		})
	}
}

// TestBuildDashboardViewDataHoldsFertilityBackUntilTheFirstCompletedCycle pins
// the early data tier at its boundary — zero completed cycles against one — for
// both the goal that asks about timing and the neutral one.
//
// With no completed cycle the fertile window and the ovulation date are the
// onboarding cycle-length slider projected forward, so the header withholds both
// and an account tracking to conceive reads one bridge line in the ovulation
// item's place instead. The moment a single cycle closes, the account has an
// observation to reason from and the header renders exactly as it did before.
// The count itself is the stats reliability signal (CompletedCycleCount), read
// rather than recomputed here.
func TestBuildDashboardViewDataHoldsFertilityBackUntilTheFirstCompletedCycle(t *testing.T) {
	today := mustParseDashboardServiceDay(t, "2026-02-21")

	for name, testCase := range map[string]struct {
		completedCycles int
		goal            string
		wantFertility   bool
		wantOvulation   bool
		wantBridge      bool
	}{
		"trying, no completed cycle": {
			completedCycles: 0,
			goal:            models.UsageGoalTrying,
			wantBridge:      true,
		},
		"trying, one completed cycle": {
			completedCycles: 1,
			goal:            models.UsageGoalTrying,
			wantFertility:   true,
			wantOvulation:   true,
		},
		"health, no completed cycle": {
			completedCycles: 0,
			goal:            models.UsageGoalHealth,
		},
		"health, one completed cycle": {
			completedCycles: 1,
			goal:            models.UsageGoalHealth,
			wantFertility:   true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			user := &models.User{ID: 9, Role: models.RoleOwner, CycleLength: 28, UsageGoal: testCase.goal, TrackBBT: true}
			service := NewDashboardViewService(
				&stubDashboardStatsProvider{stats: CycleStats{
					CompletedCycleCount: testCase.completedCycles,
					CurrentCycleDay:     5,
					MedianCycleLength:   28,
					LastPeriodStart:     mustParseDashboardServiceDay(t, "2026-02-17"),
					NextPeriodStart:     mustParseDashboardServiceDay(t, "2026-03-17"),
				}},
				&stubDashboardDayLogProvider{logEntry: models.DailyLog{Date: today}},
				&stubDashboardDayStateProvider{},
			)

			viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
			if err != nil {
				t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
			}
			if viewData.ShowFertilityStatus != testCase.wantFertility {
				t.Fatalf("expected fertility status shown=%v, got %v", testCase.wantFertility, viewData.ShowFertilityStatus)
			}
			if viewData.ShowOvulationEstimate != testCase.wantOvulation {
				t.Fatalf("expected ovulation estimate=%v, got %v", testCase.wantOvulation, viewData.ShowOvulationEstimate)
			}
			if viewData.ShowFirstCycleBridge != testCase.wantBridge {
				t.Fatalf("expected the first-cycle bridge line=%v, got %v", testCase.wantBridge, viewData.ShowFirstCycleBridge)
			}
			// The tier moves nothing else: the cycle day and the next-period
			// estimate the header shows beside them are untouched by it.
			if viewData.Stats.CurrentCycleDay != 5 {
				t.Fatalf("expected the cycle day to survive the tier, got %d", viewData.Stats.CurrentCycleDay)
			}
			if viewData.CycleContext.DisplayNextPeriodStart.IsZero() {
				t.Fatal("expected the next-period estimate to survive the tier")
			}
		})
	}
}

func mustParseDashboardServiceDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}

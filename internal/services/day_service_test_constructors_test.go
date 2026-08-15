package services

// Test-only constructors for DayService.
//
// The composition root always wires a transaction runner
// (NewDayServiceWithTx, internal/bootstrap), so a DayService with a nil runner
// is a shape production never builds — it exists so unit fixtures can drive the
// service against in-memory stubs, where withinTransaction falls back to the
// pass-through path. Keeping it here rather than in day_service.go states that
// status in the file layout and keeps the production build free of a
// constructor nothing in the application reaches.

// NewDayService builds a DayService without a transaction runner.
func NewDayService(logs DayLogRepository, users DayUserRepository) *DayService {
	return &DayService{
		logs:  logs,
		users: users,
	}
}

package db

import "gorm.io/gorm"

type Repositories struct {
	Users                *UserRepository
	OIDCIdentities       *OIDCIdentityRepository
	OIDCLogout           *OIDCLogoutStateRepository
	DailyLogs            *DailyLogRepository
	Symptoms             *SymptomRepository
	RegisterPickupTokens *RegisterPickupTokenRepository
	AppState             *AppStateRepository
	Health               *HealthRepository
}

func NewRepositories(database *gorm.DB) *Repositories {
	return &Repositories{
		Users:                NewUserRepository(database),
		OIDCIdentities:       NewOIDCIdentityRepository(database),
		OIDCLogout:           NewOIDCLogoutStateRepository(database),
		DailyLogs:            NewDailyLogRepository(database),
		Symptoms:             NewSymptomRepository(database),
		RegisterPickupTokens: NewRegisterPickupTokenRepository(database),
		AppState:             NewAppStateRepository(database),
		Health:               NewHealthRepository(database),
	}
}

// WithCalendarFeedFence attaches the calendar-feed restore fence to the
// repository that changes the armed-feed set, and returns the same set so
// production wiring stays one expression.
//
// It is a second step rather than a NewRepositories argument because the fence
// is built FROM these repositories — it reads and writes app_state through
// them — so the two cannot be constructed in one call. Production code must not
// reach NewRepositories directly for that same reason: it would get a set whose
// feed writes record nothing outside the database, and a revocation that is
// recorded only inside the database is the defect the fence exists for. Every
// binary therefore builds its repositories through bootstrap.BuildRepositories,
// and a guard refuses a direct call anywhere else.
func (repositories *Repositories) WithCalendarFeedFence(fence CalendarFeedFence) *Repositories {
	repositories.Users.calendarFeedFence = fence
	return repositories
}

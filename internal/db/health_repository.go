package db

import (
	"context"

	"gorm.io/gorm"
)

// HealthRepository answers the storage-reachability probe behind the readiness
// endpoint. It reads no rows and holds no domain state — it only proves that a
// connection can be obtained and that the engine answers a query — so it is
// unscoped by user_id and returns nothing a caller could inspect.
type HealthRepository struct {
	database *gorm.DB
}

func NewHealthRepository(database *gorm.DB) *HealthRepository {
	return &HealthRepository{database: database}
}

// Ping runs the cheapest statement both supported engines accept and returns
// the driver error unchanged. A plain connection ping is not enough: the
// glebarez/modernc SQLite driver does not implement driver.Pinger, so
// database/sql answers a ping from the pool without touching the engine.
// Executing a statement exercises the real query path instead. The caller owns
// the deadline; ctx is threaded so a hung engine cannot outlive it.
func (repo *HealthRepository) Ping(ctx context.Context) error {
	var probe int
	return repo.database.WithContext(ctx).Raw("SELECT 1").Scan(&probe).Error
}

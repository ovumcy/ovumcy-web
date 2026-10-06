package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestNewestBoundaryOpensClusterOfSkipsClustersThatOpenNoCycle pins the walk
// from the newest period cluster backwards: a later cluster that opens no cycle
// (a lone bare period day, neither adjacent to another nor today/yesterday) is
// passed over, and the answer is decided by the newest cluster that does open
// one. With no opening cluster at all, no day is in the newest cycle's cluster.
func TestNewestBoundaryOpensClusterOfSkipsClustersThatOpenNoCycle(t *testing.T) {
	utcDay := func(month time.Month, day int) time.Time {
		return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
	}
	ctx := BoundaryContext{Today: utcDay(time.June, 1)}
	opening := []models.DailyLog{
		{Date: utcDay(time.January, 1), IsPeriod: true},
		{Date: utcDay(time.January, 2), IsPeriod: true},
	}
	lone := models.DailyLog{Date: utcDay(time.March, 1), IsPeriod: true}
	logs := append(append([]models.DailyLog{}, opening...), lone)

	if !newestBoundaryOpensClusterOf(logs, ctx, utcDay(time.January, 2)) {
		t.Fatal("a later cluster that opens no cycle must not displace the January cluster as the newest boundary")
	}
	if newestBoundaryOpensClusterOf(logs, ctx, utcDay(time.March, 1)) {
		t.Fatal("the lone March day opens no cycle, so it is not in the newest boundary's cluster")
	}
	if newestBoundaryOpensClusterOf([]models.DailyLog{lone}, ctx, lone.Date) {
		t.Fatal("with no cluster opening a cycle, no day is in the newest boundary's cluster")
	}
}

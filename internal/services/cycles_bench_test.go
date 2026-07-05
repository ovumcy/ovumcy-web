package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Benchmarks for the cycle math that runs on every dashboard and stats load.
// They guard against accidental performance regressions in the hot path; run
// with `go test -bench . -run '^$' ./internal/services/` and compare with
// benchstat across commits.

func benchCycleLogs(cycles int) []models.DailyLog {
	logs := make([]models.DailyLog, 0, cycles*5)
	firstStart := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	for c := range cycles {
		periodStart := firstStart.AddDate(0, 0, c*28)
		for d := range 5 {
			logs = append(logs, models.DailyLog{
				Date:       periodStart.AddDate(0, 0, d),
				IsPeriod:   true,
				CycleStart: d == 0,
			})
		}
	}
	return logs
}

func BenchmarkBuildCycleStats(b *testing.B) {
	logs := benchCycleLogs(24)
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = BuildCycleStats(logs, now)
	}
}

func BenchmarkDetectCycleStarts(b *testing.B) {
	logs := benchCycleLogs(24)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = DetectCycleStarts(logs)
	}
}

func BenchmarkPredictCycleWindow(b *testing.B) {
	periodStart := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = PredictCycleWindow(periodStart, 28, 14)
	}
}

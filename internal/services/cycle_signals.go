package services

import (
	"math"
	"sort"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type cycleBBTPoint struct {
	Date     time.Time
	CycleDay int
	Value    float64
}

func InferUserLutealPhase(logs []models.DailyLog, location *time.Location) (int, bool) {
	if location == nil {
		location = time.UTC
	}

	starts := ObservedCycleStarts(logs)
	if len(starts) < 3 {
		return defaultLutealPhaseDays, false
	}

	lutealLengths := make([]int, 0, len(starts)-1)
	for index := 0; index+1 < len(starts); index++ {
		start := DateAtLocation(starts[index], location)
		nextStart := DateAtLocation(starts[index+1], location)
		ovulationDate := inferObservedOvulationDate(logs, start, nextStart, location)
		if ovulationDate.IsZero() {
			continue
		}

		lutealLength := int(nextStart.Sub(ovulationDate).Hours() / 24)
		if lutealLength < minLutealPhaseDays || lutealLength > 20 {
			continue
		}
		lutealLengths = append(lutealLengths, lutealLength)
	}

	if len(lutealLengths) < 2 {
		return defaultLutealPhaseDays, false
	}
	return int(math.Round(averageInts(lutealLengths))), true
}

func inferObservedOvulationDate(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) time.Time {
	bbtDate := inferBBTOvulationDate(logs, cycleStart, nextStart, location)
	if !bbtDate.IsZero() {
		return bbtDate
	}
	return inferEggWhiteOvulationDate(logs, cycleStart, nextStart, location)
}

func inferBBTOvulationDate(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) time.Time {
	points := collectCycleBBTPoints(logs, cycleStart, nextStart, location)
	if len(points) < 5 {
		return time.Time{}
	}

	var baselineTotal float64
	for index := 0; index < 5; index++ {
		baselineTotal += points[index].Value
	}
	baseline := baselineTotal / 5.0
	threshold := baseline + 0.2
	streak := 0
	for index := 5; index < len(points); index++ {
		if points[index].Value >= threshold {
			streak++
		} else {
			streak = 0
		}
		if streak >= 3 {
			return DateAtLocation(points[index-2].Date, location)
		}
	}
	return time.Time{}
}

func collectCycleBBTPoints(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) []cycleBBTPoint {
	points := make([]cycleBBTPoint, 0)
	for _, logEntry := range logs {
		if !IsValidDayBBT(logEntry.BBT) || logEntry.BBT <= 0 {
			continue
		}

		day := DateAtLocation(logEntry.Date, location)
		if day.Before(cycleStart) || !day.Before(nextStart) {
			continue
		}

		points = append(points, cycleBBTPoint{
			Date:     day,
			CycleDay: int(day.Sub(cycleStart).Hours()/24) + 1,
			Value:    logEntry.BBT,
		})
	}

	sort.Slice(points, func(i, j int) bool {
		return points[i].Date.Before(points[j].Date)
	})
	return points
}

func inferEggWhiteOvulationDate(logs []models.DailyLog, cycleStart time.Time, nextStart time.Time, location *time.Location) time.Time {
	ovulationDate := time.Time{}
	for _, logEntry := range sortDailyLogs(logs) {
		day := DateAtLocation(logEntry.Date, location)
		if day.Before(cycleStart) || !day.Before(nextStart) {
			continue
		}
		if NormalizeDayCervicalMucus(logEntry.CervicalMucus) != models.CervicalMucusEggWhite {
			continue
		}
		ovulationDate = day
	}
	return ovulationDate
}

package services

import (
	"math"
	"strconv"
)

// The ring is the at-a-glance form of the same cycle the ribbon draws: phase
// arcs read off the ribbon's own cells, a thin band hugging their outer edge
// for the fertile window (inside, it crowded the day number) and a dot on
// today. It never derives a day of its own, so the two cannot disagree, and
// what it cannot draw — a start window running past the cycle's end, the
// recorded days — stays on the ribbon and in the summary.
const (
	dashboardCycleRingCenter        = 60.0
	dashboardCycleRingRadius        = 48.0
	dashboardCycleRingFertileRadius = 57.5
	// dashboardCycleRingGap separates two phase arcs, in viewBox units.
	dashboardCycleRingGap = 1.5
)

type DashboardCycleHeroRing struct {
	Visible bool
	Arcs    []DashboardCycleHeroArc
	Fertile DashboardCycleHeroArc
	Marker  DashboardCycleHeroMarker
	// TodayOnRing is false when today falls past the cycle length the ring
	// spans; the ribbon and the summary still place it.
	TodayOnRing bool
}

type DashboardCycleHeroArc struct {
	Phase      string
	DashArray  string
	DashOffset string
}

func (arc DashboardCycleHeroArc) Present() bool { return arc.DashArray != "" }

type DashboardCycleHeroMarker struct {
	X string
	Y string
}

func dashboardCycleHeroRingFor(days []DashboardCycleHeroDay, cycleLength int, currentDay int) DashboardCycleHeroRing {
	ringDays := min(cycleLength, len(days))
	if ringDays <= 0 {
		return DashboardCycleHeroRing{}
	}
	ring := DashboardCycleHeroRing{Visible: true}
	outer := 2 * math.Pi * dashboardCycleRingRadius
	for start := 0; start < ringDays; {
		end := start
		for end+1 < ringDays && days[end+1].Phase == days[start].Phase {
			end++
		}
		ring.Arcs = append(ring.Arcs, dashboardCycleRingArc(days[start].Phase, start, end, ringDays, outer, dashboardCycleRingGap))
		start = end + 1
	}

	fertileStart, fertileEnd := -1, -1
	for index := range ringDays {
		if days[index].IsFertile {
			if fertileStart < 0 {
				fertileStart = index
			}
			fertileEnd = index
		}
	}
	if fertileStart >= 0 {
		ring.Fertile = dashboardCycleRingArc("", fertileStart, fertileEnd, ringDays, 2*math.Pi*dashboardCycleRingFertileRadius, 0)
	}

	if currentDay >= 1 && currentDay <= ringDays {
		ring.TodayOnRing = true
		angle := -math.Pi/2 + (float64(currentDay)-0.5)/float64(ringDays)*2*math.Pi
		ring.Marker = DashboardCycleHeroMarker{
			X: dashboardCycleRingFloat(dashboardCycleRingCenter + dashboardCycleRingRadius*math.Cos(angle)),
			Y: dashboardCycleRingFloat(dashboardCycleRingCenter + dashboardCycleRingRadius*math.Sin(angle)),
		}
	}
	return ring
}

// dashboardCycleRingArc draws cells [start, end] (0-based) of ringDays on a
// circle of the given circumference, starting at 12 o'clock once the SVG group
// rotates it by -90°.
func dashboardCycleRingArc(phase string, start, end, ringDays int, circumference float64, gap float64) DashboardCycleHeroArc {
	perDay := circumference / float64(ringDays)
	dash := max(perDay*float64(end-start+1)-gap, 0.5)
	return DashboardCycleHeroArc{
		Phase:      phase,
		DashArray:  dashboardCycleRingFloat(dash) + " " + dashboardCycleRingFloat(circumference-dash),
		DashOffset: dashboardCycleRingFloat(-perDay * float64(start)),
	}
}

func dashboardCycleRingFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

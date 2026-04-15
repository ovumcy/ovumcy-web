package services

import (
	"math"
	"strconv"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	dashboardCycleHeroCenterX = 110.0
	dashboardCycleHeroCenterY = 110.0
	dashboardCycleHeroRadius  = 78.0
)

var dashboardCycleHeroCircumference = 2 * math.Pi * dashboardCycleHeroRadius

type DashboardCycleHero struct {
	Visible              bool
	Approximate          bool
	CurrentDay           int
	CycleLength          int
	CurrentPhase         string
	PhaseCards           []DashboardCycleHeroPhaseCard
	Segments             []DashboardCycleHeroSegment
	CurrentDayMarker     DashboardCycleHeroMarker
	ShowCurrentDayMarker bool
}

type DashboardCycleHeroPhaseCard struct {
	Phase     string
	StartDay  int
	EndDay    int
	IsCurrent bool
}

type DashboardCycleHeroSegment struct {
	Phase      string
	DashArray  string
	DashOffset string
}

type DashboardCycleHeroMarker struct {
	X string
	Y string
}

func BuildDashboardCycleHero(user *models.User, stats CycleStats, cycleContext DashboardCycleContext) DashboardCycleHero {
	cycleLength := DashboardCycleReferenceLength(user, stats)
	if cycleLength <= 0 ||
		stats.CurrentCycleDay <= 0 ||
		stats.CurrentCycleDay > cycleLength ||
		cycleContext.PredictionDisabled ||
		cycleContext.CycleDataStale ||
		cycleContext.DisplayNextPeriodPrompt ||
		cycleContext.DisplayNextPeriodNeedsData ||
		cycleContext.DisplayOvulationNeedsData ||
		cycleContext.DisplayOvulationImpossible {
		return DashboardCycleHero{}
	}

	periodLength := predictedPeriodLength(stats.AveragePeriodLength)
	if periodLength >= cycleLength {
		return DashboardCycleHero{}
	}

	ovulationDay, _ := CalcOvulationDay(cycleLength, stats.LutealPhase)
	if ovulationDay <= periodLength+1 || ovulationDay > cycleLength {
		return DashboardCycleHero{}
	}

	currentDay := stats.CurrentCycleDay
	currentPhase := dashboardCycleHeroCurrentPhase(stats.CurrentPhase, currentDay, periodLength, ovulationDay, cycleLength)
	phaseCards := []DashboardCycleHeroPhaseCard{
		{
			Phase:     "menstrual",
			StartDay:  1,
			EndDay:    periodLength,
			IsCurrent: currentPhase == "menstrual",
		},
		{
			Phase:     "follicular",
			StartDay:  periodLength + 1,
			EndDay:    ovulationDay - 1,
			IsCurrent: currentPhase == "follicular",
		},
		{
			Phase:     "ovulation",
			StartDay:  ovulationDay,
			EndDay:    ovulationDay,
			IsCurrent: currentPhase == "ovulation",
		},
		{
			Phase:     "luteal",
			StartDay:  ovulationDay + 1,
			EndDay:    cycleLength,
			IsCurrent: currentPhase == "luteal",
		},
	}

	segments := make([]DashboardCycleHeroSegment, 0, len(phaseCards))
	offset := 0.0
	for _, card := range phaseCards {
		dayCount := float64(card.EndDay - card.StartDay + 1)
		dash := dashboardCycleHeroCircumference * (dayCount / float64(cycleLength))
		segments = append(segments, DashboardCycleHeroSegment{
			Phase:      card.Phase,
			DashArray:  dashboardCycleHeroDashArray(dash),
			DashOffset: dashboardCycleHeroFloat(-offset),
		})
		offset += dash
	}

	return DashboardCycleHero{
		Visible:              true,
		Approximate:          cycleContext.DisplayNextPeriodUseRange || cycleContext.DisplayOvulationUseRange || !cycleContext.DisplayOvulationExact,
		CurrentDay:           currentDay,
		CycleLength:          cycleLength,
		CurrentPhase:         currentPhase,
		PhaseCards:           phaseCards,
		Segments:             segments,
		CurrentDayMarker:     dashboardCycleHeroMarkerAtDay(float64(currentDay)-0.5, cycleLength),
		ShowCurrentDayMarker: true,
	}
}

func dashboardCycleHeroCurrentPhase(currentPhase string, currentDay int, periodLength int, ovulationDay int, cycleLength int) string {
	switch currentPhase {
	case "menstrual", "ovulation", "luteal":
		return currentPhase
	case "follicular", "fertile":
		return "follicular"
	}

	switch {
	case currentDay >= 1 && currentDay <= periodLength:
		return "menstrual"
	case currentDay == ovulationDay:
		return "ovulation"
	case currentDay > ovulationDay && currentDay <= cycleLength:
		return "luteal"
	default:
		return "follicular"
	}
}

func dashboardCycleHeroMarkerAtDay(dayIndex float64, cycleLength int) DashboardCycleHeroMarker {
	ratio := dayIndex / float64(cycleLength)
	angle := (-math.Pi / 2) + (ratio * 2 * math.Pi)
	x := dashboardCycleHeroCenterX + dashboardCycleHeroRadius*math.Cos(angle)
	y := dashboardCycleHeroCenterY + dashboardCycleHeroRadius*math.Sin(angle)
	return DashboardCycleHeroMarker{
		X: dashboardCycleHeroFloat(x),
		Y: dashboardCycleHeroFloat(y),
	}
}

func dashboardCycleHeroDashArray(dash float64) string {
	return dashboardCycleHeroFloat(dash) + " " + dashboardCycleHeroFloat(dashboardCycleHeroCircumference-dash)
}

func dashboardCycleHeroFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 1, 64)
}

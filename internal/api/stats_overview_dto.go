package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// statsOverviewDateLayout is the wire spelling of every date in this payload:
// YYYY-MM-DD, never an RFC 3339 instant. A cycle date is a calendar day, and
// serializing it as an instant published a timezone the owner never chose along
// with it.
const statsOverviewDateLayout = "2006-01-02"

// medicalDisclaimerMessageKey is the single catalogue entry every predictive
// surface renders. The API sends the resolved text AND the key, because a
// client cannot branch on translated prose, and a test asserting the copy alone
// goes quiet the day the wording changes.
const medicalDisclaimerMessageKey = "medical.disclaimer"

// medicalDisclaimer resolves the safety framing for this request, falling back
// to the server's default language when the request carries no catalogue.
//
// The fallback is not defensive padding. currentMessages answers an empty map
// whenever LanguageMiddleware has not run for a route, and translateMessage
// renders a miss as the key itself — which is what a template wants and is
// exactly wrong here: the payload would carry the literal "medical.disclaimer"
// where the owner-visible safety text belongs, with nothing failing. A surface
// that must not go silent carries its own fallback, which is the second return
// value lookupMessage exists for.
func (handler *Handler) medicalDisclaimer(c fiber.Ctx) string {
	if disclaimer, ok := lookupMessage(currentMessages(c), medicalDisclaimerMessageKey); ok {
		return disclaimer
	}
	if handler.i18n == nil {
		return ""
	}
	disclaimer, _ := lookupMessage(handler.i18n.Messages(handler.i18n.DefaultLanguage()), medicalDisclaimerMessageKey)
	return disclaimer
}

// StatsOverviewSuppression is the machine-readable half of the medical-safety
// decision: WHETHER a projection was withheld, and WHY.
//
// The two booleans mirror the two shared predicates
// (services.PredictionSuppression) and are what a client branches on; Reasons
// explains a decision already made and may name more signals than the booleans
// strictly needed, since several can hold at once. Reasons is never null — an
// absent reason list and an empty one would otherwise mean the same thing to a
// consumer that does not distinguish them.
type StatsOverviewSuppression struct {
	Predictions bool     `json:"predictions"`
	Fertility   bool     `json:"fertility"`
	Reasons     []string `json:"reasons"`
}

// StatsOverviewResponse is the published shape of GET /api/v1/stats/overview.
//
// It exists because the endpoint used to serialize the domain CycleStats whole.
// That struct is the derivation's working copy — every forward-looking field on
// it, including the ones /stats and the dashboard withhold — so the JSON view
// published exactly the claims the display policy had already refused. An
// explicit DTO makes the wire shape a decision instead of a side effect of a
// domain struct's field list: a field added to CycleStats reaches no client
// until someone names it here.
//
// ovulation_date carries the same additional resolution the calendar's solid
// marker and the dashboard's ovulation line already apply, through
// services.PublishedOverviewStats: once the owner's own BBT readings confirm
// the current cycle's ovulation, the confirmed day outranks the model's
// projection here too — this was the one surface still naming the superseded
// projection after the other six had moved on to the confirmed day. That day
// is inferred from the owner's own temperature shift, never a measurement of
// the ovulation itself.
//
// ovulation_confirmed names that substitution rather than folding it into
// ovulation_exact. The two are independent on the dashboard
// (DisplayOvulationExact vs DisplayOvulationConfirmed, dashboard_cycle.go): a
// fallback-luteal account (ovulation_exact=false) can still have its current
// cycle's ovulation CONFIRMED by a BBT shift, and a client that cannot see
// both loses the "measured, not modeled" distinction the domain keeps.
//
// Every projected date is a pointer and is null when suppressed. Null rather
// than omitted, and never a zero date: the field set stays constant across
// states, so a client parses one shape and reads suppression from the
// suppression object rather than from a sentinel date it has to recognise.
//
// Recorded history — the observed lengths, the last period start, the current
// cycle day — is fact rather than projection and is published in every tier.
type StatsOverviewResponse struct {
	CurrentCycleDay      int                      `json:"current_cycle_day"`
	CurrentPhase         string                   `json:"current_phase"`
	CurrentFertility     string                   `json:"current_fertility"`
	AverageCycleLength   float64                  `json:"average_cycle_length"`
	MedianCycleLength    int                      `json:"median_cycle_length"`
	MinCycleLength       int                      `json:"min_cycle_length"`
	MaxCycleLength       int                      `json:"max_cycle_length"`
	CycleLengthStdDev    float64                  `json:"cycle_length_std_dev"`
	CompletedCycleCount  int                      `json:"completed_cycle_count"`
	AveragePeriodLength  float64                  `json:"average_period_length"`
	LastCycleLength      int                      `json:"last_cycle_length"`
	LastPeriodLength     int                      `json:"last_period_length"`
	LutealPhase          int                      `json:"luteal_phase"`
	LastPeriodStart      *string                  `json:"last_period_start"`
	NextPeriodStart      *string                  `json:"next_period_start"`
	OvulationDate        *string                  `json:"ovulation_date"`
	OvulationExact       bool                     `json:"ovulation_exact"`
	OvulationConfirmed   bool                     `json:"ovulation_confirmed"`
	OvulationImpossible  bool                     `json:"ovulation_impossible"`
	FertilityWindowStart *string                  `json:"fertility_window_start"`
	FertilityWindowEnd   *string                  `json:"fertility_window_end"`
	PregnancyPaused      bool                     `json:"pregnancy_paused"`
	Suppression          StatsOverviewSuppression `json:"suppression"`
	Disclaimer           string                   `json:"disclaimer"`
	DisclaimerKey        string                   `json:"disclaimer_key"`
}

// newStatsOverviewResponse maps the PUBLISHED stats — the copy
// services.PublishedStats has already cleared — onto the wire. It applies no
// suppression of its own: a transport that decided what to withhold would be a
// second implementation of the display policy, which is the divergence the one
// adapter exists to prevent. Handed the raw stats it would faithfully publish
// them, so the clearing stays the caller's obligation and the verdict travels
// with the data.
func newStatsOverviewResponse(stats services.CycleStats, suppression services.PredictionSuppression, confirmed bool, disclaimer string) StatsOverviewResponse {
	return StatsOverviewResponse{
		CurrentCycleDay:      stats.CurrentCycleDay,
		CurrentPhase:         stats.CurrentPhase,
		CurrentFertility:     stats.CurrentFertility,
		AverageCycleLength:   stats.AverageCycleLength,
		MedianCycleLength:    stats.MedianCycleLength,
		MinCycleLength:       stats.MinCycleLength,
		MaxCycleLength:       stats.MaxCycleLength,
		CycleLengthStdDev:    stats.CycleLengthStdDev,
		CompletedCycleCount:  stats.CompletedCycleCount,
		AveragePeriodLength:  stats.AveragePeriodLength,
		LastCycleLength:      stats.LastCycleLength,
		LastPeriodLength:     stats.LastPeriodLength,
		LutealPhase:          stats.LutealPhase,
		LastPeriodStart:      statsOverviewDate(stats.LastPeriodStart),
		NextPeriodStart:      statsOverviewDate(stats.NextPeriodStart),
		OvulationDate:        statsOverviewDate(stats.OvulationDate),
		OvulationExact:       stats.OvulationExact,
		OvulationConfirmed:   confirmed,
		OvulationImpossible:  stats.OvulationImpossible,
		FertilityWindowStart: statsOverviewDate(stats.FertilityWindowStart),
		FertilityWindowEnd:   statsOverviewDate(stats.FertilityWindowEnd),
		PregnancyPaused:      stats.PregnancyPaused,
		Suppression:          newStatsOverviewSuppression(suppression),
		Disclaimer:           disclaimer,
		DisclaimerKey:        medicalDisclaimerMessageKey,
	}
}

func newStatsOverviewSuppression(suppression services.PredictionSuppression) StatsOverviewSuppression {
	reasons := make([]string, 0, len(suppression.Reasons))
	for _, reason := range suppression.Reasons {
		reasons = append(reasons, string(reason))
	}
	return StatsOverviewSuppression{
		Predictions: suppression.PredictionsSuppressed,
		Fertility:   suppression.FertilitySuppressed,
		Reasons:     reasons,
	}
}

// statsOverviewDate renders a cycle date in its OWN location. The derivation
// carries two kinds of date-only value — location midnight (the recorded
// anchors) and UTC midnight (the projected window) — and converting either into
// the other's zone moves it a calendar day in half the world's offsets (the
// issue #48 class). Formatting where the value already sits names the day it
// was computed for.
func statsOverviewDate(value time.Time) *string {
	if value.IsZero() {
		return nil
	}
	formatted := value.Format(statsOverviewDateLayout)
	return &formatted
}

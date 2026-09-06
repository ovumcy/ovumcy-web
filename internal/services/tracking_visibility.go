package services

import "github.com/ovumcy/ovumcy-web/internal/models"

// TrackingVisibility is the positive view model for the three owner day-form
// sections whose storage is inverted: the columns are hide_sex_chip,
// hide_cycle_factors and hide_notes_field, while every surface — the settings
// toggles, the dashboard, the calendar day editor and the day-write
// preservation rules — reasons in "shown".
//
// This file is the SINGLE conversion point between the two vocabularies. The
// inversion is a negation, and a negation applied twice on the way to one
// surface is invisible in review yet hides a section the owner asked to see, so
// no other file may perform it: callers either receive a TrackingVisibility or
// ask this type for the stored spelling. The property is pinned against the
// whole tree by TestTrackingVisibilityKeepsASingleConversionPoint.
//
// Renaming the columns themselves is deliberately out of scope here — the
// adapter is what makes the rename a separate, mechanical change later.
//
// ShowBBTField and ShowCervicalMucus join them because the QUESTION is the same
// one — is this field on the owner's day form — even though their columns
// (track_bbt, track_cervical_mucus) are stored the positive way round and need
// no inversion. Only TrackingVisibilityForUser can answer them: they are not
// derivable from TrackingHiddenColumns, so a value built by
// TrackingVisibilityFromHiddenColumns speaks for the three inverted sections
// alone and says nothing about these two.
type TrackingVisibility struct {
	ShowSexChip       bool
	ShowCycleFactors  bool
	ShowNotesField    bool
	ShowBBTField      bool
	ShowCervicalMucus bool
}

// TrackingHiddenColumns is the stored, inverted spelling of the same three
// values. It exists so persistence and the published v1 wire keys can be
// written from a named struct instead of an ad-hoc negation at the call site.
type TrackingHiddenColumns struct {
	HideSexChip      bool
	HideCycleFactors bool
	HideNotesField   bool
}

// TrackingVisibilityFromHiddenColumns undoes the stored inversion. It is the
// only place in the codebase that does so, and it answers for the three
// inverted sections only — the two positive columns stay at their zero value,
// so BBTFieldHidden and CervicalMucusHidden are meaningless on its result.
func TrackingVisibilityFromHiddenColumns(columns TrackingHiddenColumns) TrackingVisibility {
	return TrackingVisibility{
		ShowSexChip:      !columns.HideSexChip,
		ShowCycleFactors: !columns.HideCycleFactors,
		ShowNotesField:   !columns.HideNotesField,
	}
}

// TrackingVisibilityForUser reads one owner's stored columns through the
// conversion above and adds the two positive ones, so it is the only complete
// answer to "which day-form fields does this account show". A nil user yields
// the stored defaults (all three inverted columns false), i.e. every section
// visible — the same answer the zero-valued user row gives, so a missing user
// never reads as "the owner hid this". The two positive columns follow that
// same row, where they are false: an account that has not opted into
// temperature or cervical mucus does not show those fields.
func TrackingVisibilityForUser(user *models.User) TrackingVisibility {
	if user == nil {
		return TrackingVisibilityFromHiddenColumns(TrackingHiddenColumns{})
	}
	visibility := TrackingVisibilityFromHiddenColumns(TrackingHiddenColumns{
		HideSexChip:      user.HideSexChip,
		HideCycleFactors: user.HideCycleFactors,
		HideNotesField:   user.HideNotesField,
	})
	visibility.ShowBBTField = user.TrackBBT
	visibility.ShowCervicalMucus = user.TrackCervicalMucus
	return visibility
}

// HiddenColumns folds a positive view model back into the stored spelling, for
// the persistence write and for the v1 JSON echo that publishes the inverted
// keys.
func (visibility TrackingVisibility) HiddenColumns() TrackingHiddenColumns {
	return TrackingHiddenColumns{
		HideSexChip:      !visibility.ShowSexChip,
		HideCycleFactors: !visibility.ShowCycleFactors,
		HideNotesField:   !visibility.ShowNotesField,
	}
}

// ApplyToUser writes the three stored columns of an in-memory user row from
// this view model, so no caller has to touch the inverted fields.
func (visibility TrackingVisibility) ApplyToUser(user *models.User) {
	if user == nil {
		return
	}
	columns := visibility.HiddenColumns()
	user.HideSexChip = columns.HideSexChip
	user.HideCycleFactors = columns.HideCycleFactors
	user.HideNotesField = columns.HideNotesField
}

// SexChipHidden, CycleFactorsHidden, NotesFieldHidden, BBTFieldHidden and
// CervicalMucusHidden answer the one question that is genuinely negative: the
// day-write path does not read a field its form never showed, and preserves the
// stored value instead, so the owner cannot erase data through a form that
// never showed it. All five live here so that the day-write path asks one type
// for the whole set (hiddenDayFields) rather than negating two of the columns
// itself.
func (visibility TrackingVisibility) SexChipHidden() bool {
	return !visibility.ShowSexChip
}

func (visibility TrackingVisibility) CycleFactorsHidden() bool {
	return !visibility.ShowCycleFactors
}

func (visibility TrackingVisibility) NotesFieldHidden() bool {
	return !visibility.ShowNotesField
}

func (visibility TrackingVisibility) BBTFieldHidden() bool {
	return !visibility.ShowBBTField
}

func (visibility TrackingVisibility) CervicalMucusHidden() bool {
	return !visibility.ShowCervicalMucus
}

package services

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// storedTrackingInversionTokens are every spelling of the three inverted
// columns: the Go fields, the column/wire keys and the settings toggle hooks.
var storedTrackingInversionTokens = []string{
	"HideSexChip", "HideCycleFactors", "HideNotesField",
	"hide_sex_chip", "hide_cycle_factors", "hide_notes_field",
	"hide-sex-chip", "hide-cycle-factors", "hide-notes-field",
}

// trackingInversionAllowedFiles are the files that may name the stored,
// inverted spelling at all, each with the reason it has no alternative. Every
// other file — every template, every view builder, every handler — reasons in
// TrackingVisibility's positive vocabulary.
var trackingInversionAllowedFiles = map[string]string{
	filepath.Join("internal", "models", "user.go"):                    "declares the persisted columns",
	filepath.Join("internal", "db", "user_repository.go"):             "the settings Select whitelist and the clear-data reset map name columns",
	filepath.Join("internal", "services", "settings_service.go"):      "the tracking update map names columns",
	filepath.Join("internal", "services", "tracking_visibility.go"):   "the conversion point itself",
	filepath.Join("internal", "api", "input_types.go"):                "the published v1 JSON request body",
	filepath.Join("internal", "api", "handlers_settings_tracking.go"): "binds and echoes the published v1 JSON keys",
}

// positiveTrackingColumnTokens are every spelling of the two columns stored the
// positive way round: the Go fields and the column/wire keys. They need no
// inversion, so the sweep above says nothing about them, yet they answer the
// same question as the three inverted ones — does this owner's day form show
// this field — and must reach a day form through the same view model.
var positiveTrackingColumnTokens = []string{
	"TrackBBT", "TrackCervicalMucus",
	"track_bbt", "track_cervical_mucus",
}

// positiveTrackingColumnAllowedFiles are the files that may read those two
// columns off the user row, each with the question that entitles it to.
//
// Two kinds of entry appear here. In the settings files the column IS the
// subject: it is bound, stored, published and rendered there as its own toggle.
// calendar_days.go and cycle_signals.go ask the other question — whether a
// temperature series exists to derive a signal from at all — on surfaces that
// render no day form, and TrackingVisibility's whole subject is the day form
// (TrackingVisibilityForUser), so asking it there would name the wrong rule
// even though ShowBBTField happens to carry the same bit today.
var positiveTrackingColumnAllowedFiles = map[string]string{
	filepath.Join("internal", "models", "user.go"):                                 "declares the persisted columns",
	filepath.Join("internal", "db", "user_repository.go"):                          "the settings Select whitelist and the clear-data reset map name columns",
	filepath.Join("internal", "services", "settings_service.go"):                   "the tracking update map names columns",
	filepath.Join("internal", "services", "settings_tracking_policy.go"):           "writes the columns from one settings update",
	filepath.Join("internal", "services", "settings_view_service.go"):              "the settings page state is those toggles",
	filepath.Join("internal", "services", "tracking_visibility.go"):                "the single reader that turns the columns into day-form visibility",
	filepath.Join("internal", "services", "calendar_days.go"):                      "asks whether a temperature series exists at all, not whether a day form shows the field",
	filepath.Join("internal", "services", "cycle_signals.go"):                      "the same question, for the cycle signal derived from that series",
	filepath.Join("internal", "api", "input_types.go"):                             "the published v1 JSON request body",
	filepath.Join("internal", "api", "handlers_settings_tracking.go"):              "binds and echoes the published v1 JSON keys",
	filepath.Join("internal", "api", "settings_view_helpers.go"):                   "hands the settings toggles to their template",
	filepath.Join("internal", "templates", "components", "settings_tracking.html"): "renders the settings toggles",
}

// TestTrackingVisibilityKeepsASingleConversionPoint pins the property that
// makes the positive view model safe: the stored inversion is undone in
// exactly one file. A second conversion — a handler reading the raw column
// while the template reads the adapter — is a double negation, and a double
// negation in a visibility flag hides a field the owner asked to see, silently
// and in production only.
//
// It fails on two shapes: naming an inverted spelling outside the small set of
// files that must (columns, published v1 wire keys, the conversion point), and
// negating one of those spellings anywhere but the conversion point.
func TestTrackingVisibilityKeepsASingleConversionPoint(t *testing.T) {
	conversionPoint := filepath.Join("internal", "services", "tracking_visibility.go")

	walkTrackingColumnSources(t, func(relative string, data []byte) {
		reason, allowed := trackingInversionAllowedFiles[relative]
		for number, line := range strings.Split(string(data), "\n") {
			if !containsAnyToken(line, storedTrackingInversionTokens) {
				continue
			}
			switch {
			case !allowed:
				t.Errorf(
					"%s:%d names a stored tracking inversion (%q); read it through services.TrackingVisibility instead",
					relative, number+1, strings.TrimSpace(line),
				)
			case relative != conversionPoint && negatesTrackingFlag(line):
				t.Errorf(
					"%s:%d negates a stored tracking flag (%q), but %s may only name it (%s); the inversion belongs in %s",
					relative, number+1, strings.TrimSpace(line), relative, reason, conversionPoint,
				)
			}
		}
	})
}

// TestTrackingVisibilityIsTheOnlyDayFormReaderOfTheTrackedColumns pins the same
// property for the two columns that need no inversion. Nothing negates them, so
// the sweep above cannot see them, and the failure they hide is quieter: a
// handler or a view builder writes `user.TrackBBT` where it means "the day form
// shows the temperature field", the two spellings agree on the day they are
// written, and they drift the first time the visibility answer gains a term the
// column does not carry — a role, an onboarding state, a per-surface override.
// The five fields would then be resolved from one type in four places and from
// the raw row in the fifth, which is the divergence TrackingVisibility exists to
// make impossible. It fails on one shape: naming either column outside the files
// entitled to.
//
// It sweeps every production Go source and template under internal/ and cmd/ —
// the same set as the sweep above — rather than only the transport and dashboard
// builders where the defect was found, because the column is readable from any
// layer and the next day-form surface need not be built in either of them; a
// sweep scoped to today's two callers would be silent about tomorrow's third.
// The price is that a legitimate reader must be entered in
// positiveTrackingColumnAllowedFiles with the question it asks, which is where
// the two questions are kept apart rather than blurred.
//
// What it does NOT see, said out loud because the sibling sweep does see it: an
// entry here exempts the whole FILE, so a day-form read added to a file already
// listed passes unremarked. The inverted columns can be checked line by line
// because the defect has a spelling there — a negation outside the conversion
// point — while a positive column reads the same whichever question it is
// asked, so nothing in the line separates the two. An allowlist entry is
// therefore a claim about a file's whole subject, and a file that grows a day
// form is a reason to re-read its entry rather than to widen it.
func TestTrackingVisibilityIsTheOnlyDayFormReaderOfTheTrackedColumns(t *testing.T) {
	walkTrackingColumnSources(t, func(relative string, data []byte) {
		if _, allowed := positiveTrackingColumnAllowedFiles[relative]; allowed {
			return
		}
		for number, line := range strings.Split(string(data), "\n") {
			if !containsAnyToken(line, positiveTrackingColumnTokens) {
				continue
			}
			t.Errorf(
				"%s:%d reads a stored tracking column (%q); ask services.TrackingVisibilityForUser whether the day form shows the field. "+
					"Only if this line asks the other question — whether the account records temperature or cervical mucus at all, on a "+
					"surface with no day form — does it belong in positiveTrackingColumnAllowedFiles, named with that reason",
				relative, number+1, strings.TrimSpace(line),
			)
		}
	})
}

// walkTrackingColumnSources hands every source that can carry a tracking flag —
// production Go and every server-rendered template under internal/ and cmd/ — to
// visit, as a repo-relative path and its bytes. Both sweeps read it, so neither
// can go quiet by looking at fewer files than the other claims.
func walkTrackingColumnSources(t *testing.T, visit func(relative string, data []byte)) {
	t.Helper()

	repoRoot := filepath.Join("..", "..")
	scanned := 0
	for _, tree := range []string{"internal", "cmd"} {
		root := filepath.Join(repoRoot, tree)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !trackingColumnScannedFile(path) {
				return nil
			}
			relative, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			scanned++
			visit(relative, data)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if scanned == 0 {
		t.Fatalf("no sources scanned under %s; the test setup is wrong", repoRoot)
	}
}

// trackingColumnScannedFile selects the sources that can carry a tracking flag:
// production Go code and every server-rendered template.
func trackingColumnScannedFile(path string) bool {
	switch {
	case strings.HasSuffix(path, "_test.go"):
		return false
	case strings.HasSuffix(path, ".go"), strings.HasSuffix(path, ".html"):
		return true
	default:
		return false
	}
}

func containsAnyToken(line string, tokens []string) bool {
	for _, token := range tokens {
		if strings.Contains(line, token) {
			return true
		}
	}
	return false
}

// negatesTrackingFlag reports a boolean negation on the line — "!" that is not
// part of "!=". Template negation reads as `{{if not .Hide…}}`, so that spelling
// counts too.
func negatesTrackingFlag(line string) bool {
	if strings.Contains(line, "not .Hide") {
		return true
	}
	for index, character := range line {
		if character != '!' {
			continue
		}
		if index+1 < len(line) && line[index+1] == '=' {
			continue
		}
		return true
	}
	return false
}

func TestTrackingVisibilityUndoesAndRedoesTheStoredInversion(t *testing.T) {
	stored := TrackingHiddenColumns{HideSexChip: true, HideNotesField: true}
	visibility := TrackingVisibilityFromHiddenColumns(stored)

	if visibility.ShowSexChip {
		t.Error("a stored hide_sex_chip=true must read as not shown")
	}
	if !visibility.ShowCycleFactors {
		t.Error("a stored hide_cycle_factors=false must read as shown")
	}
	if visibility.ShowNotesField {
		t.Error("a stored hide_notes_field=true must read as not shown")
	}

	if roundTripped := visibility.HiddenColumns(); roundTripped != stored {
		t.Fatalf("expected the stored columns back unchanged, got %+v", roundTripped)
	}

	if !visibility.SexChipHidden() || visibility.CycleFactorsHidden() || !visibility.NotesFieldHidden() {
		t.Fatalf("hidden predicates disagree with the view model: %+v", visibility)
	}
}

func TestTrackingVisibilityForUserReadsTheStoredColumns(t *testing.T) {
	// The account hides one inverted section and has opted into temperature but
	// not cervical mucus, so each of the two tracked columns is asserted in the
	// direction it is stored: a reader that inverts them, or that answers one of
	// them from the other, disagrees with this row.
	user := &models.User{HideCycleFactors: true, TrackBBT: true}

	visibility := TrackingVisibilityForUser(user)
	if !visibility.ShowSexChip || visibility.ShowCycleFactors || !visibility.ShowNotesField {
		t.Fatalf("unexpected visibility for a user hiding only cycle factors: %+v", visibility)
	}
	if !visibility.ShowBBTField || visibility.ShowCervicalMucus {
		t.Fatalf("the tracked columns are read as stored, not inverted: %+v", visibility)
	}

	TrackingVisibility{ShowSexChip: true, ShowCycleFactors: true, ShowNotesField: false}.ApplyToUser(user)
	if user.HideSexChip || user.HideCycleFactors || !user.HideNotesField {
		t.Fatalf("ApplyToUser wrote the wrong columns: %+v", user)
	}

	// A missing user is the stored default — every inverted section visible —
	// never "the owner hid this", which a zero-valued TrackingVisibility would
	// mean. The two tracked columns follow that same default row, where they are
	// false: no user, no opt-in, so those two fields are not shown.
	absent := TrackingVisibilityForUser(nil)
	if !absent.ShowSexChip || !absent.ShowCycleFactors || !absent.ShowNotesField {
		t.Fatalf("a nil user must read the inverted sections as visible, got %+v", absent)
	}
	if absent.ShowBBTField || absent.ShowCervicalMucus {
		t.Fatalf("a nil user has opted into neither tracked field, got %+v", absent)
	}
	var nilUser *models.User
	TrackingVisibility{}.ApplyToUser(nilUser)
}

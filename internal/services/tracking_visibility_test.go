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
	repoRoot := filepath.Join("..", "..")
	conversionPoint := filepath.Join("internal", "services", "tracking_visibility.go")

	scanned := 0
	for _, tree := range []string{"internal", "cmd"} {
		root := filepath.Join(repoRoot, tree)
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !trackingInversionScannedFile(path) {
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
			reason, allowed := trackingInversionAllowedFiles[relative]
			for number, line := range strings.Split(string(data), "\n") {
				if !containsAnyToken(line) {
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

// trackingInversionScannedFile selects the sources that can carry the
// inversion: production Go code and every server-rendered template.
func trackingInversionScannedFile(path string) bool {
	switch {
	case strings.HasSuffix(path, "_test.go"):
		return false
	case strings.HasSuffix(path, ".go"), strings.HasSuffix(path, ".html"):
		return true
	default:
		return false
	}
}

func containsAnyToken(line string) bool {
	for _, token := range storedTrackingInversionTokens {
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
	user := &models.User{HideCycleFactors: true}

	visibility := TrackingVisibilityForUser(user)
	if !visibility.ShowSexChip || visibility.ShowCycleFactors || !visibility.ShowNotesField {
		t.Fatalf("unexpected visibility for a user hiding only cycle factors: %+v", visibility)
	}

	TrackingVisibility{ShowSexChip: true, ShowCycleFactors: true, ShowNotesField: false}.ApplyToUser(user)
	if user.HideSexChip || user.HideCycleFactors || !user.HideNotesField {
		t.Fatalf("ApplyToUser wrote the wrong columns: %+v", user)
	}

	// A missing user is the stored default — every section visible — never
	// "the owner hid this", which a zero-valued TrackingVisibility would mean.
	if absent := TrackingVisibilityForUser(nil); !absent.ShowSexChip || !absent.ShowCycleFactors || !absent.ShowNotesField {
		t.Fatalf("a nil user must read as fully visible, got %+v", absent)
	}
	var nilUser *models.User
	TrackingVisibility{}.ApplyToUser(nilUser)
}

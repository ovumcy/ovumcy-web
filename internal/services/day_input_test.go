package services

import (
	"errors"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/templates"
)

func TestNormalizeDayEntryInputRejectsInvalidFlow(t *testing.T) {
	_, err := NormalizeDayEntryInput(DayEntryInput{
		IsPeriod: true,
		Flow:     "bad-flow",
	})
	if !errors.Is(err, ErrInvalidDayFlow) {
		t.Fatalf("expected ErrInvalidDayFlow, got %v", err)
	}
}

// TestNormalizeDayEntryInputRejectsEachFieldWithItsOwnSentinel pins the whole
// rejection mapping of NormalizeDayEntryInput, not one arm of it: every tracked
// field has its own sentinel, and an invalid value must surface THAT sentinel.
// The guards run in a fixed order, so a field whose check is deleted does not
// stop rejecting — the value falls through to the next check and is reported
// under a neighbour's error, which the API's error mapping then renders as the
// wrong field. Three of these sentinels (sex activity, BBT, cervical mucus)
// were produced by no test in this package at all.
func TestNormalizeDayEntryInputRejectsEachFieldWithItsOwnSentinel(t *testing.T) {
	outOfRangeBBT := 50.0

	tests := []struct {
		name  string
		input DayEntryInput
		want  error
	}{
		{
			name:  "flow",
			input: DayEntryInput{IsPeriod: true, Flow: "bad-flow"},
			want:  ErrInvalidDayFlow,
		},
		{
			name:  "mood",
			input: DayEntryInput{Flow: models.FlowNone, Mood: MaxDayMood + 1},
			want:  ErrInvalidDayMood,
		},
		{
			name:  "sex activity",
			input: DayEntryInput{Flow: models.FlowNone, SexActivity: "bad-activity"},
			want:  ErrInvalidDaySexActivity,
		},
		{
			name:  "bbt",
			input: DayEntryInput{Flow: models.FlowNone, BBT: &outOfRangeBBT},
			want:  ErrInvalidDayBBT,
		},
		{
			name:  "cervical mucus",
			input: DayEntryInput{Flow: models.FlowNone, CervicalMucus: "bad-mucus"},
			want:  ErrInvalidDayCervicalMucus,
		},
		{
			name:  "pregnancy test",
			input: DayEntryInput{Flow: models.FlowNone, PregnancyTest: "bad-test"},
			want:  ErrInvalidDayPregnancyTest,
		},
		{
			name:  "cycle factors",
			input: DayEntryInput{Flow: models.FlowNone, CycleFactorKeys: []string{"unknown"}},
			want:  ErrInvalidDayCycleFactors,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeDayEntryInput(tt.input)
			if !errors.Is(err, tt.want) {
				t.Fatalf("NormalizeDayEntryInput() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNormalizeDayEntryInputNormalizesNonPeriodDay(t *testing.T) {
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		IsPeriod:   false,
		Flow:       models.FlowHeavy,
		SymptomIDs: []uint{10, 11},
		Notes:      "note",
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() unexpected error: %v", err)
	}
	if normalized.Flow != models.FlowNone {
		t.Fatalf("expected flow %q, got %q", models.FlowNone, normalized.Flow)
	}
	if len(normalized.SymptomIDs) != 2 || normalized.SymptomIDs[0] != 10 || normalized.SymptomIDs[1] != 11 {
		t.Fatalf("expected symptom IDs to be preserved, got %#v", normalized.SymptomIDs)
	}
}

func TestNormalizeDayEntryInputTrimsNotes(t *testing.T) {
	// Non-Latin text on the normalization path too: the cap is a character cap,
	// so an over-limit note is cut to MaxDayNotesLength characters whatever the
	// script costs in bytes.
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		IsPeriod: true,
		Flow:     models.FlowNone,
		Notes:    strings.Repeat("б", MaxDayNotesLength+13),
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() unexpected error: %v", err)
	}
	if characters := utf8.RuneCountInString(normalized.Notes); characters != MaxDayNotesLength {
		t.Fatalf("expected notes length %d characters, got %d", MaxDayNotesLength, characters)
	}
}

// TestTrimDayNotes pins the unit of the notes cap: MaxDayNotesLength counts
// CHARACTERS, the same unit the textarea's maxlength speaks in. Measured in
// bytes the cap silently cut a note the browser had accepted — 2000 typed
// Cyrillic characters are 4000 bytes and survived as ~1000 characters, 2000
// typed emoji as 500 — and nothing on the write path reported the loss. The
// non-Latin cases below are the regression; on ASCII the two units coincide,
// which is why the defect stayed invisible.
func TestTrimDayNotes(t *testing.T) {
	asciiAtLimit := strings.Repeat("a", MaxDayNotesLength)
	cyrillicAtLimit := strings.Repeat("б", MaxDayNotesLength) // 2 bytes per character: 4000 bytes at the limit
	emojiAtLimit := strings.Repeat("😀", MaxDayNotesLength)    // 4 bytes per character: 8000 bytes at the limit

	// Mixed values whose last character straddles the old byte index: under a
	// character cap they are at the limit and must survive whole.
	cyrillicTail := strings.Repeat("a", MaxDayNotesLength-1) + "б"
	emojiTail := strings.Repeat("a", MaxDayNotesLength-1) + "😀"

	tests := []struct {
		name      string
		value     string
		wantWhole bool
	}{
		{"ascii at limit unchanged", asciiAtLimit, true},
		{"cyrillic at limit unchanged", cyrillicAtLimit, true},
		{"emoji at limit unchanged", emojiAtLimit, true},
		{"cyrillic tail at the character limit unchanged", cyrillicTail, true},
		{"emoji tail at the character limit unchanged", emojiTail, true},
		{"ascii over the limit cut to the limit", asciiAtLimit + "overflow", false},
		{"cyrillic over the limit cut to the limit", cyrillicAtLimit + "хвост", false},
		{"emoji over the limit cut to the limit", emojiAtLimit + "😀😀", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TrimDayNotes(tt.value)
			if characters := utf8.RuneCountInString(got); characters > MaxDayNotesLength {
				t.Fatalf("expected trimmed length <= %d characters, got %d", MaxDayNotesLength, characters)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("expected valid UTF-8, got invalid string of length %d bytes", len(got))
			}
			if !strings.HasPrefix(tt.value, got) {
				t.Fatalf("expected the result to be a prefix of the input, got %d bytes", len(got))
			}
			if tt.wantWhole {
				if got != tt.value {
					t.Fatalf(
						"expected a value of %d characters to survive whole, got %d characters (%d of %d bytes)",
						utf8.RuneCountInString(tt.value), utf8.RuneCountInString(got), len(got), len(tt.value),
					)
				}
				return
			}
			if characters := utf8.RuneCountInString(got); characters != MaxDayNotesLength {
				t.Fatalf("expected an over-limit value to be cut to exactly %d characters, got %d", MaxDayNotesLength, characters)
			}
		})
	}
}

// dayNotesMaxlengthPattern reads the maxlength attribute off a template element.
var dayNotesMaxlengthPattern = regexp.MustCompile(`maxlength="(\d+)"`)

// dayNotesTextareaSurfaces are the templates that must carry a notes textarea.
// Naming them keeps the scan from passing vacuously if the markup hook is
// renamed and nothing at all is matched.
var dayNotesTextareaSurfaces = []string{"dashboard.html", "day_editor_partial.html"}

// TestDayNotesTextareaMaxlengthMatchesTheServerCapInCharacters ties the two
// halves of the notes limit together so they cannot drift apart again: the
// number the browser enforces on the textarea and the number the server trims
// at must be the same, AND they must mean the same thing. The number alone is
// not the contract — both sides read 2000 while the server counted bytes and
// the browser characters — so the check is behavioral: the largest value the
// textarea will hand over, spelled in a non-Latin script, has to reach the
// server untouched.
//
// It scans the embedded templates the runtime itself parses, so a notes field
// added on a new surface is covered without anyone remembering this test.
func TestDayNotesTextareaMaxlengthMatchesTheServerCapInCharacters(t *testing.T) {
	found := map[string]int{}
	err := fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		source, readErr := templates.Files.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// Element boundaries are enough here: a textarea's attributes all sit in
		// its opening tag, so splitting on '<' keeps one element's maxlength from
		// being read off its neighbor.
		for _, element := range strings.Split(string(source), "<") {
			if !strings.HasPrefix(element, "textarea") || !strings.Contains(element, "data-dashboard-notes") {
				continue
			}
			found[path]++

			match := dayNotesMaxlengthPattern.FindStringSubmatch(element)
			if match == nil {
				t.Errorf("%s: the notes textarea declares no maxlength, so the browser accepts input the server will cut.\nelement: <%s", path, strings.TrimSpace(element))
				continue
			}
			declared, convErr := strconv.Atoi(match[1])
			if convErr != nil {
				t.Errorf("%s: unreadable maxlength %q: %v", path, match[1], convErr)
				continue
			}
			if declared != MaxDayNotesLength {
				t.Errorf("%s: the notes textarea declares maxlength=%d but the server caps at %d", path, declared, MaxDayNotesLength)
				continue
			}
			// The unit check. "б" is one character the browser counts once and
			// two bytes the server used to count twice, so a byte-measured cap
			// returns roughly half of what was typed.
			atDeclaredLimit := strings.Repeat("б", declared)
			if got := TrimDayNotes(atDeclaredLimit); got != atDeclaredLimit {
				t.Errorf(
					"%s: maxlength=%d and MaxDayNotesLength=%d agree on the number but not on the unit: %d characters the browser accepts came back as %d.\n"+
						"maxlength counts UTF-16 code units, so the server cap has to be a character cap, not a byte cap.",
					path, declared, MaxDayNotesLength,
					utf8.RuneCountInString(atDeclaredLimit), utf8.RuneCountInString(got),
				)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded templates: %v", err)
	}
	for _, surface := range dayNotesTextareaSurfaces {
		if found[surface] == 0 {
			t.Fatalf("no notes textarea found in %s: the data-dashboard-notes hook moved and this guard stopped checking anything (found: %v)", surface, found)
		}
	}
}

func TestNormalizeDayEntryInputNormalizesCycleFactors(t *testing.T) {
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		Flow: models.FlowNone,
		CycleFactorKeys: []string{
			models.CycleFactorTravel,
			"  STRESS ",
			models.CycleFactorTravel,
			"",
		},
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() unexpected error: %v", err)
	}

	if len(normalized.CycleFactorKeys) != 2 {
		t.Fatalf("expected two normalized cycle factors, got %#v", normalized.CycleFactorKeys)
	}
	if normalized.CycleFactorKeys[0] != models.CycleFactorStress || normalized.CycleFactorKeys[1] != models.CycleFactorTravel {
		t.Fatalf("expected stable factor order, got %#v", normalized.CycleFactorKeys)
	}
}

func TestNormalizeDayEntryInputRejectsInvalidCycleFactor(t *testing.T) {
	_, err := NormalizeDayEntryInput(DayEntryInput{
		Flow:            models.FlowNone,
		CycleFactorKeys: []string{models.CycleFactorStress, "unknown"},
	})
	if !errors.Is(err, ErrInvalidDayCycleFactors) {
		t.Fatalf("expected ErrInvalidDayCycleFactors, got %v", err)
	}
}

func TestNormalizeDayEntryInputRejectsInvalidPregnancyTest(t *testing.T) {
	_, err := NormalizeDayEntryInput(DayEntryInput{
		Flow:          models.FlowNone,
		PregnancyTest: "bad-test",
	})
	if !errors.Is(err, ErrInvalidDayPregnancyTest) {
		t.Fatalf("expected ErrInvalidDayPregnancyTest, got %v", err)
	}
}

func TestNormalizeDayEntryInputNormalizesPregnancyTest(t *testing.T) {
	normalized, err := NormalizeDayEntryInput(DayEntryInput{
		Flow:          models.FlowNone,
		PregnancyTest: " POSITIVE ",
	})
	if err != nil {
		t.Fatalf("NormalizeDayEntryInput() unexpected error: %v", err)
	}
	if normalized.PregnancyTest != models.PregnancyTestPositive {
		t.Fatalf("expected pregnancy test %q, got %q", models.PregnancyTestPositive, normalized.PregnancyTest)
	}
}

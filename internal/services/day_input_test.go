package services

import (
	"errors"
	"io/fs"
	"regexp"
	"slices"
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

// TestNormalizeDayEntryInputDropsAPreservedFieldsIncomingValue pins the order
// the two halves of a hidden-field save run in. A write marks Preserve* for
// every field the account keeps hidden, and mergePreservedDayEntryInput
// replaces each of them with the value already stored — but validation runs
// first, so an incoming value this write had already promised to ignore could
// refuse the whole day, under a sentinel naming a field the answer never looks
// at.
//
// No HTTP request delivers such a value any more: transport does not read a
// hidden field at all (parseDayPayload), and even before that only bbt and
// cycle factors could arrive here unsanitized, since transport normalizes sex
// activity and cervical mucus to a valid spelling and merely trims notes. This
// table is therefore the service's own contract — held for any caller, not
// inherited from what one transport happens to filter.
//
// Each row asserts the whole entry rather than its own field: the value under
// test neutralised, the four preservable neighbours the caller did send
// returned unchanged, and the five fields no flag can preserve — the period
// day, its flow, the mood, the pregnancy test, the symptoms — returned
// unchanged too, so a drop reaching past its own flag cannot pass. Five
// rows carry a value that IS refused on its own and re-submit it without the
// flag to require that refusal; the sixth, a note, has no invalid spelling, so
// its control requires the opposite — that without the flag the note survives
// — which is what makes the drop attributable to the flag rather than to an
// unconditional reset.
func TestNormalizeDayEntryInputDropsAPreservedFieldsIncomingValue(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name                   string
		send                   func(input *DayEntryInput)
		neutral                func(input *DayEntryInput)
		preserve               func(input *DayEntryInput)
		wantErrWithoutPreserve error
	}{
		{
			name:                   "a bbt outside the physiological range",
			send:                   func(input *DayEntryInput) { input.BBT = bbtPtr(20) },
			neutral:                func(input *DayEntryInput) { input.BBT = nil },
			preserve:               func(input *DayEntryInput) { input.PreserveBBT = true },
			wantErrWithoutPreserve: ErrInvalidDayBBT,
		},
		{
			// The "not measured" sentinel is read in the owner's own unit, one
			// layer up (ConvertDayBBTToStorage), so by here a non-positive value
			// is simply a reading below the range — IsValidDayBBT knows no
			// sentinel — and its control expects the same refusal as the row
			// above. It is listed because a preserved bbt is dropped whatever
			// its sign, not only when the range would have caught it.
			name:                   "a non-positive bbt",
			send:                   func(input *DayEntryInput) { input.BBT = bbtPtr(-1) },
			neutral:                func(input *DayEntryInput) { input.BBT = nil },
			preserve:               func(input *DayEntryInput) { input.PreserveBBT = true },
			wantErrWithoutPreserve: ErrInvalidDayBBT,
		},
		{
			name:                   "an unknown sex activity",
			send:                   func(input *DayEntryInput) { input.SexActivity = "not-an-activity" },
			neutral:                func(input *DayEntryInput) { input.SexActivity = models.SexActivityNone },
			preserve:               func(input *DayEntryInput) { input.PreserveSexActivity = true },
			wantErrWithoutPreserve: ErrInvalidDaySexActivity,
		},
		{
			name:                   "an unknown cervical mucus",
			send:                   func(input *DayEntryInput) { input.CervicalMucus = "not-a-mucus" },
			neutral:                func(input *DayEntryInput) { input.CervicalMucus = models.CervicalMucusNone },
			preserve:               func(input *DayEntryInput) { input.PreserveCervicalMucus = true },
			wantErrWithoutPreserve: ErrInvalidDayCervicalMucus,
		},
		{
			name:                   "an unknown cycle factor",
			send:                   func(input *DayEntryInput) { input.CycleFactorKeys = []string{"not_a_factor"} },
			neutral:                func(input *DayEntryInput) { input.CycleFactorKeys = nil },
			preserve:               func(input *DayEntryInput) { input.PreserveCycleFactors = true },
			wantErrWithoutPreserve: ErrInvalidDayCycleFactors,
		},
		{
			name:     "a note",
			send:     func(input *DayEntryInput) { input.Notes = "not this write's subject" },
			neutral:  func(input *DayEntryInput) { input.Notes = "" },
			preserve: func(input *DayEntryInput) { input.PreserveNotes = true },
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			incoming := dayEntryInputWithEveryDayFieldSet()
			testCase.send(&incoming)
			wantDropped := incoming
			testCase.neutral(&wantDropped)

			preserved := incoming
			testCase.preserve(&preserved)
			normalized, err := NormalizeDayEntryInput(preserved)
			if err != nil {
				t.Fatalf("a preserved field must not refuse the day, got %v", err)
			}
			assertDayEntryFields(t, normalized, wantDropped)

			control, err := NormalizeDayEntryInput(incoming)
			if testCase.wantErrWithoutPreserve != nil {
				if !errors.Is(err, testCase.wantErrWithoutPreserve) {
					t.Fatalf("without the preserve flag the same value must answer %v, got %v", testCase.wantErrWithoutPreserve, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("without the preserve flag this value is a valid entry, got %v", err)
			}
			assertDayEntryFields(t, control, incoming)
		})
	}
}

// dayEntryInputWithEveryDayFieldSet is the input every row above starts from.
// All five preservable fields carry a valid, non-neutral value, so the four a
// row is not testing can be required to come back exactly as sent: a row
// starting from the zero value could not tell a drop confined to its own flag
// from one that neutralised the lot.
//
// The fields no flag can preserve carry values too — the period day and its
// flow, the mood, the pregnancy test, the symptoms — because the drop runs over
// a whole DayEntryInput and nothing in its signature confines it to the five. A
// reset reaching past them would unset the day the owner did send, which is the
// same defect one field further out.
func dayEntryInputWithEveryDayFieldSet() DayEntryInput {
	return DayEntryInput{
		IsPeriod:        true,
		Flow:            models.FlowMedium,
		Mood:            4,
		PregnancyTest:   models.PregnancyTestNegative,
		SymptomIDs:      []uint{7},
		SexActivity:     models.SexActivityProtected,
		BBT:             bbtPtr(36.5),
		CervicalMucus:   models.CervicalMucusCreamy,
		CycleFactorKeys: []string{models.CycleFactorStress},
		Notes:           "a neighbour this write does carry",
	}
}

// assertDayEntryFields compares a normalized entry against the whole input the
// caller sent: first the five preservable fields, which are each row's subject,
// then the five no flag can preserve, which are the cross-check.
//
// The slices are compared element by element. Joined into one string on a
// comma, ["a,b"] and ["a", "b"] are the same text and two different sets, so a
// comparison that reads as "the same keys" would also pass a set some
// normalization had rewritten. A nil slice and an empty one stay equal — "no
// cycle factors" is one answer with two spellings.
func assertDayEntryFields(t *testing.T, got DayEntryInput, want DayEntryInput) {
	t.Helper()

	if got.SexActivity != want.SexActivity {
		t.Fatalf("sex activity: expected %q, got %q", want.SexActivity, got.SexActivity)
	}
	if !sameDayBBT(got.BBT, want.BBT) {
		t.Fatalf("bbt: expected %s, got %s", describeDayBBT(want.BBT), describeDayBBT(got.BBT))
	}
	if got.CervicalMucus != want.CervicalMucus {
		t.Fatalf("cervical mucus: expected %q, got %q", want.CervicalMucus, got.CervicalMucus)
	}
	if !slices.Equal(got.CycleFactorKeys, want.CycleFactorKeys) {
		t.Fatalf("cycle factors: expected %#v, got %#v", want.CycleFactorKeys, got.CycleFactorKeys)
	}
	if got.Notes != want.Notes {
		t.Fatalf("notes: expected %q, got %q", want.Notes, got.Notes)
	}

	if got.IsPeriod != want.IsPeriod {
		t.Fatalf("is period: expected %t, got %t", want.IsPeriod, got.IsPeriod)
	}
	if got.Flow != want.Flow {
		t.Fatalf("flow: expected %q, got %q", want.Flow, got.Flow)
	}
	if got.Mood != want.Mood {
		t.Fatalf("mood: expected %d, got %d", want.Mood, got.Mood)
	}
	if got.PregnancyTest != want.PregnancyTest {
		t.Fatalf("pregnancy test: expected %q, got %q", want.PregnancyTest, got.PregnancyTest)
	}
	if !slices.Equal(got.SymptomIDs, want.SymptomIDs) {
		t.Fatalf("symptom ids: expected %#v, got %#v", want.SymptomIDs, got.SymptomIDs)
	}
}

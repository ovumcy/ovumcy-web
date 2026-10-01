package workflowfile

import (
	"errors"
	"slices"
	"testing"
)

// TestJobIfAndJobConditionReadOnlyTheJobsOwnIf holds the job-level `if:` reader
// to the shapes it answers for and refuses every other one.
func TestJobIfAndJobConditionReadOnlyTheJobsOwnIf(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		block     string
		wantIf    string // JobIf; "" means refused
		wantCond  string // JobCondition; "" means refused
		wantFold  string // JobFoldedCondition; "" means refused
		wantNoKey bool
	}{
		{
			name:     "inline wrapped",
			block:    "    needs: a\n    if: ${{ a == 'b c' }}\n    steps:\n      - name: x\n        if: always()\n",
			wantIf:   "${{ a == 'b c' }}",
			wantCond: "a == 'b c'",
			wantFold: "a == 'b c'",
		},
		{
			name:     "inline bare",
			block:    "    if: a  ==  'b'\n",
			wantIf:   "a  ==  'b'",
			wantCond: "a  ==  'b'",
			wantFold: "a  ==  'b'",
		},
		{
			name:     "double-quoted value is refused by the condition reader only",
			block:    "    if: \"a\"\n",
			wantIf:   "\"a\"",
			wantCond: "",
		},
		{name: "missing if", block: "    needs: a\n    steps:\n      - name: x\n        if: always()\n", wantNoKey: true},
		{name: "block scalar folded", block: "    if: >-\n      ${{ a\n      && b }}\n", wantFold: "a && b"},
		{name: "block scalar literal", block: "    if: |\n      ${{ a }}\n", wantFold: "a"},
		{name: "block scalar without lines", block: "    if: >\n    steps:\n"},
		{name: "plain scalar continued onto a deeper line", block: "    if: a\n      || always()\n"},
		{name: "empty value", block: "    if:\n    steps:\n"},
		{name: "duplicated key", block: "    if: a\n    if: b\n"},
		{name: "key glued to its value", block: "    if:a\n"},
		{name: "half a wrapper opened", block: "    if: ${{ a\n", wantIf: "${{ a"},
		{name: "half a wrapper closed", block: "    if: a }}\n", wantIf: "a }}"},
		{name: "empty wrapper", block: "    if: ${{ }}\n", wantIf: "${{ }}"},
		{name: "unwrapped value opening on a YAML indicator", block: "    if: !cancelled() && a\n", wantIf: "!cancelled() && a"},
		{name: "trailing comment", block: "    if: a # b\n", wantIf: "a # b"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := JobIf(testCase.block)
			checkRead(t, "JobIf", got, err, testCase.wantIf, testCase.wantNoKey)

			got, err = JobCondition(testCase.block)
			if testCase.wantNoKey {
				checkRead(t, "JobCondition", got, err, "", true)
			} else {
				checkRead(t, "JobCondition", got, err, testCase.wantCond, false)
			}

			got, err = JobFoldedCondition(testCase.block)
			checkRead(t, "JobFoldedCondition", got, err, testCase.wantFold, testCase.wantNoKey)
		})
	}
}

// checkRead asserts one reader's answer: want == "" is a refusal, and
// noKey narrows the refusal to the absent-key sentinel.
func checkRead(t *testing.T, reader, got string, err error, want string, noKey bool) {
	t.Helper()

	switch {
	case noKey:
		if !errors.Is(err, ErrNoSuchKey) {
			t.Errorf("%s = %q, %v; want ErrNoSuchKey", reader, got, err)
		}
	case want == "":
		if err == nil {
			t.Errorf("%s read %q, want a refusal", reader, got)
		}
	case err != nil || got != want:
		t.Errorf("%s = %q, %v; want %q", reader, got, err, want)
	}
}

// TestJobNeedsReadsEveryListFormAndRefusesTheRest covers the three spellings of
// a dependency list and the shapes that must not read as one.
func TestJobNeedsReadsEveryListFormAndRefusesTheRest(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		block string
		want  []string // nil means refused
	}{
		{"scalar", "    needs: changes\n    if: x\n", []string{"changes"}},
		{"flow list", "    needs: [ build, changes ]\n", []string{"build", "changes"}},
		{"block list with a comment line and blank lines", "    needs:\n      # the gate\n      - build\n      \n      - changes\n    if: x\n", []string{"build", "changes"}},
		{"keys only inside a step", "    steps:\n      - name: x\n        needs: a\n", nil},
		{"duplicated key", "    needs: a\n    needs: b\n", nil},
		{"scalar continued onto a deeper line", "    needs: a\n      b\n", nil},
		{"block scalar", "    needs: |\n      a\n", nil},
		{"quoted entry", "    needs: 'a'\n", nil},
		{"trailing comment", "    needs: a # b\n", nil},
		{"empty flow list", "    needs: []\n", nil},
		{"flow list across lines", "    needs: [a,\n      b]\n", nil},
		{"empty item in a flow list", "    needs: [a, , b]\n", nil},
		{"block item that is not a job id", "    needs:\n      - a\n      - { b: c }\n", nil},
		{"block list at the key's own depth", "    needs:\n    - a\n", nil},
		{"key with neither value nor list", "    needs:\n    runs-on: x\n", nil},
		{"neighbouring key is not read", "    runs-on: x\n    needs-not: a\n", nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := JobNeeds(testCase.block)
			if testCase.want == nil {
				if err == nil {
					t.Fatalf("read %q, want a refusal", got)
				}
				return
			}
			if err != nil || !slices.Equal(got, testCase.want) {
				t.Fatalf("= %q, %v; want %q", got, err, testCase.want)
			}
		})
	}

	if _, err := JobNeeds("    runs-on: x\n"); !errors.Is(err, ErrNoSuchKey) {
		t.Fatalf("a job with no `needs:` returned %v, want ErrNoSuchKey", err)
	}
}

// TestNeighbouringJobDoesNotBleedIntoTheJobsKeys cuts two adjacent jobs out of
// one document with Job's own cutter and reads each: the second job's `if:` and
// `needs:` are not the first's.
func TestNeighbouringJobDoesNotBleedIntoTheJobsKeys(t *testing.T) {
	document := "name: w\njobs:\n  first:\n    runs-on: x\n    steps:\n      - run: y\n  second:\n    needs: first\n    if: ${{ always() }}\n"

	first, err := jobIn(document, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := JobIf(first); !errors.Is(err, ErrNoSuchKey) {
		t.Errorf("first job's `if:` = %v, want ErrNoSuchKey (the neighbour's must not bleed in)", err)
	}
	if _, err := JobNeeds(first); !errors.Is(err, ErrNoSuchKey) {
		t.Errorf("first job's `needs:` = %v, want ErrNoSuchKey", err)
	}

	second, err := jobIn(document, "second")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := JobCondition(second); err != nil || got != "always()" {
		t.Errorf("second job's condition = %q, %v; want always()", got, err)
	}
}

func TestWithoutSpacesKeepsAQuotedLiteralWhole(t *testing.T) {
	if got, want := WithoutSpaces("a  ==\t'b c'\n&& d"), "a=='b c'&&d"; got != want {
		t.Fatalf("WithoutSpaces = %q, want %q", got, want)
	}
}

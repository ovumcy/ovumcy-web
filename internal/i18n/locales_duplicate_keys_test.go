package i18n

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"testing"
)

// json.Unmarshal into a map keeps the LAST member of a duplicated name and
// discards the earlier one without a word. Every check this package runs goes
// through that same unmarshal — NewManager, TestLocaleKeysParity,
// mustLoadAllLocaleMessages — so a key declared twice in one catalogue is
// invisible to all of them: the first definition is dead weight today and a
// trap tomorrow, when someone edits the copy they can see and ships no change
// at all. `dashboard.fertile_window` was declared twice in all six catalogues.
//
// Seeing it needs a reader that does not collapse the object, which is what the
// token stream below is: json.Decoder.Token() yields every member name in
// source order, duplicates included.
//
// It reads the EMBEDDED files rather than the directory on disk, so it covers
// exactly the bytes the binary ships.

// localeMemberDuplicate is one member name declared more than once inside a
// single JSON object.
type localeMemberDuplicate struct {
	// object is the JSON path of the enclosing object, "" for the document
	// root. Locale files are flat today; carrying the path means a nested one
	// would still be reported at the place it happened.
	object      string
	name        string
	occurrences int
}

func (duplicate localeMemberDuplicate) String() string {
	where := "the top-level object"
	if duplicate.object != "" {
		where = "object " + duplicate.object
	}
	return fmt.Sprintf("%q is declared %d times in %s", duplicate.name, duplicate.occurrences, where)
}

func TestNoLocaleFileDeclaresAKeyTwice(t *testing.T) {
	entries, err := localeFiles.ReadDir(localesDir)
	if err != nil {
		t.Fatalf("read embedded locales dir: %v", err)
	}

	checked := 0
	var failures []string
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".json" {
			continue
		}
		name := path.Join(localesDir, entry.Name())
		content, readErr := fs.ReadFile(localeFiles, name)
		if readErr != nil {
			t.Fatalf("read embedded locale %s: %v", name, readErr)
		}

		duplicates, checkErr := duplicateJSONMemberNames(content)
		if checkErr != nil {
			t.Fatalf("scanning %s: %v", name, checkErr)
		}
		checked++
		for _, duplicate := range duplicates {
			failures = append(failures, fmt.Sprintf("  %s: %s", name, duplicate))
		}
	}

	// The sweep must have read the catalogues it exists for; a run that found
	// no file would report a clean bill of health about nothing.
	//
	// A FLOOR, not an equality. Scanning more files than the required set is
	// this check doing more work, not less — an extra catalogue in the
	// directory is still scanned for duplicates, and whether that file belongs
	// here is TestLocaleKeysParity's question, not this one. Written as `!=`,
	// an unregistered locale file would fail here with "passed without reading
	// the catalogues" while every catalogue had in fact been read, sending the
	// reader to look for a sweep that skipped a directory.
	if checked < len(requiredLocales) {
		t.Fatalf("scanned %d embedded locale file(s), fewer than the %d this build requires; the sweep missed catalogues rather than clearing them", checked, len(requiredLocales))
	}
	if len(failures) == 0 {
		return
	}

	t.Fatalf("%d duplicate member name(s) in the embedded locale files:\n%s\n"+
		"json.Unmarshal keeps the last definition, so the earlier one never renders and no map-based test can see the "+
		"difference. Delete the earlier occurrence — that leaves the value the loader was already using.",
		len(failures), strings.Join(failures, "\n"))
}

// duplicateJSONMemberNames reports every object member name declared more than
// once, reading the document as a token stream.
//
// Deliberately NOT via json.Unmarshal into a map or into any Go value: the
// collapse an unmarshal performs is the exact blindness this function exists to
// remove, so a version of it built on one could not fail.
func duplicateJSONMemberNames(content []byte) ([]localeMemberDuplicate, error) {
	// One frame per open container. Arrays get a frame too: without one, the
	// second element of ["a", "a"] would be read as a member name of the
	// enclosing object and reported as a duplicate that is not there.
	type frame struct {
		isObject bool
		path     string
		counts   map[string]int
		order    []string
		member   string
	}

	decoder := json.NewDecoder(bytes.NewReader(content))
	var stack []frame
	var findings []localeMemberDuplicate

	// expectingMemberName is true exactly when the next token inside the
	// innermost container is a member name rather than a value.
	expectingMemberName := false
	inObject := func() bool { return len(stack) > 0 && stack[len(stack)-1].isObject }
	childPath := func() string {
		if len(stack) == 0 {
			return ""
		}
		parent := stack[len(stack)-1]
		if !parent.isObject || parent.member == "" {
			return parent.path
		}
		return strings.TrimPrefix(parent.path+"."+parent.member, ".")
	}

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				stack = append(stack, frame{isObject: true, path: childPath(), counts: map[string]int{}})
				expectingMemberName = true
			case '[':
				stack = append(stack, frame{path: childPath()})
				expectingMemberName = false
			case '}':
				closed := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				for _, name := range closed.order {
					if count := closed.counts[name]; count > 1 {
						findings = append(findings, localeMemberDuplicate{object: closed.path, name: name, occurrences: count})
					}
				}
				expectingMemberName = inObject()
			case ']':
				stack = stack[:len(stack)-1]
				expectingMemberName = inObject()
			}
			continue
		}

		if expectingMemberName {
			name, ok := token.(string)
			if !ok {
				return nil, fmt.Errorf("expected a member name, got %T", token)
			}
			scope := &stack[len(stack)-1]
			if scope.counts[name] == 0 {
				scope.order = append(scope.order, name)
			}
			scope.counts[name]++
			scope.member = name
			expectingMemberName = false
			continue
		}

		// A scalar value: inside an object the next token is a member name,
		// inside an array it is the next element.
		expectingMemberName = inObject()
	}

	sort.Slice(findings, func(a, b int) bool {
		if findings[a].object != findings[b].object {
			return findings[a].object < findings[b].object
		}
		return findings[a].name < findings[b].name
	})
	return findings, nil
}

// The checker is measured on fixtures rather than on the repository's own
// catalogues, so it keeps proving it can fail on the day the catalogues are
// clean — which, after this change, is every day.
func TestDuplicateJSONMemberNamesFailsOnADuplicateAndPassesOnACleanFile(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "clean flat catalogue",
			content: `{"a.one": "A", "a.two": "B", "b": "C"}`,
		},
		{
			name:    "the shape that shipped",
			content: `{"dashboard.fertile_window": "Fertile window", "x": "X", "dashboard.fertile_window": "Fertile window"}`,
			want:    []string{`"dashboard.fertile_window" is declared 2 times in the top-level object`},
		},
		{
			name:    "identical values are still a duplicate",
			content: `{"a": "same", "a": "same"}`,
			want:    []string{`"a" is declared 2 times in the top-level object`},
		},
		{
			name: "a value that merely repeats a name is not a duplicate",
			// The blind spelling — searching the text for a repeated string —
			// would report this one and be abandoned as noisy.
			content: `{"a": "b", "b": "a"}`,
		},
		{
			name:    "declared three times",
			content: `{"a": "1", "a": "2", "a": "3"}`,
			want:    []string{`"a" is declared 3 times in the top-level object`},
		},
		{
			name:    "the same name in two different objects is not a duplicate",
			content: `{"outer": {"a": "1"}, "other": {"a": "2"}}`,
		},
		{
			name:    "a duplicate inside a nested object is named where it happened",
			content: `{"outer": {"a": "1", "a": "2"}}`,
			want:    []string{`"a" is declared 2 times in object outer`},
		},
		{
			name:    "a member name repeated inside an array element is not confused for one",
			content: `{"list": ["a", "a"], "a": "1"}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			findings, err := duplicateJSONMemberNames([]byte(testCase.content))
			if err != nil {
				t.Fatalf("scanning the fixture: %v", err)
			}
			var got []string
			for _, finding := range findings {
				got = append(got, finding.String())
			}
			if strings.Join(got, "; ") != strings.Join(testCase.want, "; ") {
				t.Fatalf("reported %v, want %v", got, testCase.want)
			}
		})
	}
}

// The blindness being fixed, stated as a test: the same fixture read the way
// every other check in this package reads a catalogue reports nothing wrong.
// Without this, a later refactor could quietly rebuild the checker on an
// unmarshal and the suite would stay green.
func TestUnmarshallingIntoAMapCannotSeeADuplicateMemberName(t *testing.T) {
	const content = `{"dashboard.fertile_window": "first", "dashboard.fertile_window": "second"}`

	messages := map[string]string{}
	if err := json.Unmarshal([]byte(content), &messages); err != nil {
		t.Fatalf("unmarshalling the fixture: %v", err)
	}
	if len(messages) != 1 || messages["dashboard.fertile_window"] != "second" {
		t.Fatalf("the unmarshal produced %v; this test documents that it collapses to the LAST definition, which is why the survivor of a de-duplication must be the later one", messages)
	}

	findings, err := duplicateJSONMemberNames([]byte(content))
	if err != nil {
		t.Fatalf("scanning the fixture: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("the token reader found %d duplicate(s) where the map found none; it must find exactly one, or it is no better than the map", len(findings))
	}
}

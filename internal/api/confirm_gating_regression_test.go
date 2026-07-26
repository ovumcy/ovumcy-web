package api

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/templates"
)

// htmxRequestAttributePattern matches the htmx attributes that make an element
// issue its own request. An element carrying one of these never reaches the
// document-level submit interceptor in time (see the test below).
var htmxRequestAttributePattern = regexp.MustCompile(`\bhx-(get|post|put|patch|delete)\s*=`)

// TestNoTemplateElementMixesHTMXRequestWithDataConfirm kills the whole class
// behind a defect measured in the browser: `data-confirm` is honored by a
// document-level `submit` listener (web/src/js/app/22-confirm-modal.js), while
// htmx listens on the element itself. The element-level listener runs first, so
// htmx issues the request before the dialog is ever shown — pressing Cancel
// still rotated the calendar feed, hid a symptom, or deleted a day entry, and
// accepting fired the request a second time. The dialog was decorative on
// exactly those surfaces.
//
// An htmx-driven element must therefore use `hx-confirm`, which htmx itself
// gates on: the same modal serves it through the `htmx:confirm` handler and
// reads `data-confirm-accept` for the button label. `data-confirm` stays
// correct for plain non-htmx forms (logout, recovery-code regeneration), which
// really do submit natively.
//
// This scans the embedded template sources rather than a rendered page because
// the defect is a markup contract, not a per-page behavior: a new surface added
// tomorrow is covered without anyone remembering to write a test for it.
func TestNoTemplateElementMixesHTMXRequestWithDataConfirm(t *testing.T) {
	var scanned int
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
		scanned++

		// Element boundaries are enough here: every confirm-bearing element in
		// this codebase is a <form ...> or <button ...> whose attributes sit in
		// one tag. Splitting on '<' keeps an hx-* on one element from being
		// paired with a data-confirm on its neighbor.
		for _, element := range strings.Split(string(source), "<") {
			if !strings.Contains(element, "data-confirm=") {
				continue
			}
			if htmxRequestAttributePattern.MatchString(element) {
				t.Errorf(
					"%s: an element carries both an htmx request attribute and data-confirm.\n"+
						"htmx submits before the document-level data-confirm interceptor runs, so the dialog is decorative and Cancel does not cancel.\n"+
						"Use hx-confirm for htmx-driven elements (keep data-confirm-accept for the button label); data-confirm is only for plain non-htmx forms.\n"+
						"element: <%s",
					path, strings.TrimSpace(element),
				)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded templates: %v", err)
	}
	if scanned == 0 {
		t.Fatal("no templates were scanned: the embedded template FS layout changed and this guard silently stopped checking anything")
	}
}

// TestConfirmGatedSurfacesDeclareAnAcceptLabel pins the other half of the
// contract: both gating mechanisms render the accept button's label from
// data-confirm-accept, so a surface that asks for confirmation without one
// falls back to the generic body-level default and shows "Delete" on a rotate
// or hide action.
func TestConfirmGatedSurfacesDeclareAnAcceptLabel(t *testing.T) {
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
		for _, element := range strings.Split(string(source), "<") {
			asksToConfirm := strings.Contains(element, "data-confirm=") || strings.Contains(element, "hx-confirm=")
			if !asksToConfirm {
				continue
			}
			if !strings.Contains(element, "data-confirm-accept=") {
				t.Errorf("%s: a confirm-gated element declares no data-confirm-accept, so its dialog would show the generic default label.\nelement: <%s", path, strings.TrimSpace(element))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded templates: %v", err)
	}
}

package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// A row refusal is rendered inside the row it names. When the id names no row
// of the caller's card — another owner's symptom, one that never existed, a
// built-in — that row is not in the answer, and the refusal used to vanish
// with it: a 200 that re-rendered the card unchanged, read by the owner as
// "nothing happened". The refusal must surface on the card itself, and it must
// read the same for another owner's id as for an id that never existed.
func TestSettingsSymptomsHTMXRefusesAnotherOwnersSymptomWithoutAnOracle(t *testing.T) {
	ctx := newSettingsSymptomsHTMXTestContext(t, "settings-symptoms-refusal-b@example.com")
	other := createOnboardingTestUser(t, ctx.database, "settings-symptoms-refusal-a@example.com", "StrongPass1", true)

	archivedAt := time.Date(2026, time.March, 1, 12, 0, 0, 0, time.UTC)
	foreignActive := models.SymptomType{UserID: other.ID, Name: "Owner A active", Icon: "💧", Color: "#38BDF8"}
	foreignArchived := models.SymptomType{UserID: other.ID, Name: "Owner A archived", Icon: "🔥", Color: "#38BDF8", ArchivedAt: &archivedAt}
	for _, symptom := range []*models.SymptomType{&foreignActive, &foreignArchived} {
		if err := ctx.database.Create(symptom).Error; err != nil {
			t.Fatalf("create owner A symptom: %v", err)
		}
	}
	never := foreignArchived.ID + 1000

	notFound := i18nMessageForTest(t, i18n.LangEN, "settings.symptoms.error.not_found")
	cases := []struct {
		name    string
		method  string
		path    func(uint) string
		form    url.Values
		foreign models.SymptomType
	}{
		{
			name:    "rename",
			method:  http.MethodPatch,
			path:    func(id uint) string { return "/api/v1/symptoms/" + strconv.FormatUint(uint64(id), 10) },
			form:    url.Values{"csrf_token": {ctx.csrfToken}, "name": {"Taken over"}, "icon": {"⚡"}},
			foreign: foreignActive,
		},
		{
			name:    "hide",
			method:  http.MethodDelete,
			path:    func(id uint) string { return "/api/v1/symptoms/" + strconv.FormatUint(uint64(id), 10) },
			form:    url.Values{"csrf_token": {ctx.csrfToken}},
			foreign: foreignActive,
		},
		{
			name:    "restore",
			method:  http.MethodPost,
			path:    func(id uint) string { return "/api/v1/symptoms/" + strconv.FormatUint(uint64(id), 10) + "/restore" },
			form:    url.Values{"csrf_token": {ctx.csrfToken}},
			foreign: foreignArchived,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			foreignBody := performSettingsSymptomsHTMXRequest(t, ctx, tc.method, tc.path(tc.foreign.ID), tc.form)
			neverBody := performSettingsSymptomsHTMXRequest(t, ctx, tc.method, tc.path(never), tc.form)

			assertSymptomCardRefusal(t, foreignBody, notFound)
			if foreignBody != neverBody {
				t.Fatal("another owner's symptom id is answered differently from an id that never existed: the response is an existence oracle")
			}
			foreignID := strconv.FormatUint(uint64(tc.foreign.ID), 10)
			for _, leak := range []string{"settings-symptom-row-" + foreignID, "/api/v1/symptoms/" + foreignID, tc.foreign.Name} {
				if strings.Contains(foreignBody, leak) {
					t.Fatalf("the refusal echoes owner A's symptom (%q)", leak)
				}
			}

			stored := models.SymptomType{}
			if err := ctx.database.First(&stored, tc.foreign.ID).Error; err != nil {
				t.Fatalf("reload owner A symptom: %v", err)
			}
			if stored.UserID != other.ID || stored.Name != tc.foreign.Name || stored.Icon != tc.foreign.Icon ||
				(stored.ArchivedAt == nil) != (tc.foreign.ArchivedAt == nil) {
				t.Fatalf("owner A's symptom was mutated by owner B: %+v", stored)
			}
		})
	}
}

// A built-in symptom has no row on the settings card either, so its refusal
// takes the same card-level path.
func TestSettingsSymptomsHTMXRefusalOfABuiltinSymptomIsRendered(t *testing.T) {
	ctx := newSettingsSymptomsHTMXTestContext(t, "settings-symptoms-refusal-builtin@example.com")
	builtin := models.SymptomType{UserID: ctx.user.ID, Name: "Builtin probe", Icon: "✨", Color: "#38BDF8", IsBuiltin: true}
	if err := ctx.database.Create(&builtin).Error; err != nil {
		t.Fatalf("create built-in symptom: %v", err)
	}

	form := url.Values{"csrf_token": {ctx.csrfToken}, "name": {"Renamed"}, "icon": {"✨"}}
	body := performSettingsSymptomsHTMXRequest(t, ctx, http.MethodPatch, "/api/v1/symptoms/"+strconv.FormatUint(uint64(builtin.ID), 10), form)
	assertSymptomCardRefusal(t, body, i18nMessageForTest(t, i18n.LangEN, "settings.symptoms.error.builtin_edit_forbidden"))
}

// A refusal for a row the card does render stays in that row: the card-level
// slot is only for an id the card has no row for.
func TestSettingsSymptomsHTMXRowRefusalStaysOffTheCardSlot(t *testing.T) {
	ctx := newSettingsSymptomsHTMXTestContext(t, "settings-symptoms-refusal-row@example.com")
	symptom := models.SymptomType{UserID: ctx.user.ID, Name: "Alpha", Icon: "💧", Color: "#38BDF8"}
	if err := ctx.database.Create(&symptom).Error; err != nil {
		t.Fatalf("create custom symptom: %v", err)
	}

	form := url.Values{"csrf_token": {ctx.csrfToken}, "name": {strings.Repeat("x", 80)}, "icon": {"💧"}}
	body := performSettingsSymptomsHTMXRequest(t, ctx, http.MethodPatch, "/api/v1/symptoms/"+strconv.FormatUint(uint64(symptom.ID), 10), form)
	document := mustParseHTMLDocument(t, body)
	if len(htmlFindElements(document, func(node *html.Node) bool { return htmlHasAttr(node, "data-symptom-refusal") })) != 0 {
		t.Fatal("a refusal for a rendered row must not also appear in the card-level slot")
	}
}

func assertSymptomCardRefusal(t *testing.T, body string, want string) {
	t.Helper()

	document := mustParseHTMLDocument(t, body)
	if htmlElementByAttr(document, "id", "settings-symptoms") == nil {
		t.Fatalf("expected the symptoms card back, got %s", body)
	}
	refusals := htmlFindElements(document, func(node *html.Node) bool {
		return htmlHasAttr(node, "data-symptom-refusal")
	})
	if len(refusals) != 1 {
		t.Fatalf("expected exactly one card-level refusal, got %d in %s", len(refusals), body)
	}
	refusal := refusals[0]
	if got := strings.TrimSpace(htmlNodeText(refusal)); got != want {
		t.Fatalf("card-level refusal reads %q, want %q", got, want)
	}
	if !htmlHasAttr(refusal, "data-focus-after-swap") {
		t.Fatal("the card-level refusal must carry data-focus-after-swap, so focus and the announcement land on it after the swap")
	}
	describedBy := htmlAttr(refusal, "aria-describedby")
	described := htmlElementByAttr(document, "id", describedBy)
	if describedBy == "" || described == nil || strings.TrimSpace(htmlNodeText(described)) != want {
		t.Fatalf("the refusal's aria-describedby (%q) must name the node holding its text", describedBy)
	}
}

func i18nMessageForTest(t *testing.T, language string, key string) string {
	t.Helper()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	message, ok := manager.Messages(language)[key]
	if !ok || strings.TrimSpace(message) == "" {
		t.Fatalf("locale %s has no message for %s", language, key)
	}
	return message
}

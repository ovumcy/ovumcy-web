package api

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/templates"
	"golang.org/x/net/html"
)

// templateFormRoute is one form the templates submit without JavaScript: the
// verb the server routes it as (its hidden _method, POST otherwise) and the
// action path, query dropped, with every template action reduced to one
// placeholder segment.
type templateFormRoute struct {
	file   string
	verb   string
	action string
}

const templateFormPlaceholder = "TPLACTION"

var templateFormActionPattern = regexp.MustCompile(`(?s)\{\{.*?\}\}`)

// parameterisedFormComponents names each form component whose action is a
// template parameter, by the dict key its callers fill. The scan resolves the
// component to every literal path a caller passes under that key.
var parameterisedFormComponents = map[string]string{
	"components/cycle_start_form.html": "Endpoint",
}

// noJSAPIFormsInTemplates returns every method="post" form whose action is
// under /api/v1/, or is a template parameter: a parameterised component is
// expanded to the paths its callers pass, and any other non-literal action is
// returned as it is, so the caller fails on it instead of skipping a route it
// cannot name.
func noJSAPIFormsInTemplates(t *testing.T) []templateFormRoute {
	t.Helper()
	sources := map[string]string{}
	err := fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		source, err := fs.ReadFile(templates.Files, path)
		sources[path] = string(source)
		return err
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}

	var forms []templateFormRoute
	for path, source := range sources {
		for _, form := range postFormsInTemplate(path, source) {
			key, parameterised := parameterisedFormComponents[path]
			if !parameterised || form.action != templateFormPlaceholder {
				forms = append(forms, form)
				continue
			}
			callers := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s+\(printf\s+"([^"]+)"`)
			expanded := 0
			for _, caller := range sources {
				for _, match := range callers.FindAllStringSubmatch(caller, -1) {
					action, _, _ := strings.Cut(strings.ReplaceAll(match[1], "%s", templateFormPlaceholder), "?")
					forms = append(forms, templateFormRoute{file: path, verb: form.verb, action: action})
					expanded++
				}
			}
			if expanded == 0 {
				t.Errorf("%s: no template passes %q to this component, so the scan cannot name its route", path, key)
			}
		}
	}
	return forms
}

func postFormsInTemplate(path string, source string) []templateFormRoute {
	stripped := templateFormActionPattern.ReplaceAllString(source, templateFormPlaceholder)
	var forms []templateFormRoute
	var current *templateFormRoute
	tokenizer := html.NewTokenizer(strings.NewReader(stripped))
	for kind := tokenizer.Next(); kind != html.ErrorToken; kind = tokenizer.Next() {
		token := tokenizer.Token()
		attrs := map[string]string{}
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		switch {
		case kind == html.StartTagToken && token.Data == "form":
			current = nil
			action, _, _ := strings.Cut(attrs["action"], "?")
			if !strings.EqualFold(attrs["method"], "post") || (strings.HasPrefix(action, "/") && !strings.HasPrefix(action, "/api/v1/")) {
				continue
			}
			forms = append(forms, templateFormRoute{file: path, verb: fiber.MethodPost, action: action})
			current = &forms[len(forms)-1]
		case kind == html.EndTagToken && token.Data == "form":
			current = nil
		case (kind == html.StartTagToken || kind == html.SelfClosingTagToken) && token.Data == "input" && current != nil:
			if attrs["name"] == "_method" && strings.EqualFold(attrs["type"], "hidden") {
				current.verb = strings.ToUpper(attrs["value"])
			}
		}
	}
	return forms
}

// registeredRouteForTemplateForm resolves a form to the route the router sends
// it to: same verb, same segment count, each literal segment equal and each
// placeholder segment standing on a parameter. The route matching the most
// literal segments wins, the way the router prefers a static segment.
func registeredRouteForTemplateForm(routes []fiber.Route, form templateFormRoute) (fiber.Route, bool) {
	actionSegments := strings.Split(form.action, "/")
	var best fiber.Route
	bestLiterals := -1
	for _, route := range routes {
		if route.Method != form.verb {
			continue
		}
		routeSegments := strings.Split(route.Path, "/")
		if len(routeSegments) != len(actionSegments) {
			continue
		}
		matched, literals := true, 0
		for i, segment := range actionSegments {
			isParam := strings.HasPrefix(routeSegments[i], ":")
			switch {
			case strings.Contains(segment, templateFormPlaceholder):
				matched = isParam
			case strings.EqualFold(segment, routeSegments[i]):
				literals++
			default:
				matched = isParam
			}
			if !matched {
				break
			}
		}
		if matched && literals > bestLiterals {
			best, bestLiterals = route, literals
		}
	}
	return best, bestLiterals >= 0
}

package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// JSON success bodies are held to the schema docs/openapi.yaml declares for
// them, in both directions.
//
// Every other spec test reads one side of the contract: the route sweep checks
// that an operation exists, the status sweep that a status can be emitted, the
// bounds probe that a declared limit is enforced. None of them reads a body
// against a property list, so a settings save that answered a `fiber.Map`
// echo while the spec declared the closed `OkResponse` stayed green for as
// long as nobody validated a response — and a client that did was refused the
// server's own answer. The guard below drives each operation through the real
// router and compares what came back with what the document promises: a key
// the schema does not list is drift whatever `additionalProperties` says (an
// open schema documents nothing about the key), and a `required` key the body
// lacks is drift from the other side.
//
// The document is read with a small indentation-based reader rather than a
// YAML library, matching the dependency-free posture of the other spec tests
// in this package; it understands exactly the subset the spec is written in
// (block maps and lists, one-line flow maps and lists, block scalars, which it
// skips).

type openAPINodeKind int

const (
	openAPIScalarNode openAPINodeKind = iota
	openAPIMapNode
	openAPIListNode
)

type openAPINode struct {
	kind   openAPINodeKind
	scalar string
	keys   []string
	fields map[string]*openAPINode
	items  []*openAPINode
}

func newOpenAPIMapNode() *openAPINode {
	return &openAPINode{kind: openAPIMapNode, fields: map[string]*openAPINode{}}
}

func (node *openAPINode) set(key string, value *openAPINode) {
	if _, exists := node.fields[key]; !exists {
		node.keys = append(node.keys, key)
	}
	node.fields[key] = value
}

func (node *openAPINode) get(key string) *openAPINode {
	if node == nil || node.kind != openAPIMapNode {
		return nil
	}
	return node.fields[key]
}

func (node *openAPINode) text() string {
	if node == nil || node.kind != openAPIScalarNode {
		return ""
	}
	return node.scalar
}

type openAPILine struct {
	indent int
	text   string
}

type openAPIReader struct {
	lines []openAPILine
	pos   int
}

func parseOpenAPIDocument(t *testing.T, specPath string) *openAPINode {
	t.Helper()

	source, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}
	reader := &openAPIReader{}
	for _, raw := range strings.Split(string(source), "\n") {
		raw = strings.TrimRight(raw, "\r ")
		trimmed := strings.TrimLeft(raw, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		reader.lines = append(reader.lines, openAPILine{indent: len(raw) - len(trimmed), text: trimmed})
	}
	root := reader.mapping(0)
	if reader.pos != len(reader.lines) {
		t.Fatalf("%s: reader stopped at line %q; the document uses YAML this reader does not understand",
			specPath, reader.lines[reader.pos].text)
	}
	if root.get("paths") == nil || root.get("components").get("schemas") == nil {
		t.Fatalf("%s: no paths or components.schemas parsed; the reader is wrong", specPath)
	}
	return root
}

func isOpenAPIListItem(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

// splitOpenAPIKey splits `key: rest` (or `key:`), honouring a quoted key such
// as `'200':`. A line that is not a key reports ok == false.
func splitOpenAPIKey(text string) (key string, rest string, ok bool) {
	if text != "" && (text[0] == '\'' || text[0] == '"') {
		end := strings.IndexByte(text[1:], text[0])
		if end < 0 || !strings.HasPrefix(text[end+2:], ":") {
			return "", "", false
		}
		return text[1 : end+1], strings.TrimSpace(text[end+3:]), true
	}
	if index := strings.Index(text, ": "); index >= 0 {
		return text[:index], strings.TrimSpace(text[index+2:]), true
	}
	if strings.HasSuffix(text, ":") {
		return strings.TrimSuffix(text, ":"), "", true
	}
	return "", "", false
}

func (reader *openAPIReader) mapping(indent int) *openAPINode {
	node := newOpenAPIMapNode()
	for reader.pos < len(reader.lines) {
		line := reader.lines[reader.pos]
		if line.indent < indent || (line.indent == indent && isOpenAPIListItem(line.text)) {
			break
		}
		if line.indent > indent {
			return node
		}
		key, rest, ok := splitOpenAPIKey(line.text)
		if !ok {
			return node
		}
		reader.pos++
		node.set(key, reader.value(indent, rest))
	}
	return node
}

func (reader *openAPIReader) list(indent int) *openAPINode {
	node := &openAPINode{kind: openAPIListNode}
	for reader.pos < len(reader.lines) {
		line := reader.lines[reader.pos]
		if line.indent != indent || !isOpenAPIListItem(line.text) {
			break
		}
		content := strings.TrimSpace(strings.TrimPrefix(line.text, "-"))
		reader.pos++
		key, rest, isKey := splitOpenAPIKey(content)
		switch {
		case content == "":
			node.items = append(node.items, reader.value(indent, ""))
		case isKey && content[0] != '{' && content[0] != '[':
			item := newOpenAPIMapNode()
			item.set(key, reader.value(indent+2, rest))
			if reader.pos < len(reader.lines) && reader.lines[reader.pos].indent == indent+2 && !isOpenAPIListItem(reader.lines[reader.pos].text) {
				more := reader.mapping(indent + 2)
				for _, moreKey := range more.keys {
					item.set(moreKey, more.fields[moreKey])
				}
			}
			node.items = append(node.items, item)
		default:
			node.items = append(node.items, reader.value(indent, content))
		}
	}
	return node
}

// value reads what follows `key:` on a line at the given indent, consuming any
// deeper lines that belong to it.
func (reader *openAPIReader) value(indent int, rest string) *openAPINode {
	switch {
	case rest == "":
		if reader.pos < len(reader.lines) {
			next := reader.lines[reader.pos]
			if next.indent > indent {
				if isOpenAPIListItem(next.text) {
					return reader.list(next.indent)
				}
				return reader.mapping(next.indent)
			}
			if next.indent == indent && isOpenAPIListItem(next.text) {
				return reader.list(indent)
			}
		}
		return &openAPINode{kind: openAPIScalarNode}
	case rest[0] == '|' || rest[0] == '>':
		reader.skipDeeper(indent)
		return &openAPINode{kind: openAPIScalarNode}
	case rest[0] == '{' || rest[0] == '[':
		position := 0
		node := parseOpenAPIFlow(rest, &position)
		reader.skipDeeper(indent)
		return node
	default:
		// A plain scalar may continue on deeper lines; they are its text,
		// never child keys.
		reader.skipDeeper(indent)
		return &openAPINode{kind: openAPIScalarNode, scalar: unquoteOpenAPIScalar(rest)}
	}
}

func (reader *openAPIReader) skipDeeper(indent int) {
	for reader.pos < len(reader.lines) && reader.lines[reader.pos].indent > indent {
		reader.pos++
	}
}

func unquoteOpenAPIScalar(text string) string {
	text = strings.TrimSpace(text)
	if len(text) >= 2 && (text[0] == '\'' || text[0] == '"') && text[len(text)-1] == text[0] {
		return text[1 : len(text)-1]
	}
	return text
}

func parseOpenAPIFlow(text string, position *int) *openAPINode {
	skip := func() {
		for *position < len(text) && text[*position] == ' ' {
			*position++
		}
	}
	skip()
	if *position >= len(text) {
		return &openAPINode{kind: openAPIScalarNode}
	}
	switch text[*position] {
	case '{':
		*position++
		node := newOpenAPIMapNode()
		for {
			skip()
			if *position >= len(text) || text[*position] == '}' {
				*position++
				return node
			}
			colon := strings.IndexByte(text[*position:], ':')
			if colon < 0 {
				*position = len(text)
				return node
			}
			key := unquoteOpenAPIScalar(text[*position : *position+colon])
			*position += colon + 1
			node.set(key, parseOpenAPIFlow(text, position))
			skip()
			if *position < len(text) && text[*position] == ',' {
				*position++
			}
		}
	case '[':
		*position++
		node := &openAPINode{kind: openAPIListNode}
		for {
			skip()
			if *position >= len(text) || text[*position] == ']' {
				*position++
				return node
			}
			node.items = append(node.items, parseOpenAPIFlow(text, position))
			skip()
			if *position < len(text) && text[*position] == ',' {
				*position++
			}
		}
	case '\'', '"':
		quote := text[*position]
		end := strings.IndexByte(text[*position+1:], quote)
		if end < 0 {
			value := text[*position+1:]
			*position = len(text)
			return &openAPINode{kind: openAPIScalarNode, scalar: value}
		}
		value := text[*position+1 : *position+1+end]
		*position += end + 2
		return &openAPINode{kind: openAPIScalarNode, scalar: value}
	default:
		start := *position
		for *position < len(text) && !strings.ContainsRune(",}]", rune(text[*position])) {
			*position++
		}
		return &openAPINode{kind: openAPIScalarNode, scalar: strings.TrimSpace(text[start:*position])}
	}
}

// openAPISchemas resolves `$ref`s against components.schemas and compares
// decoded JSON against a schema node.
type openAPISchemas struct {
	components *openAPINode
}

const openAPISchemaRefPrefix = "#/components/schemas/"

func (schemas openAPISchemas) resolve(node *openAPINode) (*openAPINode, error) {
	for hops := 0; node.get("$ref") != nil; hops++ {
		ref := node.get("$ref").text()
		name, found := strings.CutPrefix(ref, openAPISchemaRefPrefix)
		if !found || hops > 8 {
			return nil, fmt.Errorf("unresolvable $ref %q", ref)
		}
		target := schemas.components.get(name)
		if target == nil {
			return nil, fmt.Errorf("$ref %q names no component schema", ref)
		}
		node = target
	}
	return node, nil
}

func (schemas openAPISchemas) component(t *testing.T, name string) *openAPINode {
	t.Helper()
	node := schemas.components.get(name)
	if node == nil {
		t.Fatalf("components.schemas declares no %s", name)
	}
	return node
}

// objectShape returns the declared properties and required keys of an object
// schema, merging the members of an `allOf`.
func (schemas openAPISchemas) objectShape(node *openAPINode) (map[string]*openAPINode, []string, error) {
	properties := map[string]*openAPINode{}
	var required []string
	parts := []*openAPINode{node}
	if allOf := node.get("allOf"); allOf != nil {
		parts = append(parts, allOf.items...)
	}
	for _, part := range parts {
		resolved, err := schemas.resolve(part)
		if err != nil {
			return nil, nil, err
		}
		if declared := resolved.get("properties"); declared != nil {
			for _, key := range declared.keys {
				properties[key] = declared.fields[key]
			}
		}
		if list := resolved.get("required"); list != nil {
			for _, item := range list.items {
				required = append(required, item.text())
			}
		}
	}
	return properties, required, nil
}

func openAPISchemaTypes(node *openAPINode) []string {
	declared := node.get("type")
	switch {
	case declared == nil:
		return nil
	case declared.kind == openAPIListNode:
		types := make([]string, 0, len(declared.items))
		for _, item := range declared.items {
			types = append(types, item.text())
		}
		return types
	default:
		return []string{declared.text()}
	}
}

func jsonValueKind(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if typed == float64(int64(typed)) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func jsonValueHasType(value any, types []string) bool {
	kind := jsonValueKind(value)
	for _, declared := range types {
		if declared == kind || (declared == "number" && kind == "integer") {
			return true
		}
	}
	return false
}

// violations lists every way value departs from schema. Keys are compared both
// ways; scalars are compared by JSON type only.
func (schemas openAPISchemas) violations(value any, schema *openAPINode, at string, depth int) []string {
	if depth > 16 {
		return []string{at + ": schema nesting deeper than 16; a $ref cycle?"}
	}
	resolved, err := schemas.resolve(schema)
	if err != nil {
		return []string{at + ": " + err.Error()}
	}
	if alternatives := resolved.get("oneOf"); alternatives != nil {
		reasons := make([]string, 0, len(alternatives.items))
		for index, alternative := range alternatives.items {
			found := schemas.violations(value, alternative, at, depth+1)
			if len(found) == 0 {
				return nil
			}
			reasons = append(reasons, fmt.Sprintf("alternative %d: %s", index+1, strings.Join(found, "; ")))
		}
		return []string{at + ": matches none of its oneOf alternatives (" + strings.Join(reasons, " | ") + ")"}
	}
	if types := openAPISchemaTypes(resolved); len(types) > 0 && !jsonValueHasType(value, types) {
		return []string{fmt.Sprintf("%s: %s where the schema declares %v", at, jsonValueKind(value), types)}
	}

	found := openAPIScalarViolations(value, resolved, at)
	switch typed := value.(type) {
	case map[string]any:
		properties, required, err := schemas.objectShape(resolved)
		if err != nil {
			return []string{at + ": " + err.Error()}
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			property, declared := properties[key]
			if !declared {
				found = append(found, fmt.Sprintf("%s: key %q is not among the schema's properties", at, key))
				continue
			}
			found = append(found, schemas.violations(typed[key], property, at+"."+key, depth+1)...)
		}
		for _, key := range required {
			if _, present := typed[key]; !present {
				found = append(found, fmt.Sprintf("%s: required key %q is missing", at, key))
			}
		}
	case []any:
		if items := resolved.get("items"); items != nil {
			for index, element := range typed {
				found = append(found, schemas.violations(element, items, at+"["+strconv.Itoa(index)+"]", depth+1)...)
			}
		}
	}
	return found
}

// openAPIScalarText spells a decoded scalar the way the reader holds an enum
// or const entry.
func openAPIScalarText(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return "null", true
	case bool:
		return strconv.FormatBool(typed), true
	case string:
		return typed, true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	default:
		return "", false
	}
}

// openAPIScalarViolations checks the value keywords the spec puts on scalars:
// enum, const, minimum, maximum and maxLength.
func openAPIScalarViolations(value any, schema *openAPINode, at string) []string {
	text, scalar := openAPIScalarText(value)
	if !scalar {
		return nil
	}
	var found []string
	allowed := schema.get("enum")
	if constant := schema.get("const"); constant != nil {
		allowed = &openAPINode{kind: openAPIListNode, items: []*openAPINode{constant}}
	}
	if allowed != nil {
		if !slices.ContainsFunc(allowed.items, func(entry *openAPINode) bool { return entry.text() == text }) {
			entries := make([]string, 0, len(allowed.items))
			for _, entry := range allowed.items {
				entries = append(entries, strconv.Quote(entry.text()))
			}
			found = append(found, fmt.Sprintf("%s: %q is not among the declared values [%s]", at, text, strings.Join(entries, ", ")))
		}
	}
	if number, isNumber := value.(float64); isNumber {
		for _, bound := range []struct {
			keyword string
			refuses func(limit float64) bool
		}{
			{keyword: "minimum", refuses: func(limit float64) bool { return number < limit }},
			{keyword: "maximum", refuses: func(limit float64) bool { return number > limit }},
		} {
			if declared := schema.get(bound.keyword).text(); declared != "" {
				limit, err := strconv.ParseFloat(declared, 64)
				if err != nil {
					found = append(found, fmt.Sprintf("%s: %s %q is not a number", at, bound.keyword, declared))
				} else if bound.refuses(limit) {
					found = append(found, fmt.Sprintf("%s: %s is outside %s %s", at, text, bound.keyword, declared))
				}
			}
		}
	}
	if str, isString := value.(string); isString {
		if declared := schema.get("maxLength").text(); declared != "" {
			limit, err := strconv.Atoi(declared)
			if err != nil {
				found = append(found, fmt.Sprintf("%s: maxLength %q is not a number", at, declared))
			} else if utf8.RuneCountInString(str) > limit {
				found = append(found, fmt.Sprintf("%s: %d runes exceed maxLength %d", at, utf8.RuneCountInString(str), limit))
			}
		}
	}
	return found
}

// openAPINonJSONSuccessMedia are the media types a 2xx answer may carry
// instead of JSON; any other media type is one the guard cannot classify.
var openAPINonJSONSuccessMedia = map[string]bool{
	"text/html":     true,
	"text/csv":      true,
	"text/calendar": true,
}

// openAPIJSONSuccess is one operation's declared JSON success answer.
type openAPIJSONSuccess struct {
	status int
	schema *openAPINode
}

// openAPIJSONSuccessResponses collects, for every operation under `paths:`,
// the 2xx status whose content is `application/json`, keyed "METHOD /path".
func openAPIJSONSuccessResponses(t *testing.T, document *openAPINode) map[string]openAPIJSONSuccess {
	t.Helper()

	methods := []string{"get", "put", "post", "patch", "delete", "head"}
	operations := map[string]openAPIJSONSuccess{}
	componentResponses := document.get("components").get("responses")
	paths := document.get("paths")
	for _, path := range paths.keys {
		for _, method := range methods {
			responses := paths.fields[path].get(method).get("responses")
			if responses == nil {
				continue
			}
			for _, status := range responses.keys {
				if !strings.HasPrefix(status, "2") {
					continue
				}
				operation := strings.ToUpper(method) + " " + path
				response := responses.fields[status]
				if ref := response.get("$ref"); ref != nil {
					name, found := strings.CutPrefix(ref.text(), "#/components/responses/")
					if !found || componentResponses.get(name) == nil {
						t.Fatalf("%s %s: $ref %q names no component response", operation, status, ref.text())
					}
					response = componentResponses.get(name)
				}
				if response.kind != openAPIMapNode {
					t.Fatalf("%s %s: the response is not a mapping; the collector cannot classify it", operation, status)
				}
				content := response.get("content")
				if content == nil {
					continue // a bodiless answer (204, a redirect) has no schema to compare
				}
				if content.kind != openAPIMapNode || len(content.keys) == 0 {
					t.Fatalf("%s %s: `content` declares no media type; the collector cannot classify it", operation, status)
				}
				schema := content.get("application/json").get("schema")
				if content.get("application/json") == nil {
					for _, media := range content.keys {
						if !openAPINonJSONSuccessMedia[media] {
							t.Fatalf("%s %s: media type %q is neither application/json nor a known non-JSON body; the collector cannot classify it", operation, status, media)
						}
					}
					continue
				}
				if schema == nil {
					t.Fatalf("%s %s: application/json declares no schema", operation, status)
				}
				code, err := strconv.Atoi(status)
				if err != nil {
					t.Fatalf("%s: status key %q is not a number", operation, status)
				}
				if previous, twice := operations[operation]; twice {
					t.Fatalf("%s declares JSON success bodies under both %d and %d; the guard pins one status per operation", operation, previous.status, code)
				}
				operations[operation] = openAPIJSONSuccess{status: code, schema: schema}
			}
		}
	}
	return operations
}

func loadOpenAPISchemaGuardDocument(t *testing.T) (openAPISchemas, map[string]openAPIJSONSuccess) {
	t.Helper()
	document := parseOpenAPIDocument(t, filepath.Join("..", "..", "docs", "openapi.yaml"))
	return openAPISchemas{components: document.get("components").get("schemas")}, openAPIJSONSuccessResponses(t, document)
}

func TestOpenAPIJSONSuccessBodiesMatchTheirDeclaredSchemas(t *testing.T) {
	schemas, declared := loadOpenAPISchemaGuardDocument(t)

	for _, entry := range openAPIResponseSchemaGuardTable() {
		t.Run(entry.operation, func(t *testing.T) {
			t.Parallel()
			success, ok := declared[entry.operation]
			if !ok {
				t.Fatalf("%s declares no JSON success body in docs/openapi.yaml", entry.operation)
			}
			status, body := entry.drive(t)
			if status != success.status {
				t.Fatalf("%s answered %d, the spec declares its JSON success as %d; body: %s", entry.operation, status, success.status, body)
			}
			var decoded any
			if err := json.Unmarshal(body, &decoded); err != nil {
				t.Fatalf("%s answered %d with a body that is not JSON (%v): %q", entry.operation, status, err, body)
			}
			if elements, isArray := decoded.([]any); isArray && len(elements) == 0 {
				t.Fatalf("%s answered an empty array, so no element was compared; the driver must seed data", entry.operation)
			}
			if found := schemas.violations(decoded, success.schema, "body", 0); len(found) > 0 {
				t.Errorf("%s: the %d body departs from the declared schema:\n  %s\nbody: %s",
					entry.operation, status, strings.Join(found, "\n  "), body)
			}
		})
	}
}

func TestOpenAPIResponseSchemaGuardCoversEveryJSONSuccessOperation(t *testing.T) {
	_, declared := loadOpenAPISchemaGuardDocument(t)
	if len(declared) == 0 {
		t.Fatal("no JSON success responses parsed from docs/openapi.yaml; the reader is wrong")
	}

	driven := map[string]bool{}
	for _, entry := range openAPIResponseSchemaGuardTable() {
		if driven[entry.operation] {
			t.Errorf("%s is driven twice in the table", entry.operation)
		}
		driven[entry.operation] = true
	}

	var problems []string
	for operation, reason := range openAPIResponseSchemaGuardExemptions {
		if driven[operation] {
			problems = append(problems, operation+": both driven and exempted")
		}
		if strings.TrimSpace(reason) == "" {
			problems = append(problems, operation+": exempted without a reason")
		}
		if _, ok := declared[operation]; !ok {
			problems = append(problems, operation+": exempted, but the spec declares no JSON success body for it (stale exemption)")
		}
	}
	for operation := range driven {
		if _, ok := declared[operation]; !ok {
			problems = append(problems, operation+": driven, but the spec declares no JSON success body for it (stale entry)")
		}
	}
	for operation := range declared {
		if !driven[operation] {
			if _, exempt := openAPIResponseSchemaGuardExemptions[operation]; !exempt {
				problems = append(problems, operation+": declares a JSON success body that no driver compares and no exemption explains")
			}
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("the response schema guard's coverage drifted from docs/openapi.yaml:\n  %s", strings.Join(problems, "\n  "))
	}
}

// The settings echo that motivated the guard is its load-bearing case: the
// tracking save must be driven, and its body must carry the key the spec
// newly documents, so a driver that stopped sending it cannot pass silently.
func TestOpenAPIResponseSchemaGuardComparesTheTrackingEchoWithWeekStartsOn(t *testing.T) {
	const operation = "PATCH /api/v1/users/current/tracking"
	schemas, declared := loadOpenAPISchemaGuardDocument(t)

	index := slices.IndexFunc(openAPIResponseSchemaGuardTable(), func(entry openAPIResponseSchemaGuardEntry) bool {
		return entry.operation == operation
	})
	if index < 0 {
		t.Fatalf("%s is not in the response schema guard table", operation)
	}
	status, body := openAPIResponseSchemaGuardTable()[index].drive(t)
	if status != declared[operation].status {
		t.Fatalf("%s answered %d, want %d; body: %s", operation, status, declared[operation].status, body)
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode %s body %q: %v", operation, body, err)
	}
	if decoded["week_starts_on"] != "monday" {
		t.Fatalf("%s body carried week_starts_on = %v, want the posted monday; body: %s", operation, decoded["week_starts_on"], body)
	}
	if found := schemas.violations(decoded, declared[operation].schema, "body", 0); len(found) > 0 {
		t.Fatalf("%s body departs from its schema: %v", operation, found)
	}
}

func TestOpenAPIResponseSchemaComparatorReportsKeyEnumAndBoundDrift(t *testing.T) {
	schemas, _ := loadOpenAPISchemaGuardDocument(t)
	schema := &openAPINode{kind: openAPIMapNode, keys: []string{"$ref"}, fields: map[string]*openAPINode{
		"$ref": {kind: openAPIScalarNode, scalar: openAPISchemaRefPrefix + "TimezoneUpdated"},
	}}

	if found := schemas.violations(map[string]any{"ok": true, "changed": false}, schema, "body", 0); len(found) != 0 {
		t.Fatalf("a body carrying exactly the declared keys was refused: %v", found)
	}
	extra := schemas.violations(map[string]any{"ok": true, "changed": false, "timezone": "UTC"}, schema, "body", 0)
	if len(extra) != 1 || !strings.Contains(extra[0], `"timezone"`) {
		t.Fatalf("an undeclared key must be one violation naming it, got %v", extra)
	}
	missing := schemas.violations(map[string]any{"ok": true}, schema, "body", 0)
	if len(missing) != 1 || !strings.Contains(missing[0], `required key "changed"`) {
		t.Fatalf("a missing required key must be one violation naming it, got %v", missing)
	}

	oneOf := schemas.violations(map[string]any{"ok": true, "stray": 1}, parseOpenAPIFlowSchema(
		"{ oneOf: [ { $ref: '#/components/schemas/OkResponse' }, { $ref: '#/components/schemas/OkRedirectResponse' } ] }"), "body", 0)
	if len(oneOf) != 1 || !strings.Contains(oneOf[0], "none of its oneOf alternatives") {
		t.Fatalf("a body no oneOf alternative accepts must be a violation, got %v", oneOf)
	}
	array := schemas.violations([]any{map[string]any{"id": 1.0}}, parseOpenAPIFlowSchema(
		"{ type: array, items: { $ref: '#/components/schemas/Symptom' } }"), "body", 0)
	if len(array) != 1 || !strings.Contains(array[0], `body[0]: required key "name"`) {
		t.Fatalf("an array element missing a required key must be a violation, got %v", array)
	}

	reminders := parseOpenAPIFlowSchema("{ $ref: '#/components/schemas/RemindersUpdated' }")
	if found := schemas.violations(map[string]any{"ok": true, "status": "reminders_updated", "reminder_lead_days": 14.0}, reminders, "body", 0); len(found) != 0 {
		t.Fatalf("a body inside every declared enum and bound was refused: %v", found)
	}
	outOfEnum := schemas.violations(map[string]any{"ok": true, "status": "reminders_saved", "reminder_lead_days": 3.0}, reminders, "body", 0)
	if len(outOfEnum) != 1 || !strings.Contains(outOfEnum[0], `body.status: "reminders_saved" is not among the declared values`) {
		t.Fatalf("a value outside the enum must be one violation naming it, got %v", outOfEnum)
	}
	outOfRange := schemas.violations(map[string]any{"ok": true, "status": "reminders_updated", "reminder_lead_days": 15.0}, reminders, "body", 0)
	if len(outOfRange) != 1 || !strings.Contains(outOfRange[0], "body.reminder_lead_days: 15 is outside maximum 14") {
		t.Fatalf("an integer past the maximum must be one violation naming it, got %v", outOfRange)
	}
}

func parseOpenAPIFlowSchema(text string) *openAPINode {
	position := 0
	return parseOpenAPIFlow(text, &position)
}

// The body guard sees only the keys one request happened to produce: an
// omitempty field left empty by the driver's seed data never reaches it. The
// DTOs behind the day, symptom and export bodies are therefore also pinned by
// reflection — every wire name is a declared property and every declared
// property is a wire name, and a key the spec calls `required` is one the DTO
// always writes (an omitempty field may be absent, so it cannot be required).
func TestOpenAPIResponseSchemasEnumerateExactlyTheirDTOsWireFields(t *testing.T) {
	schemas, _ := loadOpenAPISchemaGuardDocument(t)

	for _, subject := range []struct {
		schema string
		dto    any
	}{
		{schema: "DailyLog", dto: dayResponse{}},
		{schema: "Symptom", dto: symptomResponse{}},
		{schema: "ExportJSONEntry", dto: services.ExportJSONEntry{}},
		{schema: "ExportSymptomFlags", dto: services.ExportSymptomFlags{}},
	} {
		t.Run(subject.schema, func(t *testing.T) {
			properties, required, err := schemas.objectShape(schemas.component(t, subject.schema))
			if err != nil {
				t.Fatalf("%s: %v", subject.schema, err)
			}
			if len(properties) == 0 {
				t.Fatalf("%s declares no properties — this check is about a schema nobody read", subject.schema)
			}
			declared := make([]string, 0, len(properties))
			for name := range properties {
				declared = append(declared, name)
			}
			sort.Strings(declared)

			wire, alwaysPresent := jsonWireShape(t, subject.dto)
			if !reflect.DeepEqual(declared, wire) {
				t.Fatalf("%s properties drifted from the DTO\n spec: %v\n  dto: %v", subject.schema, declared, wire)
			}
			for _, name := range required {
				if !slices.Contains(alwaysPresent, name) {
					t.Errorf("%s requires %q, but the DTO omits it when empty (or does not write it at all)", subject.schema, name)
				}
			}
		})
	}
}

// jsonWireShape reads a DTO's wire names and the subset that is always present
// (every field whose tag lacks omitempty).
func jsonWireShape(t *testing.T, dto any) (fields []string, alwaysPresent []string) {
	t.Helper()

	structType := reflect.TypeOf(dto)
	for index := range structType.NumField() {
		field := structType.Field(index)
		tag, ok := field.Tag.Lookup("json")
		if !ok {
			t.Fatalf("%s.%s carries no json tag, so it has no documented wire name", structType.Name(), field.Name)
		}
		name, options, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		fields = append(fields, name)
		if !slices.Contains(strings.Split(options, ","), "omitempty") && !slices.Contains(strings.Split(options, ","), "omitzero") {
			alwaysPresent = append(alwaysPresent, name)
		}
	}
	sort.Strings(fields)
	sort.Strings(alwaysPresent)
	return fields, alwaysPresent
}

package api

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// The spec's field list is pinned against the struct that produces it.
//
// StatsOverview stopped being `additionalProperties: true` and now enumerates
// every field, which is the point of the change: a field added to the domain
// statistics struct must not reach the API on its own. But an enumeration is a
// SECOND spelling of the DTO, and the spec is descriptive — nothing in the route
// or bounds sweeps reads a property list, so a field added to the DTO alone
// leaves every test green while a client validating against the published schema
// rejects the response for a property the schema forbids. The reverse drift, a
// field removed from the DTO and still `required` in the spec, fails the same
// validators from the other side.
//
// Parsed line by line rather than with a YAML library, matching
// openapi_contract_test.go's dependency-free posture on the same document.

type statsOverviewSpecSchema struct {
	properties           []string
	required             []string
	additionalProperties string
}

func TestStatsOverviewSpecEnumeratesExactlyTheDTOsFields(t *testing.T) {
	specPath := filepath.Join("..", "..", "docs", "openapi.yaml")

	for _, subject := range []struct {
		schema string
		dto    any
	}{
		{schema: "StatsOverview", dto: StatsOverviewResponse{}},
		{schema: "StatsOverviewSuppression", dto: StatsOverviewSuppression{}},
	} {
		t.Run(subject.schema, func(t *testing.T) {
			spec := readStatsOverviewSpecSchema(t, specPath, subject.schema)
			wire := jsonWireFields(t, subject.dto)

			if len(spec.properties) == 0 {
				t.Fatalf("%s declares no properties — this check is about a schema nobody read", subject.schema)
			}
			if spec.additionalProperties != "false" {
				t.Fatalf("%s declares additionalProperties: %q, want false — an open schema documents nothing about the field set", subject.schema, spec.additionalProperties)
			}
			if !reflect.DeepEqual(spec.properties, wire) {
				t.Fatalf("%s properties drifted from the DTO\n spec: %v\n  dto: %v", subject.schema, spec.properties, wire)
			}
			// Every field is always present on the wire — a suppressed date is
			// null, never omitted — so `required` and the field set are the same
			// list, and a field that stops being required is a shape change.
			if !reflect.DeepEqual(spec.required, wire) {
				t.Fatalf("%s required drifted from the DTO\n spec: %v\n  dto: %v", subject.schema, spec.required, wire)
			}
		})
	}
}

// readStatsOverviewSpecSchema reads one schema block out of components.schemas.
// Keys are recognised by their indentation, so a nested object (the `items:` of
// an array, a `$ref` target) cannot be mistaken for a property of the schema
// itself.
func readStatsOverviewSpecSchema(t *testing.T, specPath string, schema string) statsOverviewSpecSchema {
	t.Helper()

	source, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read %s: %v", specPath, err)
	}

	const (
		schemaIndent   = "    "
		keyIndent      = "      "
		propertyIndent = "        "
		listIndent     = "        - "
	)

	parsed := statsOverviewSpecSchema{}
	inSchema := false
	section := ""

	for _, line := range strings.Split(string(source), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, schemaIndent+schema+":") {
			inSchema = true
			continue
		}
		if !inSchema {
			continue
		}
		// The next schema at the same indent ends this one.
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, keyIndent) {
			break
		}
		if strings.HasPrefix(line, keyIndent) && !strings.HasPrefix(line, keyIndent+" ") {
			key := strings.TrimSpace(line)
			switch {
			case key == "properties:":
				section = "properties"
			case strings.HasPrefix(key, "required:"):
				// The spec writes short lists inline (`required: [a, b]`) and long
				// ones as a block. Both are the same list, and a reader that knew
				// only one of them would report an empty one for the other.
				section = "required"
				if inline := strings.TrimSpace(strings.TrimPrefix(key, "required:")); inline != "" {
					parsed.required = append(parsed.required, splitInlineYAMLList(inline)...)
					section = ""
				}
			case strings.HasPrefix(key, "additionalProperties:"):
				parsed.additionalProperties = strings.TrimSpace(strings.TrimPrefix(key, "additionalProperties:"))
				section = ""
			default:
				section = ""
			}
			continue
		}
		switch section {
		case "properties":
			if strings.HasPrefix(line, propertyIndent) && !strings.HasPrefix(line, propertyIndent+" ") && !strings.HasPrefix(line, listIndent) {
				name, _, found := strings.Cut(strings.TrimSpace(line), ":")
				if found {
					parsed.properties = append(parsed.properties, name)
				}
			}
		case "required":
			if strings.HasPrefix(line, listIndent) {
				parsed.required = append(parsed.required, strings.TrimSpace(strings.TrimPrefix(line, listIndent)))
			}
		}
	}
	if !inSchema {
		t.Fatalf("%s is not declared in %s", schema, specPath)
	}

	sort.Strings(parsed.properties)
	sort.Strings(parsed.required)
	return parsed
}

func splitInlineYAMLList(inline string) []string {
	inline = strings.TrimSuffix(strings.TrimPrefix(inline, "["), "]")
	entries := make([]string, 0, 4)
	for _, entry := range strings.Split(inline, ",") {
		if entry = strings.TrimSpace(entry); entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

func jsonWireFields(t *testing.T, dto any) []string {
	t.Helper()

	structType := reflect.TypeOf(dto)
	names := make([]string, 0, structType.NumField())
	for index := range structType.NumField() {
		tag, ok := structType.Field(index).Tag.Lookup("json")
		if !ok {
			t.Fatalf("%s.%s carries no json tag, so it has no documented wire name", structType.Name(), structType.Field(index).Name)
		}
		name, _, _ := strings.Cut(tag, ",")
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

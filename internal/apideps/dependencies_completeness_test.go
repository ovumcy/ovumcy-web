package apideps

import (
	"reflect"
	"testing"
)

// A dependency lives in three places — the Dependencies field, the
// requirements() row that makes its absence fail fast, and the bootstrap
// assignment that wires it — and only the middle one is a check. Nothing bound
// the three together: TestDependenciesValidateReportsMissing passes on the zero
// value even if requirements() returned a single row, because Validate reports
// the FIRST missing requirement and then returns. A field added to the struct
// and forgotten in requirements() therefore loses its fail-fast silently; if it
// is later also dropped from the wiring, the binary boots green and nil-panics
// on the first request that reaches that service.
//
// The sweep below binds rows to fields behaviourally, in both directions, so
// requirements() needs no tags or naming convention to be checkable:
//
//   - field -> row: every dependency field, nilled alone inside an otherwise
//     fully wired Dependencies, must make Validate report an error. Only a row
//     that reads THAT field can produce one.
//   - row -> field: the errors those fields produce are all distinct, and
//     requirements() lists exactly as many rows as there are dependency fields,
//     so rows and fields are in bijection. A row answering for two fields shows
//     up as a repeated message, a duplicated or otherwise stale row as a count
//     mismatch, and an empty sweep as a count mismatch too.

// exemptDependencyFields names the Dependencies fields Validate deliberately
// does not check, each with the reason it cannot be a requirement. A field that
// is not named here has to be validated: the sweep fails on an unknown field
// rather than skipping it, which makes this list a decision record rather than
// a way to silence the guard.
var exemptDependencyFields = map[string]string{
	"AuditLogEnabled": "runtime config (AUDIT_LOG_ENABLED), not a service: false is the default and a legitimate value, so no state of it means missing",
}

// The interface-typed ports cannot be allocated by reflection the way a
// *services.X field can, so the sweep needs one concrete stand-in each. A
// struct embedding the port satisfies it without restating its methods; the
// sweep only ever needs the field to be non-nil and never calls into it.
type stubRegistrationWorkflowService struct{ RegistrationWorkflowService }

type stubLoginWorkflowService struct{ LoginWorkflowService }

type stubOIDCWorkflowService struct{ OIDCWorkflowService }

type stubRegisterPickupTokenStore struct{ RegisterPickupTokenStore }

// interfacePortStubs maps each interface-typed dependency to its stand-in. It
// is a lookup, not an escape hatch: a port added without an entry fails the
// sweep instead of dropping out of it.
func interfacePortStubs() map[reflect.Type]reflect.Value {
	return map[reflect.Type]reflect.Value{
		reflect.TypeFor[RegistrationWorkflowService](): reflect.ValueOf(stubRegistrationWorkflowService{}),
		reflect.TypeFor[LoginWorkflowService]():        reflect.ValueOf(stubLoginWorkflowService{}),
		reflect.TypeFor[OIDCWorkflowService]():         reflect.ValueOf(stubOIDCWorkflowService{}),
		reflect.TypeFor[RegisterPickupTokenStore]():    reflect.ValueOf(stubRegisterPickupTokenStore{}),
	}
}

// dependencyFiller returns a non-nil stand-in for a dependency field of the
// given type, or the reason it cannot build one. Pointer fields are allocated
// outright, so a new *services.X dependency needs no entry anywhere. Refusing
// is a failure of the sweep, never a skip — a field the sweep cannot wire is
// exactly the field whose validation nobody would notice missing.
func dependencyFiller(fieldType reflect.Type) (reflect.Value, string) {
	switch fieldType.Kind() {
	case reflect.Pointer:
		return reflect.New(fieldType.Elem()), ""
	case reflect.Interface:
		stub, known := interfacePortStubs()[fieldType]
		if !known {
			return reflect.Value{}, "it is an interface port with no stand-in; add one to interfacePortStubs so the field stays swept"
		}
		return stub, ""
	default:
		return reflect.Value{}, "Validate detects a missing dependency by its nil, and a value of this kind is never nil; validate it another way or record it in exemptDependencyFields with the reason"
	}
}

// fullyWiredDependencies builds a Dependencies with every non-exempt field set
// to a non-nil stand-in — the state a correct bootstrap hands the handler.
func fullyWiredDependencies(t *testing.T) Dependencies {
	t.Helper()

	dependencies := Dependencies{}
	value := reflect.ValueOf(&dependencies).Elem()
	for index := range value.NumField() {
		field := value.Type().Field(index)
		if _, exempt := exemptDependencyFields[field.Name]; exempt {
			continue
		}
		filler, refusal := dependencyFiller(field.Type)
		if refusal != "" {
			t.Fatalf("Dependencies.%s (%s) cannot be wired by this guard: %s", field.Name, field.Type, refusal)
		}
		value.Field(index).Set(filler)
	}
	return dependencies
}

// TestEveryDependencyFieldHasItsOwnRequirement is the completeness guard: one
// requirements() row per dependency field, each reading its own field and
// reporting its own message.
func TestEveryDependencyFieldHasItsOwnRequirement(t *testing.T) {
	wired := fullyWiredDependencies(t)
	if err := wired.Validate(); err != nil {
		t.Fatalf("a fully wired Dependencies must validate, got %v; every case below reads the error left after nilling one field, so it means nothing unless the wired baseline is clean", err)
	}

	value := reflect.ValueOf(&wired).Elem()
	reporter := map[string]string{}
	dependencyFields := 0
	for index := range value.NumField() {
		field := value.Type().Field(index)
		if _, exempt := exemptDependencyFields[field.Name]; exempt {
			continue
		}
		dependencyFields++

		probe := wired
		reflect.ValueOf(&probe).Elem().Field(index).Set(reflect.Zero(field.Type))

		err := probe.Validate()
		if err == nil {
			t.Errorf("Dependencies.%s is nil and Validate accepted it: requirements() has no row reading that field, so a build that forgets to wire it starts anyway and nil-panics on the first request reaching it", field.Name)
			continue
		}
		if owner, repeated := reporter[err.Error()]; repeated {
			t.Errorf("Dependencies.%s and Dependencies.%s both report %q: one requirements() row is answering for two fields, so the startup error cannot name the culprit", field.Name, owner, err.Error())
			continue
		}
		reporter[err.Error()] = field.Name
	}

	if rows := len(wired.requirements()); rows != dependencyFields {
		t.Errorf("requirements() lists %d row(s) against %d dependency field(s): with every field proven to have a row of its own, a surplus row is a stale or duplicated one and a shortfall means the sweep saw no fields at all", rows, dependencyFields)
	}
}

// TestDependencyExemptionsNameLiveFields keeps the exemption list from
// outliving what it exempts: a renamed or removed field would leave an entry
// that silently exempts nothing, and the next field to take that name would
// inherit the exemption without anyone deciding it should.
func TestDependencyExemptionsNameLiveFields(t *testing.T) {
	dependencyType := reflect.TypeOf(Dependencies{})
	for name, reason := range exemptDependencyFields {
		if _, live := dependencyType.FieldByName(name); !live {
			t.Errorf("exemptDependencyFields names %q, which Dependencies no longer declares; drop the entry rather than leaving it to exempt a future field of that name", name)
		}
		if reason == "" {
			t.Errorf("exemptDependencyFields exempts %q with no reason", name)
		}
	}
}

// TestDependencyFillerRefusesAFieldItCannotWire anchors the sweep on fixtures
// it owns, in both directions: the two shapes a dependency can take must be
// wired to a non-nil value, and the two shapes the guard cannot honestly wire
// must be refused rather than passed over. Without this, a filler that returned
// "no problem" for everything would leave the sweep nilling fields that were
// never wired in the first place.
func TestDependencyFillerRefusesAFieldItCannotWire(t *testing.T) {
	cases := []struct {
		name        string
		fieldType   reflect.Type
		wantRefusal bool
	}{
		{"service pointer", reflect.TypeFor[*Dependencies](), false},
		{"known interface port", reflect.TypeFor[RegisterPickupTokenStore](), false},
		{"unknown interface port", reflect.TypeFor[interface{ Unwired() }](), true},
		{"non-nilable field", reflect.TypeFor[string](), true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			filler, refusal := dependencyFiller(testCase.fieldType)
			if testCase.wantRefusal {
				if refusal == "" {
					t.Fatalf("dependencyFiller(%s) wired the field instead of refusing it", testCase.fieldType)
				}
				return
			}
			if refusal != "" {
				t.Fatalf("dependencyFiller(%s) refused a field it can wire: %s", testCase.fieldType, refusal)
			}
			if (dependencyRequirement{value: filler.Interface()}).missing() {
				t.Fatalf("dependencyFiller(%s) returned a value Validate still reads as missing", testCase.fieldType)
			}
		})
	}
}

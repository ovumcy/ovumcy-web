package api

import (
	"go/constant"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The JSON overview publishes the reasons a projection was withheld as a closed
// enum, and a client branches on those strings. A reason the service can emit but
// the spec never lists is a value the client was promised could not exist; a
// listed value no constant produces is a branch that never fires. The check runs
// both ways, resolves the constants through the type checker (a hand-written
// list here would be the shape that hides the next reason), and is anchored to
// the compiler so a scan that silently stops matching cannot pass as an empty set.
func TestOpenAPISuppressionReasonEnumMatchesTheServiceConstants(t *testing.T) {
	declared := declaredSuppressionReasons(t)
	published := openAPIPublishedSuppressionReasons(t, filepath.Join("..", "..", "docs", "openapi.yaml"))

	for _, anchor := range []services.SuppressionReason{
		services.SuppressionReasonUnpredictableCycle,
		services.SuppressionReasonAwaitingMoreCycles,
	} {
		if _, ok := declared[string(anchor)]; !ok {
			t.Fatalf("reason scan did not find %q among the SuppressionReason constants; the scan, not the spec, is broken", anchor)
		}
		if _, ok := published[string(anchor)]; !ok {
			t.Fatalf("enum scan did not find %q in docs/openapi.yaml StatsOverviewSuppression.reasons", anchor)
		}
	}

	if missing := difference(declared, published); len(missing) > 0 {
		t.Errorf("reasons the service can emit but docs/openapi.yaml does not list in StatsOverviewSuppression.reasons:\n  %s",
			strings.Join(missing, "\n  "))
	}
	if extra := difference(published, declared); len(extra) > 0 {
		t.Errorf("reasons listed in docs/openapi.yaml but declared by no SuppressionReason constant:\n  %s",
			strings.Join(extra, "\n  "))
	}
}

// declaredSuppressionReasons type-checks the services package and takes the value
// of every package-level constant whose declared type is SuppressionReason, found
// through the type checker rather than by the shape of the declaration text: a
// constant spelled in a group, through an expression, or in another file of the
// package is still one.
func declaredSuppressionReasons(t *testing.T) map[string]struct{} {
	t.Helper()
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  filepath.Join("..", ".."),
	}, "./internal/services")
	if err != nil || len(loaded) != 1 || len(loaded[0].Errors) > 0 {
		t.Fatalf("type-check internal/services: %v (%d packages)", err, len(loaded))
	}
	scope := loaded[0].Types.Scope()
	reasons := make(map[string]struct{})
	for _, name := range scope.Names() {
		object, ok := scope.Lookup(name).(*types.Const)
		if !ok {
			continue
		}
		named, ok := object.Type().(*types.Named)
		if !ok || named.Obj().Name() != "SuppressionReason" || named.Obj().Pkg() != loaded[0].Types {
			continue
		}
		reasons[constant.StringVal(object.Val())] = struct{}{}
	}
	return reasons
}

// openAPIPublishedSuppressionReasons reads the enum under
// StatsOverviewSuppression.reasons.items. The scan tracks the schema nesting
// (schema 4, property 8, items 10, enum 12, values 14) rather than taking the
// first "enum:" it meets, and stops at the first line that leaves the list.
func openAPIPublishedSuppressionReasons(t *testing.T, specPath string) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}

	inSchema, inReasons, inEnum := false, false, false
	values := make(map[string]struct{})
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))

		switch {
		case indent == 4 && strings.HasSuffix(text, ":"):
			inSchema = text == "StatsOverviewSuppression:"
			inReasons, inEnum = false, false
		case indent == 8 && inSchema && strings.HasSuffix(text, ":"):
			inReasons = text == "reasons:"
			inEnum = false
		case indent == 12 && inReasons:
			inEnum = text == "enum:"
		case indent == 14 && inEnum && strings.HasPrefix(text, "- "):
			values[strings.Trim(strings.TrimSpace(strings.TrimPrefix(text, "- ")), `"'`)] = struct{}{}
		}
	}
	if len(values) == 0 {
		t.Fatal("no enum found for StatsOverviewSuppression.reasons in docs/openapi.yaml; parser or spec is wrong")
	}
	return values
}

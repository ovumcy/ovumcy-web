package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// TestEveryDaySaveMessageKeyHasADeclaredKind enumerates the day-save message
// keys from the policy source itself, so a key added later without a decided
// kind fails here instead of silently defaulting to a routine, self-clearing
// line. The safety sentence (the pregnancy pause with its red-flag guidance)
// must stay up until dismissed; the bare confirmation is not repeated on a
// surface that has its own save indicator.
func TestEveryDaySaveMessageKeyHasADeclaredKind(t *testing.T) {
	t.Parallel()

	want := map[string]DaySaveMessageKind{
		"dashboard.save_message_self_care":        DaySaveMessageRoutine,
		"dashboard.save_message_fertile":          DaySaveMessageRoutine,
		"dashboard.save_message_neutral":          DaySaveMessageConfirmation,
		"dashboard.save_message_pregnancy_paused": DaySaveMessageSafety,
	}

	keys := daySaveMessageKeysFromSource(t)
	if keys["daySaveMessagePregnancyPaused"] != daySaveMessagePregnancyPaused {
		t.Fatalf("the source sweep must find the pregnancy-pause key by name, got %v", keys)
	}
	for name, key := range keys {
		kind, decided := want[key]
		if !decided {
			t.Errorf("%s (%q) has no decided kind: state whether it is routine, a bare confirmation, or safety guidance", name, key)
			continue
		}
		if got := ClassifyDaySaveMessage(key); got != kind {
			t.Errorf("ClassifyDaySaveMessage(%q) = %d, want %d", key, got, kind)
		}
	}

	// No resolved sentence: the caller answers with the bare timestamped
	// confirmation, which is a confirmation like the neutral line.
	if got := ClassifyDaySaveMessage(""); got != DaySaveMessageConfirmation {
		t.Errorf("ClassifyDaySaveMessage(\"\") = %d, want the confirmation kind", got)
	}
}

func daySaveMessageKeysFromSource(t *testing.T) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "day_feedback_policy.go", nil, 0)
	if err != nil {
		t.Fatalf("parse day_feedback_policy.go: %v", err)
	}
	keys := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if !strings.HasPrefix(name.Name, "daySaveMessage") || index >= len(value.Values) {
					continue
				}
				literal, ok := value.Values[index].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Fatalf("%s is not a string literal constant", name.Name)
				}
				key, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", name.Name, err)
				}
				keys[name.Name] = key
			}
		}
	}
	return keys
}

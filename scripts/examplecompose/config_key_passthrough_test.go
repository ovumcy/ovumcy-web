package examplecompose

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The example stacks carry an explicit `environment:` allowlist and no
// `env_file`, so a runtime key the binary reads that the stack does not forward
// is silently pinned to its in-code default. For REGISTRATION_MODE that default
// is `open`: a public HTTPS instance built from the documented proxy stack ran
// with self-service registration, and every remedy the operator docs name
// (editing .env, setting the variable) changed nothing.
//
// The set of keys is read from the binary's own sources, not listed here, so a
// key added tomorrow is judged by the next run: a stack either forwards it or
// the exemption table below says why it does not.

// envKeyShape is what a configuration variable name looks like. It is applied
// to a whole string literal, so a message or a fallback value is never a key.
var envKeyShape = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)

// keyReaders maps a reader-function name prefix in cmd/ovumcy to how many of
// its leading arguments name an environment variable. The remaining arguments
// are fallbacks and limits, which are never keys.
var keyReaders = []struct {
	prefix string
	keyArg int
}{
	{"getEnv", 1},
	{"getRateLimit", 1},
	{"getCredentialRateLimit", 2},
	{"resolveSecretFromEnvOrFile", 2},
}

// readerKeyArgs reports how many leading arguments of call name environment
// variables, resolving os.Getenv/os.LookupEnv and the in-package helpers.
func readerKeyArgs(call *ast.CallExpr) int {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		for _, reader := range keyReaders {
			if strings.HasPrefix(fn.Name, reader.prefix) {
				return reader.keyArg
			}
		}
	case *ast.SelectorExpr:
		if pkg, ok := fn.X.(*ast.Ident); ok && pkg.Name == "os" && (fn.Sel.Name == "Getenv" || fn.Sel.Name == "LookupEnv") {
			return 1
		}
	}
	return 0
}

// collectReadKeys returns every environment variable the given sources read,
// and the key-shaped literals of the files named in strictFiles that no reader
// call accounts for. A key passed as a package constant (security.X) resolves
// through consts, keyed by the constant's name.
func collectReadKeys(sources map[string]string, consts map[string]string, strictFiles map[string]bool) (keys map[string]bool, unread []string, err error) {
	keys = map[string]bool{}
	fset := token.NewFileSet()
	for _, name := range sortedKeys(sources) {
		file, parseErr := parser.ParseFile(fset, name, sources[name], parser.SkipObjectResolution)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		viaReader := map[*ast.BasicLit]bool{}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			limit := readerKeyArgs(call)
			for i, arg := range call.Args {
				if i >= limit {
					break
				}
				switch value := arg.(type) {
				case *ast.BasicLit:
					if key, ok := unquoteKey(value); ok {
						keys[key] = true
						viaReader[value] = true
					}
				case *ast.SelectorExpr:
					if key, ok := consts[value.Sel.Name]; ok && envKeyShape.MatchString(key) {
						keys[key] = true
					}
				}
			}
			return true
		})
		if !strictFiles[name] {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || viaReader[lit] {
				return true
			}
			if key, ok := unquoteKey(lit); ok {
				unread = append(unread, fmt.Sprintf("%s %s", fset.Position(lit.Pos()).String(), key))
			}
			return true
		})
	}
	return keys, unread, nil
}

func unquoteKey(lit *ast.BasicLit) (string, bool) {
	if lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil || len(value) < 2 || !envKeyShape.MatchString(value) {
		return "", false
	}
	return value, true
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// loadBinarySources reads the non-test Go files of cmd/ovumcy plus the string
// constants of internal/security (where the fence path variable is named).
func loadBinarySources(t *testing.T, root string) (sources map[string]string, consts map[string]string) {
	t.Helper()
	sources = map[string]string{}
	dir := filepath.Join(root, "cmd", "ovumcy")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		sources[name] = string(content)
	}

	consts = map[string]string{}
	securityDir := filepath.Join(root, "internal", "security")
	securityEntries, err := os.ReadDir(securityDir)
	if err != nil {
		t.Fatalf("read %s: %v", securityDir, err)
	}
	fset := token.NewFileSet()
	for _, entry := range securityEntries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(securityDir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				for i, ident := range valueSpec.Names {
					if i >= len(valueSpec.Values) {
						continue
					}
					if lit, ok := valueSpec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if value, err := strconv.Unquote(lit.Value); err == nil {
							consts[ident.Name] = value
						}
					}
				}
			}
		}
	}
	return sources, consts
}

// stack is one shipped compose file's ovumcy service.
type stack struct {
	path string // slash-separated, relative to the repository root
	// env maps each key the service's environment block sets to its raw value.
	env map[string]string
}

var (
	serviceHeader   = regexp.MustCompile(`(?m)^  [A-Za-z0-9_-]+:\s*$`)
	environmentKey  = regexp.MustCompile(`^\s{6}(?:-\s*)?([A-Z][A-Z0-9_]*)\s*[:=]\s*(.*?)\s*$`)
	environmentHead = regexp.MustCompile(`^\s{4}environment:\s*$`)
)

// parseOvumcyEnvironment returns the keys set in the environment block of the
// service that runs the ovumcy image, or ok=false when the file has no such
// service. Both the map form (`KEY: value`) and the list form (`- KEY=value`)
// are read; a commented-out line is not a key.
func parseOvumcyEnvironment(content string) (env map[string]string, ok bool) {
	headers := serviceHeader.FindAllStringIndex(content, -1)
	for i, header := range headers {
		end := len(content)
		if i+1 < len(headers) {
			end = headers[i+1][0]
		}
		body := content[header[1]:end]
		if !ovumcyImage.MatchString(body) {
			continue
		}
		env = map[string]string{}
		inBlock := false
		for _, line := range strings.Split(body, "\n") {
			line = strings.TrimRight(line, "\r")
			switch {
			case environmentHead.MatchString(line):
				inBlock = true
			case !inBlock:
			case strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#"):
			case environmentKey.MatchString(line):
				match := environmentKey.FindStringSubmatch(line)
				env[match[1]] = match[2]
			default:
				inBlock = false
			}
		}
		return env, true
	}
	return nil, false
}

// exemption records a key a stack may legitimately not forward. applies decides
// which stacks it covers; a nil applies means every stack.
type exemption struct {
	reason  string
	applies func(stack) bool
}

// composesDatabaseURL is true for a stack that builds DATABASE_URL itself from
// its postgres service's credentials rather than passing the operator's value
// through.
func composesDatabaseURL(s stack) bool {
	value, set := s.env["DATABASE_URL"]
	return set && !strings.HasPrefix(value, "${DATABASE_URL")
}

// fixesPostgresDriver is true for a stack that pins DB_DRIVER to postgres
// rather than leaving the choice to the operator.
func fixesPostgresDriver(s stack) bool {
	return s.env["DB_DRIVER"] == "postgres"
}

// forwardingExemptions is the whole list of keys an example stack may leave
// out. A new key is not added here to make the test pass: it is added only with
// a reason an operator reading the stack would accept.
var forwardingExemptions = map[string]exemption{
	"PORT": {
		reason: "the example stacks address the app on its default port 8080 (the proxy configs name ovumcy:8080, the local postgres stack publishes 8080:8080), so a PORT override would break the stack instead of tuning it",
	},
	"DB_PATH": {
		reason:  "the stack fixes DB_DRIVER to postgres, which never opens the SQLite file, so the path would be read by nothing",
		applies: fixesPostgresDriver,
	},
	"DATABASE_URL_FILE": {
		reason:  "the stack composes DATABASE_URL from its own postgres service credentials, which takes precedence over a file, so forwarding a file path would be silently ignored",
		applies: composesDatabaseURL,
	},
}

func loadExampleStacks(t *testing.T, root string) []stack {
	t.Helper()
	var stacks []stack
	err := filepath.WalkDir(filepath.Join(root, "docs", "examples"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "docker-compose.yml" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		env, ok := parseOvumcyEnvironment(string(content))
		if !ok {
			return nil
		}
		stacks = append(stacks, stack{path: filepath.ToSlash(rel), env: env})
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/examples: %v", err)
	}
	return stacks
}

// TestEveryExampleStackForwardsEveryRuntimeConfigKey asserts that each key the
// binary reads reaches the app in every shipped example stack, or is exempted
// above with a stated reason.
func TestEveryExampleStackForwardsEveryRuntimeConfigKey(t *testing.T) {
	root := repoRoot(t)
	sources, consts := loadBinarySources(t, root)
	keys, unread, err := collectReadKeys(sources, consts, map[string]bool{"config.go": true})
	if err != nil {
		t.Fatalf("scan cmd/ovumcy: %v", err)
	}
	for _, literal := range unread {
		t.Errorf("config.go holds a key-shaped literal no known reader accounts for (%s): read it through one of the env helpers so the example stacks are held to it, or extend keyReaders", literal)
	}

	// The scan has to reach the keys the failure that motivated it dropped, and
	// each way a key is spelled: a direct literal, the second name of a pair
	// reader, a *_FILE twin and a constant from another package.
	for _, want := range []string{"REGISTRATION_MODE", "HSTS_ENABLED", "RATE_LIMIT_PASSWORD_RESET_REDEEM_WINDOW", "SECRET_KEY_FILE", "CALENDAR_FEED_FENCE_PATH", "TZ"} {
		if !keys[want] {
			t.Fatalf("the source scan did not find %s: it is not reaching the keys the binary reads (found %d)", want, len(keys))
		}
	}

	stacks := loadExampleStacks(t, root)
	have := map[string]bool{}
	for _, s := range stacks {
		have[s.path] = true
		if len(s.env) == 0 {
			t.Errorf("%s: no environment block was read for the ovumcy service", s.path)
		}
	}
	for _, want := range []string{
		"docs/examples/postgres/docker-compose.yml",
		"docs/examples/reverse-proxy/caddy/docker-compose.yml",
	} {
		if !have[want] {
			t.Fatalf("the stack scan did not find %s: it is not reaching the shipped stacks", want)
		}
	}

	exempted := map[string]bool{}
	for _, key := range sortedKeys(keys) {
		for _, s := range stacks {
			if _, forwarded := s.env[key]; forwarded {
				continue
			}
			if rule, ok := forwardingExemptions[key]; ok && (rule.applies == nil || rule.applies(s)) {
				exempted[key] = true
				continue
			}
			t.Errorf("%s: does not forward %s, so the app ignores the operator's value and runs on the in-code default; add `%s: ${%s:-}` to its environment block, or exempt the key in forwardingExemptions with the reason the stack must not carry it", s.path, key, key, key)
		}
	}

	// An exemption that no stack needs any more, or for a key the binary no
	// longer reads, hides the day the exempted key becomes live.
	for _, key := range sortedKeys(forwardingExemptions) {
		rule := forwardingExemptions[key]
		if strings.TrimSpace(rule.reason) == "" {
			t.Errorf("exemption for %s has no reason", key)
		}
		if !keys[key] {
			t.Errorf("exemption for %s names a key the binary no longer reads", key)
		}
		if !exempted[key] {
			t.Errorf("exemption for %s is not needed by any stack: every stack forwards it, drop the entry", key)
		}
	}
}

// TestConfigKeyScanSeesEachWayAKeyIsRead proves the source scan on fixtures it
// owns: each reader spelling yields its keys, a fallback or limit argument is
// never a key, and a key-shaped literal outside a reader is reported rather
// than skipped.
func TestConfigKeyScanSeesEachWayAKeyIsRead(t *testing.T) {
	sources := map[string]string{
		"config.go": `package main
func load() {
	a := getEnv("ALPHA_KEY", "FALLBACK")
	b := getRateLimitMax("BETA_MAX", 5, 100)
	c, d := getCredentialRateLimit("GAMMA_MAX", "GAMMA_WINDOW", 8, 0)
	e, _ := resolveSecretFromEnvOrFile("DELTA", "DELTA_FILE", 8)
	f := os.Getenv(security.FencePathEnv)
	g := getEnvBoolStrict("EPSILON_ENABLED", false)
	h := notAReader("ZETA_KEY")
}`,
	}
	keys, unread, err := collectReadKeys(sources, map[string]string{"FencePathEnv": "FENCE_PATH"}, map[string]bool{"config.go": true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ALPHA_KEY", "BETA_MAX", "DELTA", "DELTA_FILE", "EPSILON_ENABLED", "FENCE_PATH", "GAMMA_MAX", "GAMMA_WINDOW"}
	if got := sortedKeys(keys); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keys read = %v, want %v", got, want)
	}
	if len(unread) != 2 || !strings.Contains(unread[0]+unread[1], "FALLBACK") || !strings.Contains(unread[0]+unread[1], "ZETA_KEY") {
		t.Fatalf("a fallback-shaped literal and a literal passed to an unknown helper must both be reported, got %v", unread)
	}
}

// TestEnvironmentBlockParserReadsBothForms proves the stack reader on fixtures:
// map and list forms, a commented-out key that is not a key, a block that ends
// at the next field, and the service pick by image rather than by name.
func TestEnvironmentBlockParserReadsBothForms(t *testing.T) {
	content := strings.Join([]string{
		"services:",
		"  postgres:",
		"    image: postgres:18",
		"    environment:",
		"      POSTGRES_DB: ovumcy",
		"  ovumcy:",
		"    image: ${OVUMCY_IMAGE:-ghcr.io/ovumcy/ovumcy-web:v2.0.0}",
		"    environment:",
		"      REGISTRATION_MODE: ${REGISTRATION_MODE:-open}",
		"      # AUDIT_LOG_ENABLED: true",
		"      - TZ=UTC",
		"      DATABASE_URL: postgres://u:p@postgres/db",
		"    init: true",
		"    read_only: true",
		"",
	}, "\n")
	env, ok := parseOvumcyEnvironment(content)
	if !ok {
		t.Fatal("the ovumcy service must be found by its image")
	}
	if len(env) != 3 || env["REGISTRATION_MODE"] != "${REGISTRATION_MODE:-open}" || env["TZ"] != "UTC" {
		t.Fatalf("environment read wrongly: %v", env)
	}
	if _, found := env["AUDIT_LOG_ENABLED"]; found {
		t.Fatal("a commented-out line must not count as a forwarded key")
	}
	if _, found := env["POSTGRES_DB"]; found {
		t.Fatal("another service's environment must not be read")
	}
	if !composesDatabaseURL(stack{env: env}) {
		t.Fatal("a literal DATABASE_URL must count as composed")
	}
	if composesDatabaseURL(stack{env: map[string]string{"DATABASE_URL": "${DATABASE_URL:-}"}}) {
		t.Fatal("a passthrough DATABASE_URL must not count as composed")
	}
}

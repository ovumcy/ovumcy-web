package examplecompose

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// The example stacks carry an explicit `environment:` allowlist and no
// `env_file`, so a runtime key the binary reads that the stack does not forward
// is silently pinned to its in-code default. For REGISTRATION_MODE that default
// is `open`: a public HTTPS instance built from the documented proxy stack ran
// with self-service registration, and every remedy the operator docs name
// (editing .env, setting the variable) changed nothing.
//
// The set of keys is read from the binary's own type-checked sources, not
// listed here, so a key added tomorrow is judged by the next run: a stack either
// forwards it or the exemption table below says why it does not. A key is
// identified by declaration, never by the shape of the node that names it: the
// readers are the functions whose bodies hand a parameter to os.Getenv or
// os.LookupEnv (directly or through another reader), and a key is the constant
// value the type checker resolves for the argument in that position, whether it
// is a literal, a constant of this package or another, or a constant
// expression. An argument that is neither a constant nor a reader's own
// parameter cannot be resolved and fails the run instead of being skipped.

// envKeyShape is what a configuration variable name looks like.
var envKeyShape = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// isStdEnvRead is true for os.Getenv and os.LookupEnv, identified by the
// declaring package, so a user function that merely shares the name is not one.
func isStdEnvRead(fn *types.Func) bool {
	return fn.Pkg() != nil && fn.Pkg().Path() == "os" && (fn.Name() == "Getenv" || fn.Name() == "LookupEnv")
}

// calleeFunc resolves the function a call invokes to its declaration.
func calleeFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	var ident *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return nil
	}
	fn, _ := info.Uses[ident].(*types.Func)
	if fn == nil {
		return nil
	}
	return fn.Origin()
}

// keyArgIndexes lists which arguments of call name an environment variable:
// the first argument of os.Getenv/os.LookupEnv, or the parameters a derived
// reader hands to one.
func keyArgIndexes(info *types.Info, call *ast.CallExpr, readers map[*types.Func]map[int]bool) []int {
	fn := calleeFunc(info, call)
	if fn == nil {
		return nil
	}
	if isStdEnvRead(fn) {
		return []int{0}
	}
	var indexes []int
	for index := range readers[fn] {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

// paramIndex reports which parameter of fn the expression arg is, when it is
// exactly one.
func paramIndex(info *types.Info, fn *types.Func, arg ast.Expr) (int, bool) {
	ident, ok := ast.Unparen(arg).(*ast.Ident)
	if !ok {
		return 0, false
	}
	variable, ok := info.Uses[ident].(*types.Var)
	if !ok {
		return 0, false
	}
	params := fn.Type().(*types.Signature).Params()
	for i := range params.Len() {
		if params.At(i) == variable {
			return i, true
		}
	}
	return 0, false
}

// deriveReaders finds every function that reads an environment variable named
// by one of its parameters, and which parameters those are, by running to a
// fixed point: a function is a reader when its body passes a parameter to
// os.Getenv, os.LookupEnv or an already-derived reader.
func deriveReaders(pkgs []*packages.Package) map[*types.Func]map[int]bool {
	readers := map[*types.Func]map[int]bool{}
	for changed := true; changed; {
		changed = false
		for _, pkg := range pkgs {
			for _, file := range pkg.Syntax {
				for _, decl := range file.Decls {
					funcDecl, ok := decl.(*ast.FuncDecl)
					if !ok || funcDecl.Body == nil {
						continue
					}
					self, _ := pkg.TypesInfo.Defs[funcDecl.Name].(*types.Func)
					if self == nil {
						continue
					}
					ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
						call, ok := node.(*ast.CallExpr)
						if !ok {
							return true
						}
						for _, index := range keyArgIndexes(pkg.TypesInfo, call, readers) {
							if index >= len(call.Args) {
								continue
							}
							param, flows := paramIndex(pkg.TypesInfo, self, call.Args[index])
							if !flows || readers[self][param] {
								continue
							}
							if readers[self] == nil {
								readers[self] = map[int]bool{}
							}
							readers[self][param] = true
							changed = true
						}
						return true
					})
				}
			}
		}
	}
	return readers
}

// scanReads returns every environment variable the packages read, with the
// positions that read it, and the key arguments it could not resolve.
func scanReads(pkgs []*packages.Package, readers map[*types.Func]map[int]bool) (reads map[string][]string, unresolved []string) {
	reads = map[string][]string{}
	for _, pkg := range pkgs {
		info := pkg.TypesInfo
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				var self *types.Func
				if funcDecl, ok := decl.(*ast.FuncDecl); ok {
					self, _ = info.Defs[funcDecl.Name].(*types.Func)
				}
				ast.Inspect(decl, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					for _, index := range keyArgIndexes(info, call, readers) {
						if index >= len(call.Args) {
							continue
						}
						arg := call.Args[index]
						position := pkg.Fset.Position(arg.Pos())
						where := fmt.Sprintf("%s:%d", filepath.Base(position.Filename), position.Line)
						if value := info.Types[arg].Value; value != nil && value.Kind() == constant.String {
							key := constant.StringVal(value)
							if envKeyShape.MatchString(key) {
								reads[key] = append(reads[key], where)
							} else {
								unresolved = append(unresolved, fmt.Sprintf("%s: %q is read as an environment variable but is not shaped like one", where, key))
							}
							continue
						}
						if self != nil {
							if _, flows := paramIndex(info, self, arg); flows {
								continue
							}
						}
						unresolved = append(unresolved, fmt.Sprintf("%s: the environment variable name is neither a constant nor a parameter of the reader that passes it on", where))
					}
					return true
				})
			}
		}
	}
	return reads, unresolved
}

// binaryPackages loads the module packages the server binary is built from:
// cmd/ovumcy and every package under internal/ it imports, directly or not.
// Test files are excluded, and so is a package the binary never links, such as
// a test-support package that reads its own environment.
func binaryPackages(t *testing.T, root string) []*packages.Package {
	t.Helper()
	loaded := loadPackages(t, root, "./cmd/ovumcy", "./internal/...")
	byID := map[string]*packages.Package{}
	var main *packages.Package
	for _, pkg := range loaded {
		byID[pkg.ID] = pkg
		if strings.HasSuffix(pkg.PkgPath, "/cmd/ovumcy") {
			main = pkg
		}
	}
	if main == nil {
		t.Fatal("cmd/ovumcy was not among the loaded packages: the scan is not reaching the binary")
	}
	seen := map[string]bool{}
	var linked []*packages.Package
	var walk func(*packages.Package)
	walk = func(pkg *packages.Package) {
		if seen[pkg.ID] {
			return
		}
		seen[pkg.ID] = true
		if len(pkg.Syntax) > 0 {
			linked = append(linked, pkg)
		}
		for _, imported := range pkg.Imports {
			if next, ok := byID[imported.ID]; ok {
				walk(next)
			}
		}
	}
	walk(main)
	return linked
}

// loadPackages type-checks the packages matching patterns under dir, failing
// closed when any of them does not type-check: an unresolved identifier would
// leave exactly the evidence this scan reads empty.
func loadPackages(t *testing.T, dir string, patterns ...string) []*packages.Package {
	t.Helper()
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:   dir,
		Tests: false,
	}
	loaded, err := packages.Load(config, patterns...)
	if err != nil {
		t.Fatalf("type-checking %v: %v", patterns, err)
	}
	var problems []string
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			problems = append(problems, pkg.PkgPath+": "+packageError.Error())
		}
	}
	if len(problems) > 0 {
		t.Fatalf("the packages do not type-check, so no key could be identified:\n  %s", strings.Join(problems, "\n  "))
	}
	return loaded
}

// readerNamed returns the parameters of the reader declared as name in a
// package whose path ends with pkgSuffix.
func readerNamed(readers map[*types.Func]map[int]bool, pkgSuffix, name string) map[int]bool {
	for fn, indexes := range readers {
		if fn.Name() == name && fn.Pkg() != nil && strings.HasSuffix(fn.Pkg().Path(), pkgSuffix) {
			return indexes
		}
	}
	return nil
}

// stack is one shipped compose file's ovumcy service.
type stack struct {
	path string // slash-separated, relative to the repository root
	// env maps each key the service's environment block sets to its raw value.
	env map[string]string
}

var (
	serviceHeader = regexp.MustCompile(`(?m)^  [A-Za-z0-9_-]+:\s*$`)
	// environmentKey reads one entry: an optional list marker, the key, and an
	// optional separator with the value after it. A list entry with no
	// separator (`- KEY`) is compose's bare passthrough.
	environmentKey  = regexp.MustCompile(`^\s{6}(-\s*)?([A-Z][A-Z0-9_]*)(?:(\s*[:=])\s*(.*?))?\s*$`)
	environmentHead = regexp.MustCompile(`^\s{4}environment:\s*$`)
)

// parseOvumcyEnvironment returns the keys set in the environment block of the
// service that runs the ovumcy image, or ok=false when the file has no such
// service. The map form (`KEY: value`) and the list form (`- KEY=value`, and
// the bare `- KEY`, which compose reads from the shell or .env) are read; a
// commented-out line is not a key.
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
				key, listEntry, separated, value := match[2], match[1] != "", match[3] != "", match[4]
				switch {
				case separated:
					env[key] = value
				case listEntry:
					env[key] = "${" + key + "}"
				}
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
	return set && !isPassthrough("DATABASE_URL", value)
}

// fixesPostgresDriver is true for a stack that pins DB_DRIVER to postgres
// rather than leaving the choice to the operator.
func fixesPostgresDriver(s stack) bool {
	return unquoted(s.env["DB_DRIVER"]) == "postgres"
}

// fixesProxyTrust is true for a stack that sets TRUST_PROXY_ENABLED to true,
// that is, one whose topology puts a reverse proxy in front of the app.
func fixesProxyTrust(s stack) bool {
	return unquoted(s.env["TRUST_PROXY_ENABLED"]) == "true"
}

// unquoted strips the YAML quotes around a scalar.
func unquoted(value string) string {
	return strings.Trim(value, `"'`)
}

// isPassthrough is true when value hands the operator's setting of key to the
// app: `${KEY}`, `${KEY:-default}`, `${KEY:?message}` and the other
// substitution forms that name the key itself. A literal, or a substitution of
// a different variable, leaves the operator's value for key unread.
func isPassthrough(key, value string) bool {
	head := "${" + key
	value = unquoted(value)
	if !strings.HasPrefix(value, head) || len(value) == len(head) {
		return false
	}
	return strings.ContainsRune("}:-?+", rune(value[len(head)]))
}

// forwardingExemptions is the whole list of keys an example stack may leave
// out. A new key is not added here to make the test pass: it is added only with
// a reason an operator reading the stack would accept.
var forwardingExemptions = map[string]exemption{
	"PORT": {
		reason: "the example stacks address the app on its default port 8080 (the proxy configs name ovumcy:8080, the local postgres stack publishes 8080:8080), so a PORT override would break the stack instead of tuning it",
	},
	"OVUMCY_WEBHOOK_URL": {
		reason: "the operator CLI reads it for the single invocation that sets an owner's webhook endpoint, and refuses a run that also pipes a URL on stdin; carried in the container's own environment it would be ambient on every later run and make that second source impossible",
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

// pinnedLiterals is the whole list of keys an example stack may set to a fixed
// value instead of passing the operator's through. A key here is set by the
// stack on purpose, so a value in .env does not reach the app; the runbook names
// them. A key is not added to make the test pass: it is added only when the
// stack's own wiring (a mounted volume, the postgres service, the proxy
// network) would break if the operator could change the value.
var pinnedLiterals = map[string]exemption{
	"DB_DRIVER": {
		reason:  "the stack ships its own postgres service and is wired to it, so the driver is part of the stack",
		applies: fixesPostgresDriver,
	},
	"DATABASE_URL": {
		reason:  "the stack builds the URL from its own postgres service and the POSTGRES_* credentials, so it is derived rather than passed through",
		applies: composesDatabaseURL,
	},
	"DB_PATH": {
		reason:  "the path is a file inside the stack's data volume, so another path would write outside the volume and lose the data on the next container replacement",
		applies: func(s stack) bool { return !fixesPostgresDriver(s) },
	},
	"CALENDAR_FEED_FENCE_PATH": {
		reason: "the path is where the stack mounts the ovumcy_fence volume, so another path would write the restore fence outside the volume and disarm every calendar feed on each start",
	},
	"COOKIE_SECURE": {
		reason:  "the proxy stack serves the app over HTTPS only, so session cookies must stay Secure whatever .env says",
		applies: fixesProxyTrust,
	},
	"TRUST_PROXY_ENABLED": {
		reason: "whether a reverse proxy fronts the app is the stack's topology: trusting forwarded headers on a stack with no proxy lets any client choose its own address, and distrusting them behind the proxy puts every client behind the proxy's address",
	},
	"PROXY_HEADER": {
		reason:  "the header must be the one this stack's proxy config overwrites with the real client address",
		applies: fixesProxyTrust,
	},
	"TRUSTED_PROXIES": {
		reason:  "the range must be the stack's own proxy network, which the compose file declares",
		applies: fixesProxyTrust,
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

// judgeStacks holds every stack to every key: the key must reach the app as a
// passthrough of the operator's value, or be exempted (absent) or pinned (a
// literal) by the tables above. It returns what it refused, and which entries
// of each table it relied on.
func judgeStacks(keys map[string]bool, stacks []stack) (problems []string, exempted, pinned map[string]bool) {
	exempted = map[string]bool{}
	pinned = map[string]bool{}
	for _, key := range sortedKeys(keys) {
		for _, s := range stacks {
			if value, set := s.env[key]; set {
				if isPassthrough(key, value) {
					continue
				}
				if rule, ok := pinnedLiterals[key]; ok && (rule.applies == nil || rule.applies(s)) {
					pinned[key] = true
					continue
				}
				problems = append(problems, fmt.Sprintf("%s: sets %s to %q, so the operator's value is ignored; write `%s: ${%s:-<default>}`, or pin the key in pinnedLiterals with the reason the stack must fix it", s.path, key, value, key, key))
				continue
			}
			if rule, ok := forwardingExemptions[key]; ok && (rule.applies == nil || rule.applies(s)) {
				exempted[key] = true
				continue
			}
			problems = append(problems, fmt.Sprintf("%s: does not forward %s, so the app ignores the operator's value and runs on the in-code default; add `%s: ${%s:-}` to its environment block, or exempt the key in forwardingExemptions with the reason the stack must not carry it", s.path, key, key, key))
		}
	}
	return problems, exempted, pinned
}

// TestStackJudgmentRefusesWhatIgnoresTheOperator proves the judgment on
// fixture stacks: a literal that ignores .env, a key left out, and a
// substitution of another variable are each refused naming the key, while a
// passthrough, a pinned literal and an exempted absence are not.
func TestStackJudgmentRefusesWhatIgnoresTheOperator(t *testing.T) {
	keys := map[string]bool{"REGISTRATION_MODE": true, "HSTS_ENABLED": true, "AUDIT_LOG_ENABLED": true, "TRUST_PROXY_ENABLED": true, "PORT": true}
	fixture := stack{path: "fixture/docker-compose.yml", env: map[string]string{
		"REGISTRATION_MODE":   "open",
		"AUDIT_LOG_ENABLED":   "${HSTS_ENABLED:-false}",
		"TRUST_PROXY_ENABLED": `"true"`,
	}}
	problems, exempted, pinned := judgeStacks(keys, []stack{fixture})
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"sets REGISTRATION_MODE to", "sets AUDIT_LOG_ENABLED to", "does not forward HSTS_ENABLED,"} {
		if !strings.Contains(joined, want) {
			t.Errorf("want a refusal containing %q, got:\n%s", want, joined)
		}
	}
	if len(problems) != 3 {
		t.Errorf("want exactly the three refusals, got %d:\n%s", len(problems), joined)
	}
	if !pinned["TRUST_PROXY_ENABLED"] || !exempted["PORT"] {
		t.Errorf("a pinned literal and an exempted absence must be accepted and recorded, got pinned=%v exempted=%v", pinned, exempted)
	}

	problems, _, _ = judgeStacks(map[string]bool{"REGISTRATION_MODE": true}, []stack{{path: "fixture/docker-compose.yml", env: map[string]string{"REGISTRATION_MODE": "${REGISTRATION_MODE:-open}"}}})
	if len(problems) != 0 {
		t.Errorf("a passthrough must be accepted, got %v", problems)
	}
}

// TestEveryExampleStackForwardsEveryRuntimeConfigKey asserts that each key the
// binary reads reaches the app in every shipped example stack, or is exempted
// above with a stated reason.
func TestEveryExampleStackForwardsEveryRuntimeConfigKey(t *testing.T) {
	root := repoRoot(t)
	binary := binaryPackages(t, root)
	readers := deriveReaders(binary)
	reads, unresolved := scanReads(binary, readers)
	for _, problem := range unresolved {
		t.Errorf("%s: resolve it to a constant, or read it through a helper that takes the name as a parameter, so the example stacks can be held to it", problem)
	}
	keys := map[string]bool{}
	for key := range reads {
		keys[key] = true
	}

	// The scan has to reach the keys the failure that motivated it dropped, and
	// each way a key is spelled: a direct literal, the second name of a pair
	// reader, a *_FILE twin and a constant declared in another package. The
	// readers are asserted by name too, so a scan that derived none of them (and
	// so found only the direct os.Getenv reads) cannot pass on the keys alone.
	for _, want := range []string{"REGISTRATION_MODE", "HSTS_ENABLED", "RATE_LIMIT_PASSWORD_RESET_REDEEM_WINDOW", "SECRET_KEY_FILE", "CALENDAR_FEED_FENCE_PATH", "TZ"} {
		if !keys[want] {
			t.Errorf("the source scan did not find %s: it is not reaching the keys the binary reads (found %d)", want, len(keys))
		}
	}
	for name, wantIndexes := range map[string][]int{"getEnv": {0}, "getCredentialRateLimit": {0, 1}, "resolveSecretFromEnvOrFile": {0, 1}} {
		got := readerNamed(readers, "/cmd/ovumcy", name)
		for _, index := range wantIndexes {
			if !got[index] {
				t.Errorf("reader %s was not derived with parameter %d as a key (got %v): the scan is not following parameters into os.Getenv", name, index, got)
			}
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

	problems, exempted, pinned := judgeStacks(keys, stacks)
	for _, problem := range problems {
		t.Error(problem)
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
	for _, key := range sortedKeys(pinnedLiterals) {
		rule := pinnedLiterals[key]
		if strings.TrimSpace(rule.reason) == "" {
			t.Errorf("pin for %s has no reason", key)
		}
		if !keys[key] {
			t.Errorf("pin for %s names a key the binary no longer reads", key)
		}
		if !pinned[key] {
			t.Errorf("pin for %s is not needed by any stack: every stack passes it through, drop the entry", key)
		}
	}
}

// TestPassthroughNamesTheKeyItself proves the value check on fixtures: only a
// substitution of the key itself forwards the operator's setting.
func TestPassthroughNamesTheKeyItself(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"${REGISTRATION_MODE:-open}", true},
		{`"${REGISTRATION_MODE:-open}"`, true},
		{"${REGISTRATION_MODE}", true},
		{"${REGISTRATION_MODE:?set it}", true},
		{"open", false},
		{`"true"`, false},
		{"", false},
		{"${REGISTRATION_MODE", false},
		{"${REGISTRATION_MODE_OTHER:-open}", false},
		{"${OTHER_KEY:-open}", false},
		{"prefix-${REGISTRATION_MODE:-open}", false},
	} {
		if got := isPassthrough("REGISTRATION_MODE", tc.value); got != tc.want {
			t.Errorf("isPassthrough(REGISTRATION_MODE, %q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// fixtureModule writes a throwaway module under a temp directory and returns
// its packages, type-checked. The fixture owns every declaration the scan is
// proved on, so the proof does not depend on what cmd/ovumcy reads today.
func fixtureModule(t *testing.T, files map[string]string) []*packages.Package {
	t.Helper()
	dir := t.TempDir()
	files["go.mod"] = "module fixture\n\ngo 1.24\n"
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return loadPackages(t, dir, "./...")
}

// TestConfigKeyScanResolvesKeysByDeclaration proves the source scan on a
// fixture module: a key is whatever constant the type checker resolves for the
// argument a derived reader passes to os.Getenv, however it is spelled; a
// fallback is never a key; and a function that only borrows a reader's name is
// not one.
func TestConfigKeyScanResolvesKeysByDeclaration(t *testing.T) {
	pkgs := fixtureModule(t, map[string]string{
		"cfg/cfg.go": `package cfg

const RemoteEnv = "REMOTE_KEY"
`,
		"main.go": `package main

import (
	"os"

	"fixture/cfg"
)

const localEnv = "LOCAL_KEY"
const joinedEnv = "JOINED_" + "KEY"

func readA(name, fallback string) string { return readB(name) + fallback }

func readB(name string) string { return os.Getenv(name) }

func pair(first, second string, n int) {
	_ = readA(first, "x")
	_ = readA(second, "y")
}

func notAReader(s string) string { return s }

func getEnv(key string) string { return "static" }

func main() {
	_ = readA("ALPHA_KEY", "FALLBACK_VALUE")
	_ = readA(localEnv, "")
	_ = readA(joinedEnv, "")
	_ = readA(cfg.RemoteEnv, "")
	pair("PAIR_ONE", "PAIR_TWO", 3)
	_ = notAReader("ZETA_KEY")
	_ = getEnv("SHADOW_KEY")
	_, _ = os.LookupEnv("LOOKUP_KEY")
}
`,
	})
	readers := deriveReaders(pkgs)
	reads, unresolved := scanReads(pkgs, readers)
	if len(unresolved) != 0 {
		t.Fatalf("every key in the fixture resolves, got unresolved: %v", unresolved)
	}
	want := []string{"ALPHA_KEY", "JOINED_KEY", "LOCAL_KEY", "LOOKUP_KEY", "PAIR_ONE", "PAIR_TWO", "REMOTE_KEY"}
	if got := sortedKeys(reads); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keys read = %v, want %v", got, want)
	}
	if got := readerNamed(readers, "fixture", "pair"); !got[0] || !got[1] || got[2] {
		t.Fatalf("pair reads parameters 0 and 1 and not 2, got %v", got)
	}
	if got := readerNamed(readers, "fixture", "getEnv"); len(got) != 0 {
		t.Fatalf("a function named getEnv that reads nothing is not a reader, got %v", got)
	}
}

// TestConfigKeyScanRefusesAKeyItCannotResolve proves the other half: a name
// that is neither a constant nor a reader's own parameter fails the scan
// naming its position instead of being skipped.
func TestConfigKeyScanRefusesAKeyItCannotResolve(t *testing.T) {
	pkgs := fixtureModule(t, map[string]string{
		"main.go": `package main

import "os"

func main() {
	for _, name := range []string{"LOOP_ONE", "LOOP_TWO"} {
		_ = os.Getenv(name)
	}
	_ = os.Getenv("lower_case")
}
`,
	})
	reads, unresolved := scanReads(pkgs, deriveReaders(pkgs))
	if len(reads) != 0 {
		t.Fatalf("nothing in the fixture resolves to a key, got %v", reads)
	}
	joined := strings.Join(unresolved, "\n")
	for _, want := range []string{"main.go:7: the environment variable name is neither a constant nor a parameter", `main.go:9: "lower_case" is read as an environment variable but is not shaped like one`} {
		if !strings.Contains(joined, want) {
			t.Errorf("want an unresolved entry containing %q, got:\n%s", want, joined)
		}
	}
}

// TestEnvironmentBlockParserReadsBothForms proves the stack reader on fixtures:
// map and list forms, the bare list passthrough, a commented-out key that is
// not a key, a block that ends at the next field, and the service pick by image
// rather than by name.
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
		"      - HSTS_ENABLED",
		"      DATABASE_URL: postgres://u:p@postgres/db",
		"    init: true",
		"    read_only: true",
		"",
	}, "\n")
	env, ok := parseOvumcyEnvironment(content)
	if !ok {
		t.Fatal("the ovumcy service must be found by its image")
	}
	if len(env) != 4 || env["REGISTRATION_MODE"] != "${REGISTRATION_MODE:-open}" || env["TZ"] != "UTC" {
		t.Fatalf("environment read wrongly: %v", env)
	}
	if !isPassthrough("HSTS_ENABLED", env["HSTS_ENABLED"]) {
		t.Fatalf("a bare list entry is compose's passthrough of the same name, got %q", env["HSTS_ENABLED"])
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

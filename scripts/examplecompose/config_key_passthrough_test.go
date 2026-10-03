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
// forwards it or the exemption table below says why it does not.
//
// What the scan reads. The packages of this module that cmd/ovumcy links are
// type-checked, for the host's operating system and architecture with cgo off
// (the image's build). A key is the constant the type checker resolves for the
// argument in the name position of os.Getenv, os.LookupEnv or syscall.Getenv,
// whether it is a literal, a constant of another package or a constant
// expression; the argument is identified by its declaration, never by the shape
// of the node. A function of the module that hands one of its own parameters,
// unchanged, to such a reader (or to another one) is a reader of that parameter
// itself, and is found by running that to a fixed point rather than listed. An
// argument in a reader's name position that is neither a constant nor the
// enclosing function's own parameter cannot be resolved and fails the run
// instead of being skipped. A call or mention of os.Environ, os.ExpandEnv or
// os.Expand from the module fails the run, because the names read through them
// are not in the source.
//
// What it does not read, so that a green run is not taken for more:
//   - Reads inside dependencies, the standard library included. A library that
//     reads its own variable (time.LoadLocation reads ZONEINFO) is not a setting
//     the module chose and is not judged, and neither is one a library reads for
//     the module behind a call the scan cannot see into.
//   - Reads through a function value or an interface: a reader named without
//     being called (`lookup := os.LookupEnv`), a method expression, a function
//     typed parameter, an interface method. A name handed to one of those is not
//     followed, and nothing fails.
//   - A name that reaches a reader through a local variable, a struct field or a
//     closure's parameter fails closed when it is read directly; the same name
//     handed first to a function value is not seen at all.
//   - Platforms. Only the files the host compiles are read, so a reader in a
//     file for another operating system, or behind a build tag the host does not
//     set (the unsupported-terminal stub, the gofuzz harness), is not scanned.
//   - Code outside Go (an assembly stub, a linkname), and any accessor of the
//     whole environment other than the three above (exec.Cmd.Environ,
//     syscall.Environ) reached from the module.

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// readerSeeds are the standard-library functions that read a variable by the
// name they are given, by full name, with the index of the argument that carries
// it. Every reader of the module is derived from a body that passes a parameter
// to one of these.
var readerSeeds = map[string]int{
	"os.Getenv":      0,
	"os.LookupEnv":   0,
	"syscall.Getenv": 0,
}

// refusedEnvFuncs return the environment as a whole, or expand names found
// inside a string, so the variables read through them are not named in the
// source. The module may not use them.
var refusedEnvFuncs = map[string]bool{
	"os.Environ":   true,
	"os.ExpandEnv": true,
	"os.Expand":    true,
}

// loadModulePackages type-checks, from source, each package of the module that
// pattern links, and no other: the dependencies are read from the compiler's
// export data. It fails when any of them does not type-check, because an
// unresolved identifier would leave exactly the evidence this scan reads empty.
func loadModulePackages(dir, pattern string) ([]*packages.Package, error) {
	env := append(os.Environ(), "CGO_ENABLED=0")
	linked, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedModule | packages.NeedImports | packages.NeedDeps,
		Dir:  dir,
		Env:  env,
	}, pattern)
	if err != nil {
		return nil, fmt.Errorf("listing what %s links: %w", pattern, err)
	}
	var paths []string
	packages.Visit(linked, nil, func(pkg *packages.Package) {
		if pkg.Module != nil && pkg.Module.Main {
			paths = append(paths, pkg.PkgPath)
		}
	})
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s links no package of the module: the scan is not reaching the binary", pattern)
	}
	sort.Strings(paths)
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir: dir,
		Env: env,
	}, paths...)
	if err != nil {
		return nil, fmt.Errorf("type-checking %v: %w", paths, err)
	}
	var problems []string
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			problems = append(problems, pkg.PkgPath+": "+packageError.Error())
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("the packages do not type-check, so no key could be identified:\n  %s", strings.Join(problems, "\n  "))
	}
	return loaded, nil
}

// calledFunc returns the declared function or method that call invokes, or nil
// for a call through a function value, a method expression, a conversion or a
// builtin: those have no declaration to follow.
func calledFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	fun := ast.Unparen(call.Fun)
	switch node := fun.(type) {
	case *ast.IndexExpr: // explicit instantiation: read[int]("KEY")
		fun = ast.Unparen(node.X)
	case *ast.IndexListExpr:
		fun = ast.Unparen(node.X)
	}
	var ident *ast.Ident
	switch node := fun.(type) {
	case *ast.Ident:
		ident = node
	case *ast.SelectorExpr:
		if selection := info.Selections[node]; selection != nil && selection.Kind() == types.MethodExpr {
			return nil
		}
		ident = node.Sel
	}
	if ident == nil {
		return nil
	}
	fn, _ := info.Uses[ident].(*types.Func)
	if fn == nil {
		return nil
	}
	return fn.Origin()
}

// keyArgIndexes lists which arguments of call name an environment variable: the
// name argument of a seed reader, or the parameters a derived reader hands to
// one.
func keyArgIndexes(info *types.Info, call *ast.CallExpr, readers map[*types.Func]map[int]bool) []int {
	fn := calledFunc(info, call)
	if fn == nil {
		return nil
	}
	if index, seed := readerSeeds[fn.FullName()]; seed {
		return []int{index}
	}
	var indexes []int
	for index := range readers[fn] {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

// paramIndex reports which parameter of fn the expression arg is, when it is
// exactly that parameter and nothing computed from it.
func paramIndex(info *types.Info, fn *types.Func, arg ast.Expr) (int, bool) {
	ident, ok := ast.Unparen(arg).(*ast.Ident)
	if !ok || fn == nil {
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

// deriveReaders finds every function of pkgs that reads an environment variable
// named by one of its parameters, and which parameters those are, by running to
// a fixed point: a function is a reader when its body passes a parameter to a
// seed reader or to an already-derived reader.
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

// moduleScan is what the type-checked packages say about the environment.
type moduleScan struct {
	// reads maps each key to the positions that read it.
	reads map[string][]string
	// readers maps each derived reader to the parameters it reads a variable by.
	readers map[*types.Func]map[int]bool
	// problems are the key arguments that could not be resolved and the refused
	// uses, each with its position.
	problems []string
}

// scanModule type-checks the packages of the module under dir that pattern
// links and returns every environment variable they read.
func scanModule(dir, pattern string) (moduleScan, error) {
	pkgs, err := loadModulePackages(dir, pattern)
	if err != nil {
		return moduleScan{}, err
	}
	scan := moduleScan{reads: map[string][]string{}, readers: deriveReaders(pkgs)}
	for _, pkg := range pkgs {
		info := pkg.TypesInfo
		where := func(node ast.Node) string {
			position := pkg.Fset.Position(node.Pos())
			return fmt.Sprintf("%s:%d", filepath.Base(position.Filename), position.Line)
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				var self *types.Func
				if funcDecl, ok := decl.(*ast.FuncDecl); ok {
					self, _ = info.Defs[funcDecl.Name].(*types.Func)
				}
				ast.Inspect(decl, func(node ast.Node) bool {
					switch node := node.(type) {
					case *ast.Ident:
						if fn, isFunc := info.Uses[node].(*types.Func); isFunc && refusedEnvFuncs[fn.FullName()] {
							scan.problems = append(scan.problems, fmt.Sprintf("%s: %s hands back the whole environment, or expands names found in a string, so the variables read through it are not named in the source; read each variable by name through os.Getenv or os.LookupEnv", where(node), fn.FullName()))
						}
					case *ast.CallExpr:
						scan.addReads(info, self, node, where)
					}
					return true
				})
			}
		}
	}
	sort.Strings(scan.problems)
	return scan, nil
}

// addReads records the keys call reads: the constant in each name position, or
// a problem when that argument is neither a constant nor the enclosing
// function's own parameter.
func (s *moduleScan) addReads(info *types.Info, self *types.Func, call *ast.CallExpr, where func(ast.Node) string) {
	for _, index := range keyArgIndexes(info, call, s.readers) {
		if index >= len(call.Args) {
			continue
		}
		arg := call.Args[index]
		if value := info.Types[arg].Value; value != nil && value.Kind() == constant.String {
			key := constant.StringVal(value)
			s.reads[key] = append(s.reads[key], where(arg))
			continue
		}
		if _, flows := paramIndex(info, self, arg); flows {
			continue
		}
		s.problems = append(s.problems, fmt.Sprintf("%s: the environment variable name passed to %s is neither a constant nor a parameter of the function that passes it on, so no key can be identified; resolve it to a constant, or read it through a helper that takes the name as a parameter", where(arg), calledFunc(info, call).FullName()))
	}
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
	// environmentKey reads one entry up to its key: an optional list marker, the
	// key, and whatever follows it on the line. A list entry with no separator
	// (`- KEY`) is compose's bare passthrough.
	environmentKey = regexp.MustCompile(`^\s{6}(-\s*)?([A-Z][A-Z0-9_]*)(.*)$`)
	// environmentTail splits what follows the key into its separator and the raw
	// value, which may still carry an inline comment.
	environmentTail = regexp.MustCompile(`^(\s*[:=])\s*(.*)$`)
	environmentHead = regexp.MustCompile(`^\s{4}environment:\s*$`)
)

// entryValue returns the value of an environment entry without the inline YAML
// comment after it. A comment starts at a `#` that begins the value or follows
// whitespace, so the `#` inside `${KEY:-a#b}` is part of the value. In the map
// form (`KEY: "a # b"`) a quoted scalar ends at its closing quote and a `#`
// inside it is content; the list form (`- KEY=value`) is one plain scalar, so
// quotes there protect nothing.
func entryValue(raw string, mapForm bool) string {
	if mapForm && raw != "" && (raw[0] == '"' || raw[0] == '\'') {
		quote := raw[0]
		for i := 1; i < len(raw); i++ {
			switch {
			case quote == '"' && raw[i] == '\\':
				i++
			case raw[i] == quote && quote == '\'' && i+1 < len(raw) && raw[i+1] == '\'':
				i++
			case raw[i] == quote:
				return raw[:i+1]
			}
		}
		return strings.TrimSpace(raw)
	}
	for i := range len(raw) {
		if raw[i] == '#' && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t') {
			return strings.TrimSpace(raw[:i])
		}
	}
	return strings.TrimSpace(raw)
}

// environmentKeyLine is true for a line that is one environment entry: a key
// followed by a separator and a value, or by nothing but an optional comment.
func environmentKeyLine(line string) bool {
	match := environmentKey.FindStringSubmatch(line)
	if match == nil {
		return false
	}
	tail := match[3]
	if environmentTail.MatchString(tail) {
		return true
	}
	rest := strings.TrimLeft(tail, " \t")
	return rest == "" || (rest != tail && strings.HasPrefix(rest, "#"))
}

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
			case environmentKeyLine(line):
				match := environmentKey.FindStringSubmatch(line)
				key, listEntry := match[2], match[1] != ""
				tail := environmentTail.FindStringSubmatch(match[3])
				switch {
				case tail != nil:
					separator := strings.TrimSpace(tail[1])
					value := entryValue(tail[2], separator == ":" && !listEntry)
					if separator == ":" && !listEntry && value == "" {
						// `KEY:` with no value is null in YAML, which compose
						// resolves from the shell or .env like the bare list entry.
						value = "${" + key + "}"
					}
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
// app unchanged: `${KEY}`, `${KEY:-default}`, `${KEY-default}`,
// `${KEY:?message}` and `${KEY?message}`, and nothing after the closing brace.
// A literal, a substitution of a different variable, the alternate-value forms
// (`${KEY:+alt}` and `${KEY+alt}` yield `alt` exactly when the operator did set
// the key) and a substitution with text around it all leave the operator's
// value for key unread.
func isPassthrough(key, value string) bool {
	rest, ok := strings.CutPrefix(unquoted(value), "${"+key)
	if !ok || rest == "" {
		return false
	}
	if rest == "}" {
		return true
	}
	rest = strings.TrimPrefix(rest, ":")
	if rest == "" || (rest[0] != '-' && rest[0] != '?') {
		return false
	}
	// The operand runs to the brace that closes this substitution, which must be
	// the last character; it may itself contain a nested `${...}`.
	depth := 1
	for i, r := range rest[1:] {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i == len(rest[1:])-1
			}
		}
	}
	return false
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
	scan, err := scanModule(root, "./cmd/ovumcy")
	if err != nil {
		t.Fatalf("scanning the binary: %v", err)
	}
	for _, problem := range scan.problems {
		t.Error(problem)
	}
	keys := map[string]bool{}
	for key := range scan.reads {
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
		got := readerNamed(scan.readers, "/cmd/ovumcy", name)
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
		{"${REGISTRATION_MODE-open}", true},
		{"${REGISTRATION_MODE?set it}", true},
		{"${REGISTRATION_MODE:-${FALLBACK:-open}}", true},
		{"${REGISTRATION_MODE:+open}", false},
		{"${REGISTRATION_MODE+open}", false},
		{"${REGISTRATION_MODE:}", false},
		{"${REGISTRATION_MODE:=open}", false},
		{"${REGISTRATION_MODE}-x", false},
		{"${REGISTRATION_MODE:-open}x", false},
		{"${REGISTRATION_MODE:-open}${REGISTRATION_MODE:-closed}", false},
		{"${REGISTRATION_MODE:-open", false},
	} {
		if got := isPassthrough("REGISTRATION_MODE", tc.value); got != tc.want {
			t.Errorf("isPassthrough(REGISTRATION_MODE, %q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// fixtureModule writes a throwaway module under a temp directory and scans
// every package in it. The fixture owns every declaration the scan is proved on,
// so the proof does not depend on what cmd/ovumcy reads today.
func fixtureModule(t *testing.T, files map[string]string) moduleScan {
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
	scan, err := scanModule(dir, "./...")
	if err != nil {
		t.Fatal(err)
	}
	return scan
}

// TestConfigKeyScanResolvesKeysByDeclaration proves the source scan on a
// fixture module: a key is whatever constant the type checker resolves for the
// argument a derived reader passes to os.Getenv, however it is spelled and
// whichever package of the module declares the reader; a fallback is never a
// key; and a function that only borrows a reader's name is not one.
func TestConfigKeyScanResolvesKeysByDeclaration(t *testing.T) {
	scan := fixtureModule(t, map[string]string{
		"cfg/cfg.go": `package cfg

import "os"

const RemoteEnv = "REMOTE_KEY"

func Read(name string) string { return os.Getenv(name) }
`,
		"main.go": `package main

import (
	"os"
	"syscall"

	"fixture/cfg"
)

type source struct{}

func (source) read(name string) string { return os.Getenv(name) }

func readAs[T any](name string) T {
	var zero T
	_ = os.Getenv(name)
	return zero
}

const localEnv = "LOCAL_KEY"
const joinedEnv = "JOINED_" + "KEY"

var atInit = os.Getenv("INIT_KEY")

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
	_ = cfg.Read("CROSS_PACKAGE_KEY")
	pair("PAIR_ONE", "PAIR_TWO", 3)
	_ = notAReader("ZETA_KEY")
	_ = getEnv("SHADOW_KEY")
	_, _ = os.LookupEnv("LOOKUP_KEY")
	_, _ = syscall.Getenv("SYSCALL_KEY")
	_ = readAs[int]("GENERIC_KEY")
	_ = readAs[string](localEnv)
	_ = source{}.read("METHOD_KEY")
}
`,
	})
	if len(scan.problems) != 0 {
		t.Fatalf("every key in the fixture resolves, got problems: %v", scan.problems)
	}
	want := []string{"ALPHA_KEY", "CROSS_PACKAGE_KEY", "GENERIC_KEY", "INIT_KEY", "JOINED_KEY", "LOCAL_KEY", "LOOKUP_KEY", "METHOD_KEY", "PAIR_ONE", "PAIR_TWO", "REMOTE_KEY", "SYSCALL_KEY"}
	if got := sortedKeys(scan.reads); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("keys read = %v, want %v", got, want)
	}
	if got := readerNamed(scan.readers, "fixture", "pair"); !got[0] || !got[1] || got[2] {
		t.Fatalf("pair reads parameters 0 and 1 and not 2, got %v", got)
	}
	if got := readerNamed(scan.readers, "fixture/cfg", "Read"); !got[0] {
		t.Fatalf("cfg.Read reads parameter 0, got %v", got)
	}
	if got := readerNamed(scan.readers, "fixture", "getEnv"); len(got) != 0 {
		t.Fatalf("a function named getEnv that reads nothing is not a reader, got %v", got)
	}
}

// TestConfigKeyScanRefusesWhatItCannotResolve proves the other half: a name that
// is neither a constant nor a reader's own parameter fails the scan naming its
// position instead of being skipped, and so does each use of os.Environ,
// os.ExpandEnv and os.Expand, called or merely named.
func TestConfigKeyScanRefusesWhatItCannotResolve(t *testing.T) {
	scan := fixtureModule(t, map[string]string{
		"main.go": `package main

import "os"

func loop() {
	for _, name := range []string{"LOOP_ONE", "LOOP_TWO"} {
		_ = os.Getenv(name)
	}
}

func closure() func(string) string {
	return func(name string) string { return os.Getenv(name) }
}

func environ() []string { return os.Environ() }

func expandEnv() string { return os.ExpandEnv("${EXPAND_KEY}") }

func expand() string { return os.Expand("${EXPAND_FN_KEY}", func(string) string { return "" }) }

func namedOnly() func() []string { return os.Environ }

func main() {}
`,
	})
	if len(scan.reads) != 0 {
		t.Fatalf("nothing in the fixture resolves to a key, got %v", scan.reads)
	}
	joined := strings.Join(scan.problems, "\n")
	for _, want := range []string{
		"main.go:7: the environment variable name passed to os.Getenv is neither a constant nor a parameter",
		"main.go:12: the environment variable name passed to os.Getenv is neither a constant nor a parameter",
		"main.go:15: os.Environ hands back the whole environment",
		"main.go:17: os.ExpandEnv hands back the whole environment",
		"main.go:19: os.Expand hands back the whole environment",
		"main.go:21: os.Environ hands back the whole environment",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("want a problem containing %q, got:\n%s", want, joined)
		}
	}
	if len(scan.problems) != 6 {
		t.Errorf("want exactly the six problems, got %d:\n%s", len(scan.problems), joined)
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
		"      LOG_LEVEL:",
		"      - EMPTY_LITERAL=",
		"      RATE_LIMIT_API_MAX: ${RATE_LIMIT_API_MAX:-300}  # far below the budget",
		"      QUOTED_HASH: \"a # b\"  # trailing",
		"      ESCAPED_QUOTE: \"a \\\" # b\"  # trailing",
		"      HASH_INSIDE: ${HASH_INSIDE:-a#b}",
		"      NULL_COMMENT: # nothing set here",
		"      - LIST_COMMENT=${LIST_COMMENT:-x} # note",
		"      - BARE_COMMENT # note",
		"      AFTER_BARE: ${AFTER_BARE:-y}",
		"      DATABASE_URL: postgres://u:p@postgres/db",
		"    init: true",
		"    read_only: true",
		"",
	}, "\n")
	env, ok := parseOvumcyEnvironment(content)
	if !ok {
		t.Fatal("the ovumcy service must be found by its image")
	}
	if !isPassthrough("LOG_LEVEL", env["LOG_LEVEL"]) {
		t.Fatalf("a map entry with no value is compose's passthrough of the same name, got %q", env["LOG_LEVEL"])
	}
	if isPassthrough("EMPTY_LITERAL", env["EMPTY_LITERAL"]) {
		t.Fatalf("a list entry that sets the key to nothing pins an empty literal, got %q", env["EMPTY_LITERAL"])
	}
	for _, key := range []string{"RATE_LIMIT_API_MAX", "HASH_INSIDE", "NULL_COMMENT", "LIST_COMMENT", "BARE_COMMENT", "AFTER_BARE"} {
		if !isPassthrough(key, env[key]) {
			t.Errorf("%s is a passthrough with a comment after it (or none), got %q", key, env[key])
		}
	}
	if env["QUOTED_HASH"] != `"a # b"` || env["ESCAPED_QUOTE"] != `"a \" # b"` {
		t.Errorf("a quoted value keeps the # inside its quotes and loses the comment after them, got %q and %q", env["QUOTED_HASH"], env["ESCAPED_QUOTE"])
	}
	if len(env) != 14 || env["REGISTRATION_MODE"] != "${REGISTRATION_MODE:-open}" || env["TZ"] != "UTC" {
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

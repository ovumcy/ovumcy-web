package examplecompose

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/constant"
	"go/parser"
	"go/token"
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
// readers are the functions whose bodies hand a parameter to os.Getenv,
// os.LookupEnv or syscall.Getenv (directly or through another reader), and a key
// is the constant value the type checker resolves for the argument in that
// position, whether it is a literal, a constant of this package or another, or a
// constant expression. An argument that is neither a constant nor a reader's own
// parameter cannot be resolved and fails the run instead of being skipped. A
// standard-library accessor of the whole environment (os.Environ, os.ExpandEnv,
// os.Expand and the rest of unfollowedEnvAccessors) is refused wherever it is
// called, because no name reaches it that could be followed.
//
// The scan is deliberately narrower than "every way a key can be read". A name
// held in a local variable, or transformed on its way into a function value or
// interface method of the module (`fn(strings.TrimSpace(name))`), is not
// tracked: that takes value tracking, and a direct os.Getenv of such a variable
// fails closed as unresolved while the same name behind a function value does
// not. Only a key-shaped constant handed straight to such a call, or through a
// helper that forwards a parameter unchanged, is refused.

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

// followedEnvReads are the standard-library functions that read one variable by
// the name they are given, with the index of the argument that carries it. They
// are matched by full name, so a user function that merely shares a name is not
// one.
var followedEnvReads = map[string]int{
	"os.Getenv":      0,
	"os.LookupEnv":   0,
	"syscall.Getenv": 0,
}

// unfollowedEnvAccessors are the standard-library functions that hand the
// process environment to the caller without a variable name as an argument, so
// a read through one cannot be followed to the key it wants. A call to one is
// refused, or sits in envAccessExemptions with the reason it reads nothing the
// operator configures. The value is why the scan cannot follow it.
var unfollowedEnvAccessors = map[string]string{
	"os.Environ":                     "it returns every variable, so the names used are chosen by whatever filters the result",
	"syscall.Environ":                "it returns every variable, so the names used are chosen by whatever filters the result",
	"(*os/exec.Cmd).Environ":         "it returns every variable the command will run with, so the names used are chosen by whatever filters the result",
	"os.ExpandEnv":                   "it substitutes the names written inside a string, which are not call arguments",
	"os.Expand":                      "it hands the names written inside a string to a mapping function the scan cannot see into",
	"syscall.GetEnvironmentVariable": "it takes the name as a UTF-16 pointer, not a string constant",
	"syscall.GetEnvironmentStrings":  "it returns every variable, so the names used are chosen by whatever filters the result",
	"syscall.FreeEnvironmentStrings": "it is the release half of GetEnvironmentStrings, which is refused where it is called",
}

// envWrites are the standard-library functions that change the environment
// without reading a value out of it. A key set here is read, if anywhere, by one
// of the readers above, which the scan judges where it reads.
var envWrites = map[string]bool{
	"os.Setenv": true, "os.Unsetenv": true, "os.Clearenv": true,
	"syscall.Setenv": true, "syscall.Unsetenv": true, "syscall.Clearenv": true,
	"syscall.SetEnvironmentVariable": true,
}

// envAccessExemptions lists the functions of the module allowed to call an
// unfollowed accessor, by the enclosing function's full name, with the reason
// the call reads no setting an operator configures. It is empty: an entry is
// added only with a reason an operator reading the stack would accept.
var envAccessExemptions = map[string]string{}

// stdEnvReadIndex returns the argument of fn that names the variable it reads,
// for os.Getenv, os.LookupEnv and syscall.Getenv.
func stdEnvReadIndex(fn *types.Func) (int, bool) {
	index, ok := followedEnvReads[fn.FullName()]
	return index, ok
}

// envAccessorReason returns why fn, a standard-library accessor of the whole
// environment, cannot be followed to a key.
func envAccessorReason(fn *types.Func) (string, bool) {
	reason, ok := unfollowedEnvAccessors[fn.FullName()]
	return reason, ok
}

// callee is what a call invokes, as far as the type checker can tell.
type callee struct {
	// fn is the declared function or method, set when the call resolves to one
	// whose body can be followed.
	fn *types.Func
	// ident is the name the call goes through, when it goes through one.
	ident *ast.Ident
	// opaque marks a call that reaches its target through a function value, an
	// interface method or a method expression, so no declaration is followed.
	opaque bool
}

// declName returns the identifier that names a function or method in expr.
func declName(expr ast.Expr) *ast.Ident {
	switch node := ast.Unparen(expr).(type) {
	case *ast.Ident:
		return node
	case *ast.SelectorExpr:
		return node.Sel
	}
	return nil
}

// calleeOf resolves what call invokes. ok is false for a call that is not a
// call of anything a reader could hide in: a type conversion or a builtin.
func calleeOf(info *types.Info, call *ast.CallExpr) (c callee, ok bool) {
	fun := ast.Unparen(call.Fun)
	if tv, found := info.Types[fun]; found && (tv.IsType() || tv.IsBuiltin()) {
		return callee{}, false
	}
	// An explicit instantiation (`read[int]("KEY")`) names the generic function.
	var instantiated ast.Expr
	switch node := fun.(type) {
	case *ast.IndexExpr:
		instantiated = node.X
	case *ast.IndexListExpr:
		instantiated = node.X
	}
	if instantiated != nil {
		if ident := declName(instantiated); ident != nil {
			if _, isFunc := info.Uses[ident].(*types.Func); isFunc {
				fun = ast.Unparen(instantiated)
			}
		}
	}
	ident := declName(fun)
	if ident == nil {
		return callee{opaque: true}, true
	}
	fn, isFunc := info.Uses[ident].(*types.Func)
	if !isFunc {
		return callee{opaque: true, ident: ident}, true
	}
	if sel, isSel := fun.(*ast.SelectorExpr); isSel {
		if selection := info.Selections[sel]; selection != nil && selection.Kind() == types.MethodExpr {
			return callee{opaque: true, ident: ident}, true
		}
	}
	if recv := fn.Type().(*types.Signature).Recv(); recv != nil && types.IsInterface(recv.Type()) {
		return callee{opaque: true, ident: ident, fn: fn.Origin()}, true
	}
	return callee{fn: fn.Origin(), ident: ident}, true
}

// keyArgIndexes lists which arguments of call name an environment variable:
// the name argument of a standard-library read (followedEnvReads), or the
// parameters a derived reader hands to one.
func keyArgIndexes(info *types.Info, call *ast.CallExpr, readers map[*types.Func]map[int]bool) []int {
	target, ok := calleeOf(info, call)
	if !ok || target.opaque || target.fn == nil {
		return nil
	}
	fn := target.fn
	if index, isRead := stdEnvReadIndex(fn); isRead {
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

// hidesItsBody is true for a call whose body the scan cannot follow and that
// can reach code of the module: a function value or method expression, or a
// method of an interface the module declares. An interface of another module
// (the standard library's io.Writer) is implemented by this module's types but
// is called by that other code, which is not handed this module's keys.
func hidesItsBody(target callee, scanned map[*types.Package]bool) bool {
	return target.opaque && (target.fn == nil || scanned[target.fn.Pkg()])
}

// isKeyShapedConstant is true for an argument that is a constant string shaped
// like an environment variable name.
func isKeyShapedConstant(info *types.Info, arg ast.Expr) bool {
	value := info.Types[arg].Value
	return value != nil && value.Kind() == constant.String && envKeyShape.MatchString(constant.StringVal(value))
}

// deriveOpaqueSinks finds every function that hands a string parameter to a
// call whose body the scan cannot follow, directly or through another such
// function, and which parameters those are. A key constant passed in that
// position goes where no reader can be seen, so scanReads refuses it. The
// string-typed parameters of one function are many and the calls that carry
// them are everywhere (a repository lookup by email), so a parameter alone is
// not reported: only a constant shaped like a variable name reaching one is.
func deriveOpaqueSinks(pkgs []*packages.Package, scanned map[*types.Package]bool) map[*types.Func]map[int]bool {
	sinks := map[*types.Func]map[int]bool{}
	for changed := true; changed; {
		changed = false
		for _, pkg := range pkgs {
			info := pkg.TypesInfo
			for _, file := range pkg.Syntax {
				for _, decl := range file.Decls {
					funcDecl, ok := decl.(*ast.FuncDecl)
					if !ok || funcDecl.Body == nil {
						continue
					}
					self, _ := info.Defs[funcDecl.Name].(*types.Func)
					if self == nil {
						continue
					}
					ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
						call, ok := node.(*ast.CallExpr)
						if !ok {
							return true
						}
						target, resolved := calleeOf(info, call)
						if !resolved {
							return true
						}
						for index, arg := range call.Args {
							forwardsToSink := target.fn != nil && sinks[target.fn][index]
							if !hidesItsBody(target, scanned) && !forwardsToSink {
								continue
							}
							param, flows := paramIndex(info, self, arg)
							if !flows || sinks[self][param] {
								continue
							}
							if basic, isBasic := info.TypeOf(arg).Underlying().(*types.Basic); !isBasic || basic.Info()&types.IsString == 0 {
								continue
							}
							if sinks[self] == nil {
								sinks[self] = map[int]bool{}
							}
							sinks[self][param] = true
							changed = true
						}
						return true
					})
				}
			}
		}
	}
	return sinks
}

// deriveReaders finds every function that reads an environment variable named
// by one of its parameters, and which parameters those are, by running to a
// fixed point: a function is a reader when its body passes a parameter to a
// standard-library read (followedEnvReads) or an already-derived reader.
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
	scanned := map[*types.Package]bool{}
	for _, pkg := range pkgs {
		scanned[pkg.Types] = true
	}
	sinks := deriveOpaqueSinks(pkgs, scanned)
	for _, pkg := range pkgs {
		info := pkg.TypesInfo
		where := func(pos ast.Node) string {
			position := pkg.Fset.Position(pos.Pos())
			return fmt.Sprintf("%s:%d", filepath.Base(position.Filename), position.Line)
		}
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				var self *types.Func
				if funcDecl, ok := decl.(*ast.FuncDecl); ok {
					self, _ = info.Defs[funcDecl.Name].(*types.Func)
				}
				exempt := false
				if self != nil {
					_, exempt = envAccessExemptions[self.FullName()]
				}
				// A reader is followed only through a call. Naming one without
				// calling it (`lookup := os.LookupEnv`) hands it to code the scan
				// does not read, so each such use is refused.
				called := map[*ast.Ident]bool{}
				ast.Inspect(decl, func(node ast.Node) bool {
					if call, ok := node.(*ast.CallExpr); ok {
						if target, resolved := calleeOf(info, call); resolved && target.ident != nil {
							called[target.ident] = true
						}
					}
					return true
				})
				ast.Inspect(decl, func(node ast.Node) bool {
					ident, ok := node.(*ast.Ident)
					if !ok || called[ident] {
						return true
					}
					fn, isFunc := info.Uses[ident].(*types.Func)
					if !isFunc {
						return true
					}
					_, isRead := stdEnvReadIndex(fn.Origin())
					_, isAccessor := envAccessorReason(fn.Origin())
					if isRead || readers[fn.Origin()] != nil || (isAccessor && !exempt) {
						unresolved = append(unresolved, fmt.Sprintf("%s: %s reads the environment and is used here without being called, so the names it is given cannot be followed", where(ident), fn.Name()))
					}
					return true
				})
				ast.Inspect(decl, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					if target, resolved := calleeOf(info, call); resolved {
						if target.fn != nil && !exempt {
							if reason, isAccessor := envAccessorReason(target.fn); isAccessor {
								unresolved = append(unresolved, fmt.Sprintf("%s: %s reads the environment and the scan cannot follow it to a variable name (%s); read each variable by name through os.Getenv or os.LookupEnv, or exempt the calling function in envAccessExemptions with the reason it reads no operator setting", where(call), target.fn.FullName(), reason))
							}
						}
						for index, arg := range call.Args {
							if !isKeyShapedConstant(info, arg) {
								continue
							}
							if hidesItsBody(target, scanned) {
								unresolved = append(unresolved, fmt.Sprintf("%s: a variable name is handed to a function value, interface method or method expression, whose body cannot be followed to see whether it reads the environment", where(arg)))
							} else if target.fn != nil && sinks[target.fn][index] {
								unresolved = append(unresolved, fmt.Sprintf("%s: a variable name is handed to %s, which passes it to a function value or interface method whose body cannot be followed to see whether it reads the environment", where(arg), target.fn.Name()))
							}
						}
					}
					for _, index := range keyArgIndexes(info, call, readers) {
						if index >= len(call.Args) {
							continue
						}
						arg := call.Args[index]
						at := where(arg)
						if value := info.Types[arg].Value; value != nil && value.Kind() == constant.String {
							key := constant.StringVal(value)
							if envKeyShape.MatchString(key) {
								reads[key] = append(reads[key], at)
							} else {
								unresolved = append(unresolved, fmt.Sprintf("%s: %q is read as an environment variable but is not shaped like one", at, key))
							}
							continue
						}
						if self != nil {
							if _, flows := paramIndex(info, self, arg); flows {
								continue
							}
						}
						unresolved = append(unresolved, fmt.Sprintf("%s: the environment variable name is neither a constant nor a parameter of the reader that passes it on", at))
					}
					return true
				})
			}
		}
	}
	return reads, unresolved
}

// binaryPackages loads the module packages the server binary is built from:
// cmd/ovumcy and every package of the module it imports, directly or not,
// wherever it lives in the tree. Test files are excluded, and so is a package
// the binary never links, such as a test-support package that reads its own
// environment. Every package of the module is loaded and the walk then keeps
// the linked ones; a linked module package that was not loaded fails the run
// instead of going unread.
func binaryPackages(t *testing.T, root string) []*packages.Package {
	t.Helper()
	loaded := loadPackages(t, root, "./...")
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
	modulePrefix := strings.TrimSuffix(main.PkgPath, "/cmd/ovumcy") + "/"
	seen := map[string]bool{}
	var linked []*packages.Package
	var notLoaded []string
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
			} else if strings.HasPrefix(imported.PkgPath, modulePrefix) {
				notLoaded = append(notLoaded, imported.PkgPath)
			}
		}
	}
	walk(main)
	if len(notLoaded) > 0 {
		sort.Strings(notLoaded)
		t.Fatalf("the binary links module packages the scan did not load, so their environment reads are unread: %s", strings.Join(notLoaded, ", "))
	}
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
	"io"
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
	_, _ = syscall.Getenv("SYSCALL_KEY")
	_ = readAs[int]("GENERIC_KEY")
	_ = readAs[string](localEnv)
	_ = source{}.read("METHOD_KEY")
	var sink io.StringWriter
	_, _ = sink.WriteString("GET")
}
`,
	})
	readers := deriveReaders(pkgs)
	reads, unresolved := scanReads(pkgs, readers)
	if len(unresolved) != 0 {
		t.Fatalf("every key in the fixture resolves, got unresolved: %v", unresolved)
	}
	want := []string{"ALPHA_KEY", "GENERIC_KEY", "JOINED_KEY", "LOCAL_KEY", "LOOKUP_KEY", "METHOD_KEY", "PAIR_ONE", "PAIR_TWO", "REMOTE_KEY", "SYSCALL_KEY"}
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

// TestConfigKeyScanRefusesACallItCannotFollow proves that a read cannot hide
// where the scan has no declaration to follow: an interface of the module whose
// implementation reads the environment, a function value, a method expression,
// a helper that forwards its parameter to one of those, and a reader or
// os.LookupEnv named without being called. Each is refused naming its position;
// a call through an interface the module does not declare is not, and neither
// is a helper that forwards a parameter when no variable name is passed to it.
func TestConfigKeyScanRefusesACallItCannotFollow(t *testing.T) {
	pkgs := fixtureModule(t, map[string]string{
		"main.go": `package main

import (
	"io"
	"os"
)

type envSource interface{ Get(name string) string }

type osSource struct{}

func (osSource) Get(name string) string { return os.Getenv(name) }

func (osSource) read(name string) string { return os.Getenv(name) }

func readB(name string) string { return os.Getenv(name) }

func throughInterface(src envSource) string { return src.Get("IFACE_KEY") }

func throughValue(fn func(string) string) string { return fn("VALUE_KEY") }

func forwardsAParameter(fn func(string) string, name string) string { return fn(name) }

func throughAHelper() string { return forwardsAParameter(nil, "HELPER_KEY") }

func throughMethodExpression() string { return osSource.read(osSource{}, "EXPR_KEY") }

func namedOnly() func(string) (string, bool) { return os.LookupEnv }

func readerNamedOnly() func(string) string { return readB }

func main() {
	var sink io.StringWriter
	_, _ = sink.WriteString("GET")
}
`,
	})
	_, unresolved := scanReads(pkgs, deriveReaders(pkgs))
	joined := strings.Join(unresolved, "\n")
	for _, line := range []int{18, 20, 24, 26, 28, 30} {
		if !strings.Contains(joined, fmt.Sprintf("main.go:%d:", line)) {
			t.Errorf("want an unresolved entry at main.go:%d, got:\n%s", line, joined)
		}
	}
	if len(unresolved) != 6 {
		t.Errorf("want exactly the six hidden reads refused (the standard-library interface call is not one), got %d:\n%s", len(unresolved), joined)
	}
}

// TestConfigKeyScanRefusesAnEnvironmentReadItCannotFollow proves that the
// accessors of the whole environment cannot be used to read a key unseen: each
// of os.Environ, syscall.Environ, os.ExpandEnv, os.Expand and exec.Cmd.Environ
// is refused where it is called, and one named without being called is refused
// too. A function listed in envAccessExemptions is the only one not refused.
func TestConfigKeyScanRefusesAnEnvironmentReadItCannotFollow(t *testing.T) {
	pkgs := fixtureModule(t, map[string]string{
		"main.go": `package main

import (
	"os"
	"os/exec"
	"syscall"
)

func viaEnviron() []string { return os.Environ() }

func viaSyscallEnviron() []string { return syscall.Environ() }

func viaExpandEnv() string { return os.ExpandEnv("${EXPAND_KEY}") }

func viaExpand() string {
	return os.Expand("${EXPAND_FN_KEY}", func(name string) string { return name })
}

func viaCommand() []string { return exec.Command("true").Environ() }

func namedOnly() func() []string { return os.Environ }

func exempted() string { return os.ExpandEnv("${EXEMPT_KEY}") }

func main() {}
`,
	})
	scan := func() []string {
		_, unresolved := scanReads(pkgs, deriveReaders(pkgs))
		return unresolved
	}
	unresolved := scan()
	joined := strings.Join(unresolved, "\n")
	for _, want := range []string{
		"os.Environ reads the environment and the scan cannot follow it",
		"syscall.Environ reads the environment and the scan cannot follow it",
		"os.ExpandEnv reads the environment and the scan cannot follow it",
		"os.Expand reads the environment and the scan cannot follow it",
		"(*os/exec.Cmd).Environ reads the environment and the scan cannot follow it",
		"Environ reads the environment and is used here without being called",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("want an unresolved entry containing %q, got:\n%s", want, joined)
		}
	}
	// Seven calls or uses name an accessor, and the exempted function's is the
	// one left out; the same scan with that exemption absent refuses it too.
	if len(unresolved) != 7 {
		t.Errorf("want the seven unexempted accessor uses refused, got %d:\n%s", len(unresolved), joined)
	}
	envAccessExemptions["fixture.exempted"] = "a fixture function that reads nothing an operator configures"
	t.Cleanup(func() { delete(envAccessExemptions, "fixture.exempted") })
	if exempted := scan(); len(exempted) != 6 {
		t.Errorf("want the exempted function's call left out, got %d:\n%s", len(exempted), strings.Join(exempted, "\n"))
	}
}

// TestEveryStandardEnvironmentAccessorIsClassified keeps the accessor tables
// whole against the standard library itself. The exported functions and methods
// of os, syscall and os/exec that name the environment, whichever platform
// declares them, are read from the Go source tree, and each must be followed as
// a read, refused as an accessor, or listed as a write. A table entry that names
// nothing in the source fails too, so a renamed or mistyped entry cannot leave
// the class unguarded.
func TestEveryStandardEnvironmentAccessorIsClassified(t *testing.T) {
	root := filepath.Join(build.Default.GOROOT, "src")
	named := regexp.MustCompile(`(?i)env|^Expand$`)
	found := map[string]bool{}
	for _, dir := range []string{"os", "syscall", "os/exec"} {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			t.Fatalf("reading the standard library source %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, filepath.FromSlash(dir), entry.Name()), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parsing %s/%s: %v", dir, entry.Name(), err)
			}
			for _, decl := range file.Decls {
				funcDecl, ok := decl.(*ast.FuncDecl)
				if !ok || !funcDecl.Name.IsExported() || !named.MatchString(funcDecl.Name.Name) {
					continue
				}
				if funcDecl.Recv == nil {
					found[dir+"."+funcDecl.Name.Name] = true
					continue
				}
				switch recv := funcDecl.Recv.List[0].Type.(type) {
				case *ast.StarExpr:
					if ident, isIdent := recv.X.(*ast.Ident); isIdent && ident.IsExported() {
						found["(*"+dir+"."+ident.Name+")."+funcDecl.Name.Name] = true
					}
				case *ast.Ident:
					if recv.IsExported() {
						found["("+dir+"."+recv.Name+")."+funcDecl.Name.Name] = true
					}
				}
			}
		}
	}
	for _, name := range sortedKeys(found) {
		_, followed := followedEnvReads[name]
		_, refused := unfollowedEnvAccessors[name]
		if !followed && !refused && !envWrites[name] {
			t.Errorf("the standard library declares %s, which names the environment and is in none of followedEnvReads, unfollowedEnvAccessors or envWrites: follow it as a read, refuse it as an accessor, or list it as a write", name)
		}
	}
	listed := map[string]bool{}
	for name := range followedEnvReads {
		listed[name] = true
	}
	for name := range unfollowedEnvAccessors {
		listed[name] = true
	}
	for name := range envWrites {
		listed[name] = true
	}
	for _, name := range sortedKeys(listed) {
		if !found[name] {
			t.Errorf("the accessor tables name %s, which the standard library source does not declare", name)
		}
	}
	for name, reason := range envAccessExemptions {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("the exemption for %s has no reason", name)
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

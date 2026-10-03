package examplecompose

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
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
// identified by declaration, never by the shape of the node that names it.
//
// The function bodies of EVERY package the binary links are read, the standard
// library and the dependencies included, and the classes below are derived from
// them, so an accessor added by a new dependency is classified without a table
// edit. Only the primitives at the bottom of the standard library are named:
// syscall.Getenv, the one function that reads a variable by name, and
// syscall.Environ and GetEnvironmentStrings, which return the whole environment
// (the write primitives only keep a write out of the refusals).
//   - A reader is a function whose body hands a parameter, unchanged, to a
//     reader (directly or through another one) down to syscall.Getenv: os.Getenv,
//     os.LookupEnv, golang.org/x/sys/unix.Getenv and golang.org/x/sys/windows.Getenv
//     are readers because their bodies are, not because they are listed. A key is
//     the constant value the type checker resolves for the argument in a
//     reader's key position, whether it is a literal, a constant of this package
//     or another, or a constant expression. An argument that is neither a
//     constant nor a reader's own parameter cannot be resolved and fails the
//     run instead of being skipped.
//   - An accessor is a function that returns what it took from the environment
//     without a key the caller named: its result mentions the whole environment,
//     a reader handed on as a value (os.ExpandEnv), or a read under a name the
//     function computes. A call to one from the module is refused, because no
//     name reaches it that could be followed; so is a call into a package outside
//     the module whose function name says "env" and that is neither a reader nor
//     a writer, which covers an operating-system call the bodies cannot show.
//   - A reader of one fixed variable inside a dependency (time.LoadLocation reads
//     ZONEINFO) is neither: its key is the dependency's own, not a setting the
//     module chose.
//
// The scan runs once for each operating system the module has code for, so a
// reader behind a build constraint is read where it is compiled, and a file of a
// linked package that no scanned system compiles fails the run.
//
// The scan is deliberately narrower than "every way a key can be read". A name
// held in a local variable, or transformed on its way into a function value or
// interface method of the module (`fn(strings.TrimSpace(name))`), is not
// tracked: that takes value tracking, and a direct os.Getenv of such a variable
// fails closed as unresolved while the same name behind a function value does
// not. Only a key-shaped constant handed straight to such a call, or through a
// helper that forwards a parameter unchanged, is refused. The accessor
// derivation follows a value through local variables and results, not through a
// struct field, a channel or a closure's captured state, and a read by code
// outside Go (an assembly stub, a linkname) is seen only through its name.

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

// followedEnvReads is the one function at the bottom of the standard library
// that reads a variable by the name it is given, with the index of the argument
// that carries it. Every other reader, os.Getenv and the dependencies' wrappers
// included, is derived from the bodies that hand their parameter down to it. It
// is matched by full name, so a user function that merely shares a name is not
// one.
var followedEnvReads = map[string]int{
	"syscall.Getenv": 0,
}

// wholeEnvPrimitives are the functions at the bottom of the standard library that
// return the process environment as a whole, so the names used are chosen by
// whatever filters the result. Every function whose result carries what these
// return is derived as an accessor.
var wholeEnvPrimitives = map[string]bool{
	"syscall.Environ":               true,
	"syscall.GetEnvironmentStrings": true,
}

// envWritePrimitives are the functions at the bottom of the standard library
// that change the environment without reading a value out of it. A function that
// reaches one of them is a writer, which the name-based refusal lets through: a
// key set that way is read, if anywhere, by a reader the scan judges where it
// reads.
var envWritePrimitives = map[string]bool{
	"syscall.Setenv": true, "syscall.Unsetenv": true, "syscall.Clearenv": true,
	"syscall.SetEnvironmentVariable": true,
}

// envFuncName is what a function that reaches the environment may be called. A
// function outside the module that matches it and that no body shows to be a
// reader or a writer is refused where the module calls it.
var envFuncName = regexp.MustCompile(`(?i)env`)

// envAccessExemptions lists the functions of the module allowed to call a
// refused accessor, by the enclosing function's full name, with the reason the
// call reads no setting an operator configures. It is empty: an entry is added
// only with a reason an operator reading the stack would accept.
var envAccessExemptions = map[string]string{}

// stdEnvReadIndex returns the argument of fn that names the variable it reads,
// for the primitive syscall.Getenv.
func stdEnvReadIndex(fn *types.Func) (int, bool) {
	index, ok := followedEnvReads[fn.FullName()]
	return index, ok
}

// linkedGraph is every package the binary links that has Go source, the standard
// library and the dependencies among them, and which of them belong to the
// module under scan.
type linkedGraph struct {
	all      []*packages.Package
	module   []*packages.Package
	inModule map[*types.Package]bool
}

// newLinkedGraph walks the imports of roots, each with the source the loader read
// for it.
func newLinkedGraph(roots []*packages.Package) linkedGraph {
	graph := linkedGraph{inModule: map[*types.Package]bool{}}
	seen := map[string]bool{}
	var walk func(*packages.Package)
	walk = func(pkg *packages.Package) {
		if seen[pkg.ID] {
			return
		}
		seen[pkg.ID] = true
		if len(pkg.Syntax) > 0 {
			graph.all = append(graph.all, pkg)
			if pkg.Module != nil && pkg.Module.Main {
				graph.module = append(graph.module, pkg)
				graph.inModule[pkg.Types] = true
			}
		}
		paths := make([]string, 0, len(pkg.Imports))
		for path := range pkg.Imports {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			walk(pkg.Imports[path])
		}
	}
	for _, root := range roots {
		walk(root)
	}
	return graph
}

// envModel is what the bodies of the linked graph say about the environment.
type envModel struct {
	graph linkedGraph
	// readers maps a function to the parameters it reads a variable by.
	readers map[*types.Func]map[int]bool
	// accessors are the functions that return the environment or a read under a
	// name they compute.
	accessors map[*types.Func]bool
	// writers are the functions that reach a write primitive.
	writers map[*types.Func]bool
}

func buildEnvModel(graph linkedGraph) envModel {
	readers := deriveReaders(graph.all)
	return envModel{
		graph:     graph,
		readers:   readers,
		accessors: deriveAccessors(graph.all, readers),
		writers:   deriveWriters(graph.all),
	}
}

// isRead is true for a function that reads a variable by a name it is given.
func (m envModel) isRead(fn *types.Func) bool {
	_, primitive := stdEnvReadIndex(fn)
	return primitive || m.readers[fn] != nil
}

// isAccessor is true for a primitive that returns the whole environment and for
// a function derived to return what such a primitive returns.
func (m envModel) isAccessor(fn *types.Func) bool {
	return wholeEnvPrimitives[fn.FullName()] || m.accessors[fn]
}

// refusal returns why fn, called from the module, cannot be followed to a key,
// and false when the call is one the scan follows or has nothing to follow in.
func (m envModel) refusal(fn *types.Func) (string, bool) {
	fn = fn.Origin()
	if m.graph.inModule[fn.Pkg()] {
		return "", false
	}
	if m.isAccessor(fn) {
		return "its body returns the environment, or a read under a name it computes, so the names used are chosen by whatever filters the result", true
	}
	if m.isRead(fn) || m.writers[fn] {
		return "", false
	}
	if envFuncName.MatchString(fn.Name()) {
		return "its name says it reaches the environment, and no body the scan can read shows it taking the variable name as an argument", true
	}
	return "", false
}

// isErrorType is true for the built-in error type, which carries no environment
// text and so does not carry the taint of the call that made it.
func isErrorType(t types.Type) bool {
	return t != nil && types.Identical(t, types.Universe.Lookup("error").Type())
}

// deriveAccessors finds, by running to a fixed point over every body, each
// function that hands the caller something it took from the environment without
// a key the caller named. A value is tainted when it is the whole environment
// (syscall.Environ, or an accessor), a reader handed on as a value, or the result
// of a read under a name that is neither a constant nor the function's own
// parameter; the taint follows local variables, and a function is an accessor
// when a result other than an error mentions it.
func deriveAccessors(pkgs []*packages.Package, readers map[*types.Func]map[int]bool) map[*types.Func]bool {
	accessors := map[*types.Func]bool{}
	reads := func(fn *types.Func) bool {
		_, primitive := stdEnvReadIndex(fn)
		return primitive || readers[fn] != nil
	}
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
					if self == nil || accessors[self] {
						continue
					}
					called := map[*ast.Ident]bool{}
					ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
						if call, isCall := node.(*ast.CallExpr); isCall {
							if target, resolved := calleeOf(info, call); resolved && target.ident != nil {
								called[target.ident] = true
							}
						}
						return true
					})
					tainted := map[types.Object]bool{}
					mentions := func(root ast.Node) bool {
						found := false
						ast.Inspect(root, func(node ast.Node) bool {
							if found {
								return false
							}
							switch node := node.(type) {
							case *ast.Ident:
								switch object := info.Uses[node].(type) {
								case *types.Var:
									found = tainted[object]
								case *types.Func:
									origin := object.Origin()
									found = wholeEnvPrimitives[origin.FullName()] || accessors[origin] || (!called[node] && reads(origin))
								}
							case *ast.CallExpr:
								for _, index := range keyArgIndexes(info, node, readers) {
									if index >= len(node.Args) {
										continue
									}
									arg := node.Args[index]
									if value := info.Types[arg].Value; value != nil && value.Kind() == constant.String {
										continue
									}
									if _, flows := paramIndex(info, self, arg); !flows {
										found = true
									}
								}
							}
							return !found
						})
						return found
					}
					taint := func(expr ast.Expr) {
						ident, isIdent := ast.Unparen(expr).(*ast.Ident)
						if !isIdent {
							return
						}
						object := info.ObjectOf(ident)
						if object != nil && !isErrorType(object.Type()) {
							tainted[object] = true
						}
					}
					for settled := false; !settled; {
						before := len(tainted)
						ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
							switch stmt := node.(type) {
							case *ast.AssignStmt:
								for _, rhs := range stmt.Rhs {
									if mentions(rhs) {
										for _, lhs := range stmt.Lhs {
											taint(lhs)
										}
										break
									}
								}
							case *ast.ValueSpec:
								for _, value := range stmt.Values {
									if mentions(value) {
										for _, name := range stmt.Names {
											taint(name)
										}
										break
									}
								}
							case *ast.RangeStmt:
								if mentions(stmt.X) {
									if stmt.Key != nil {
										taint(stmt.Key)
									}
									if stmt.Value != nil {
										taint(stmt.Value)
									}
								}
							}
							return true
						})
						settled = len(tainted) == before
					}
					exposes := false
					results := self.Type().(*types.Signature).Results()
					ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
						ret, isReturn := node.(*ast.ReturnStmt)
						if !isReturn || exposes {
							return !exposes
						}
						if len(ret.Results) == 0 {
							for i := range results.Len() {
								if tainted[results.At(i)] {
									exposes = true
								}
							}
							return true
						}
						for _, result := range ret.Results {
							if !isErrorType(info.TypeOf(result)) && mentions(result) {
								exposes = true
							}
						}
						return true
					})
					if exposes {
						accessors[self] = true
						changed = true
					}
				}
			}
		}
	}
	return accessors
}

// deriveWriters finds every function that reaches a write primitive, directly or
// through another writer.
func deriveWriters(pkgs []*packages.Package) map[*types.Func]bool {
	writers := map[*types.Func]bool{}
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
					if self == nil || writers[self] {
						continue
					}
					ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
						ident, isIdent := node.(*ast.Ident)
						if !isIdent || writers[self] {
							return !writers[self]
						}
						if fn, isFunc := info.Uses[ident].(*types.Func); isFunc && (envWritePrimitives[fn.Origin().FullName()] || writers[fn.Origin()]) {
							writers[self] = true
							changed = true
						}
						return true
					})
				}
			}
		}
	}
	return writers
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

// scan returns every environment variable the module's packages read, with the
// positions that read it, and the key arguments it could not resolve. The
// readers and accessors it follows or refuses are the ones derived from every
// linked package.
func (m envModel) scan() (reads map[string][]string, unresolved []string) {
	reads = map[string][]string{}
	readers := m.readers
	scanned := m.graph.inModule
	sinks := deriveOpaqueSinks(m.graph.module, scanned)
	for _, pkg := range m.graph.module {
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
					_, refused := m.refusal(fn)
					if m.isRead(fn.Origin()) || (refused && !exempt) {
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
							if reason, refused := m.refusal(target.fn); refused {
								unresolved = append(unresolved, fmt.Sprintf("%s: %s reaches the environment and the scan cannot follow it to a variable name (%s); read each variable by name through os.Getenv or os.LookupEnv, or exempt the calling function in envAccessExemptions with the reason it reads no operator setting", where(call), target.fn.FullName(), reason))
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

// scannedPlatforms are the operating systems the module's code is read for: the
// one the container image is built for, and the others the module has files
// for. TestEveryLinkedModuleFileIsReadOnSomePlatform fails when a file of a
// linked package is compiled on none of them, so a platform the module grows
// code for is named here by that failure.
var scannedPlatforms = []string{"linux", "windows", "darwin"}

// unscannedModuleFiles are the files of a linked module package that no scanned
// platform compiles, by path from the repository root, with the reason the
// binary never runs them. An entry is added only with a reason, and one that a
// scanned platform now compiles, or that names no file, fails the run.
var unscannedModuleFiles = map[string]string{
	"internal/cli/password_prompt_unsupported.go": "the stub for a platform with no terminal support: it returns a constant error and reads no variable",
	"internal/services/policy_fuzz_libfuzzer.go":  "built only under the gofuzz tag by the fuzzing toolchain, never into the server binary",
}

var (
	binaryModelsOnce sync.Once
	binaryModels     map[string]envModel
	binaryModelsErr  error
)

// binaryModel returns the model of cmd/ovumcy for goos, with every package it
// links, the dependencies and the standard library included, each with the source
// the type checker read. Test files are excluded, and so is a package the binary
// never links, such as a test-support package that reads its own environment.
// The build is the image's (cgo off). Every scanned platform is loaded once, side
// by side, because each load type-checks the whole graph from source.
func binaryModel(t *testing.T, root, goos string) envModel {
	t.Helper()
	binaryModelsOnce.Do(func() {
		binaryModels = map[string]envModel{}
		models := make([]envModel, len(scannedPlatforms))
		failures := make([]error, len(scannedPlatforms))
		var wg sync.WaitGroup
		for i, platform := range scannedPlatforms {
			wg.Add(1)
			go func() {
				defer wg.Done()
				roots, err := loadPackages(root, platform, "./cmd/ovumcy")
				if err == nil && (len(roots) != 1 || !strings.HasSuffix(roots[0].PkgPath, "/cmd/ovumcy")) {
					err = fmt.Errorf("cmd/ovumcy was not the loaded package: the scan is not reaching the binary")
				}
				if err != nil {
					failures[i] = fmt.Errorf("GOOS=%s: %w", platform, err)
					return
				}
				models[i] = buildEnvModel(newLinkedGraph(roots))
			}()
		}
		wg.Wait()
		binaryModelsErr = errors.Join(failures...)
		for i, platform := range scannedPlatforms {
			binaryModels[platform] = models[i]
		}
	})
	if binaryModelsErr != nil {
		t.Fatalf("loading the binary: %v", binaryModelsErr)
	}
	return binaryModels[goos]
}

// loadPackages type-checks the packages matching patterns under dir with their
// whole import graph, for goos (the host's when empty), failing closed when any
// package of the graph does not type-check: an unresolved identifier would leave
// exactly the evidence this scan reads empty.
func loadPackages(dir, goos string, patterns ...string) ([]*packages.Package, error) {
	env := append(os.Environ(), "CGO_ENABLED=0")
	if goos != "" {
		env = append(env, "GOOS="+goos)
	}
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedModule |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir:   dir,
		Env:   env,
		Tests: false,
	}
	loaded, err := packages.Load(config, patterns...)
	if err != nil {
		return nil, fmt.Errorf("type-checking %v: %w", patterns, err)
	}
	var problems []string
	packages.Visit(loaded, nil, func(pkg *packages.Package) {
		for _, packageError := range pkg.Errors {
			problems = append(problems, pkg.PkgPath+": "+packageError.Error())
		}
	})
	if len(problems) > 0 {
		return nil, fmt.Errorf("the packages do not type-check, so no key could be identified:\n  %s", strings.Join(problems, "\n  "))
	}
	return loaded, nil
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
	reads := map[string][]string{}
	for _, goos := range scannedPlatforms {
		platformReads, unresolved := binaryModel(t, root, goos).scan()
		for _, problem := range unresolved {
			t.Errorf("GOOS=%s, %s: resolve it to a constant, or read it through a helper that takes the name as a parameter, so the example stacks can be held to it", goos, problem)
		}
		for key, positions := range platformReads {
			reads[key] = append(reads[key], positions...)
		}
	}
	readers := binaryModel(t, root, scannedPlatforms[0]).readers
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
// the model of everything its packages link, type-checked. The fixture owns every
// declaration the scan is proved on, so the proof does not depend on what
// cmd/ovumcy reads today. A fixture that brings a go.mod of its own may require a
// dependency that another directory of the fixture provides.
func fixtureModule(t *testing.T, files map[string]string) envModel {
	t.Helper()
	dir := t.TempDir()
	if _, own := files["go.mod"]; !own {
		files["go.mod"] = "module fixture\n\ngo 1.24\n"
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	roots, err := loadPackages(dir, "", "./...")
	if err != nil {
		t.Fatal(err)
	}
	return buildEnvModel(newLinkedGraph(roots))
}

// TestConfigKeyScanResolvesKeysByDeclaration proves the source scan on a
// fixture module: a key is whatever constant the type checker resolves for the
// argument a derived reader passes to os.Getenv, however it is spelled; a
// fallback is never a key; and a function that only borrows a reader's name is
// not one.
func TestConfigKeyScanResolvesKeysByDeclaration(t *testing.T) {
	model := fixtureModule(t, map[string]string{
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
	readers := model.readers
	reads, unresolved := model.scan()
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
	model := fixtureModule(t, map[string]string{
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
	reads, unresolved := model.scan()
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
	model := fixtureModule(t, map[string]string{
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
	_, unresolved := model.scan()
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
// of os.Environ, syscall.Environ, os.ExpandEnv and exec.Cmd.Environ is refused
// where it is called, and one named without being called is refused too, while
// a standard-library function that reaches the environment without handing it
// back (a command's Run, a fixed-key read such as os.TempDir, a write) is not.
// os.Expand is refused only through what its mapping reads. A function listed in
// envAccessExemptions is the only one not refused.
func TestConfigKeyScanRefusesAnEnvironmentReadItCannotFollow(t *testing.T) {
	model := fixtureModule(t, map[string]string{
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
	return os.Expand("${EXPAND_FN_KEY}", func(name string) string { return os.Getenv(name) })
}

func viaCommand() []string { return exec.Command("true").Environ() }

func namedOnly() func() []string { return os.Environ }

func exempted() string { return os.ExpandEnv("${EXEMPT_KEY}") }

func runs() error { return exec.Command("true").Run() }

func tempDir() string { return os.TempDir() }

func writes() error { return os.Setenv("WRITTEN_KEY", "x") }

func main() {}
`,
	})
	scan := func() []string {
		_, unresolved := model.scan()
		return unresolved
	}
	unresolved := scan()
	joined := strings.Join(unresolved, "\n")
	for _, want := range []string{
		"os.Environ reaches the environment and the scan cannot follow it",
		"syscall.Environ reaches the environment and the scan cannot follow it",
		"os.ExpandEnv reaches the environment and the scan cannot follow it",
		"(*os/exec.Cmd).Environ reaches the environment and the scan cannot follow it",
		"Environ reads the environment and is used here without being called",
		"main.go:16: the environment variable name is neither a constant nor a parameter",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("want an unresolved entry containing %q, got:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{"Run", "TempDir", "Setenv", "os.Expand "} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("%s reaches the environment without handing it back and must not be refused, got:\n%s", unwanted, joined)
		}
	}
	// Seven calls or uses are refused, and the exempted function's is the one
	// left out; the same scan with that exemption absent refuses it too.
	if len(unresolved) != 7 {
		t.Errorf("want the seven unexempted uses refused, got %d:\n%s", len(unresolved), joined)
	}
	envAccessExemptions["fixture.exempted"] = "a fixture function that reads nothing an operator configures"
	t.Cleanup(func() { delete(envAccessExemptions, "fixture.exempted") })
	if exempted := scan(); len(exempted) != 6 {
		t.Errorf("want the exempted function's call left out, got %d:\n%s", len(exempted), strings.Join(exempted, "\n"))
	}
}

// funcNames returns the full names of the functions in set, for an assertion by
// the name a reader of the scan would look for.
func funcNames[V any](set map[*types.Func]V) map[string]bool {
	names := map[string]bool{}
	for fn := range set {
		names[fn.FullName()] = true
	}
	return names
}

// TestConfigKeyScanClassifiesADependencyByItsBody proves the closure on a
// dependency the scan has no table entry for: a wrapper that hands its parameter
// to a reader is followed to the key the module passes, one that returns the
// environment, a read under a name it computes or a reader handed on as a value
// is refused, directly or through another function, and the rest are left
// alone: a read of one fixed variable inside the dependency, a write, and a
// function that never reaches the environment. A name that says "env" with no
// body to show it is refused.
func TestConfigKeyScanClassifiesADependencyByItsBody(t *testing.T) {
	model := fixtureModule(t, map[string]string{
		"go.mod":     "module fixture\n\ngo 1.24\n\nrequire example.com/dep v0.0.0\n\nreplace example.com/dep => ./dep\n",
		"dep/go.mod": "module example.com/dep\n\ngo 1.24\n",
		"dep/dep.go": `package dep

import (
	"os"
	"strings"
	"syscall"
)

func Getenv(name string) string { v, _ := syscall.Getenv(name); return v }

func Lookup(name string) string { return Getenv(name) }

func Environ() []string { return syscall.Environ() }

func Filtered(prefix string) []string {
	var out []string
	for _, entry := range Environ() {
		if strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return out
}

func Expanded(s string) string { return os.ExpandEnv(s) }

func Home(windows bool) string {
	name := "HOME"
	if windows {
		name = "USERPROFILE"
	}
	return os.Getenv(name)
}

func Prefixed(name string) string { return os.Getenv("APP_" + strings.ToUpper(name)) }

func Fixed() string { return os.Getenv("FIXED_DEP_KEY") }

func Setenv(key, value string) error { return os.Setenv(key, value) }

func GetEnvironmentVariable(name *uint16) uint32 { return 0 }

func Unrelated() string { return "x" }
`,
		"main.go": `package main

import "example.com/dep"

func main() {
	_ = dep.Getenv("DEP_GETENV_KEY")
	_ = dep.Lookup("DEP_LOOKUP_KEY")
	_ = dep.Environ()
	_ = dep.Filtered("X")
	_ = dep.Expanded("$A")
	_ = dep.Home(false)
	_ = dep.Prefixed("a")
	_ = dep.Fixed()
	_ = dep.Setenv("DEP_SET_KEY", "x")
	_ = dep.GetEnvironmentVariable(nil)
	_ = dep.Unrelated()
}
`,
	})
	reads, unresolved := model.scan()
	if got := sortedKeys(reads); strings.Join(got, ",") != "DEP_GETENV_KEY,DEP_LOOKUP_KEY" {
		t.Errorf("keys read = %v, want the two the module passes to a dependency reader and not the dependency's own fixed one", got)
	}
	joined := strings.Join(unresolved, "\n")
	for _, name := range []string{"Environ", "Filtered", "Expanded", "Home", "Prefixed", "GetEnvironmentVariable"} {
		if !strings.Contains(joined, "example.com/dep."+name+" reaches the environment") {
			t.Errorf("want dep.%s refused, got:\n%s", name, joined)
		}
	}
	for _, name := range []string{"Getenv", "Lookup", "Fixed", "Setenv", "Unrelated"} {
		if strings.Contains(joined, "example.com/dep."+name+" ") {
			t.Errorf("dep.%s must not be refused, got:\n%s", name, joined)
		}
	}
	if len(unresolved) != 6 {
		t.Errorf("want exactly the six refusals, got %d:\n%s", len(unresolved), joined)
	}
	readers, accessors, writers := funcNames(model.readers), funcNames(model.accessors), funcNames(model.writers)
	for _, name := range []string{"Getenv", "Lookup"} {
		if !readers["example.com/dep."+name] || accessors["example.com/dep."+name] {
			t.Errorf("dep.%s is a reader and not an accessor", name)
		}
	}
	if !writers["example.com/dep.Setenv"] || accessors["example.com/dep.Setenv"] {
		t.Error("dep.Setenv is a writer and not an accessor")
	}
	if readers["example.com/dep.Fixed"] || accessors["example.com/dep.Fixed"] {
		t.Error("a read of one fixed variable inside a dependency is neither a reader nor an accessor")
	}
}

// TestEnvironmentModelDerivesWhatTheBinaryLinks holds the derivation to the
// real graph on every scanned platform, by the functions a reader of the scan
// would name: the readers and accessors of the standard library and of
// golang.org/x/sys that the module links must have been derived from their
// bodies (so an empty derivation cannot pass as a clean scan), a reader must not
// be an accessor, and the primitives the derivation starts from must exist in
// the standard library. A function that merely reaches the environment (a
// command's Run) must be neither.
func TestEnvironmentModelDerivesWhatTheBinaryLinks(t *testing.T) {
	root := repoRoot(t)
	primitives := map[string]bool{}
	for name := range followedEnvReads {
		primitives[name] = false
	}
	for name := range wholeEnvPrimitives {
		primitives[name] = false
	}
	for name := range envWritePrimitives {
		primitives[name] = false
	}
	for _, goos := range scannedPlatforms {
		model := binaryModel(t, root, goos)
		readers, accessors, writers := funcNames(model.readers), funcNames(model.accessors), funcNames(model.writers)
		xsys := "golang.org/x/sys/unix"
		if goos == "windows" {
			xsys = "golang.org/x/sys/windows"
		}
		for _, name := range []string{"os.Getenv", "os.LookupEnv", xsys + ".Getenv"} {
			if !readers[name] {
				t.Errorf("GOOS=%s: %s was not derived as a reader: the derivation is not following parameters down to syscall.Getenv", goos, name)
			}
			if accessors[name] {
				t.Errorf("GOOS=%s: %s reads by the name it is given and must not be an accessor", goos, name)
			}
		}
		for _, name := range []string{"os.Environ", "os.ExpandEnv", xsys + ".Environ"} {
			if !accessors[name] {
				t.Errorf("GOOS=%s: %s was not derived as an accessor: the derivation is not following results down to the environment primitives", goos, name)
			}
		}
		if !model.isAccessor(funcNamedIn(t, model, "syscall", "Environ")) {
			t.Errorf("GOOS=%s: syscall.Environ is a primitive accessor", goos)
		}
		if !writers["os.Setenv"] || accessors["os.Setenv"] || readers["os.Setenv"] {
			t.Errorf("GOOS=%s: os.Setenv is a writer, and neither a reader nor an accessor", goos)
		}
		if exec := funcNamedIn(t, model, "os/exec", "Environ"); exec != nil && !model.isAccessor(exec) {
			t.Errorf("GOOS=%s: (*os/exec.Cmd).Environ was not derived as an accessor", goos)
		}
		for _, name := range []string{"(*os/exec.Cmd).Run", "os.TempDir", "os.Setenv"} {
			if accessors[name] {
				t.Errorf("GOOS=%s: %s does not hand the environment back and must not be an accessor", goos, name)
			}
		}
		for _, pkg := range model.graph.all {
			if pkg.PkgPath != "syscall" {
				continue
			}
			for name := range primitives {
				if short, isSyscall := strings.CutPrefix(name, "syscall."); isSyscall && pkg.Types.Scope().Lookup(short) != nil {
					primitives[name] = true
				}
			}
		}
	}
	for _, name := range sortedKeys(primitives) {
		if !primitives[name] {
			t.Errorf("the derivation starts from %s, which the standard library does not declare on any scanned platform", name)
		}
	}
	for name, reason := range envAccessExemptions {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("the exemption for %s has no reason", name)
		}
	}
}

// funcNamedIn returns the function or method called name in the linked package
// whose path is pkgPath, or nil when the binary does not link that package.
func funcNamedIn(t *testing.T, model envModel, pkgPath, name string) *types.Func {
	t.Helper()
	for _, pkg := range model.graph.all {
		if pkg.PkgPath != pkgPath {
			continue
		}
		if fn, ok := pkg.Types.Scope().Lookup(name).(*types.Func); ok {
			return fn
		}
		for _, typeName := range pkg.Types.Scope().Names() {
			named, ok := pkg.Types.Scope().Lookup(typeName).(*types.TypeName)
			if !ok {
				continue
			}
			methods := types.NewMethodSet(types.NewPointer(named.Type()))
			for i := range methods.Len() {
				if fn, isFunc := methods.At(i).Obj().(*types.Func); isFunc && fn.Name() == name && fn.Pkg() == pkg.Types {
					return fn
				}
			}
		}
	}
	if pkgPath == "os/exec" {
		return nil
	}
	t.Fatalf("%s.%s is not declared in a linked package", pkgPath, name)
	return nil
}

// TestEveryLinkedModuleFileIsReadOnSomePlatform keeps the platform list whole:
// every Go file, other than a test, in the directory of a module package the
// binary links must be compiled on at least one scanned platform, so a reader
// behind a build constraint no scan compiles cannot go unread.
func TestEveryLinkedModuleFileIsReadOnSomePlatform(t *testing.T) {
	root := repoRoot(t)
	compiled := map[string]bool{}
	dirs := map[string]bool{}
	for _, goos := range scannedPlatforms {
		for _, pkg := range binaryModel(t, root, goos).graph.module {
			for _, file := range pkg.CompiledGoFiles {
				compiled[filepath.ToSlash(file)] = true
				dirs[filepath.ToSlash(filepath.Dir(file))] = true
			}
		}
	}
	for _, want := range []string{"internal/cli/password_prompt_unix.go", "internal/cli/password_prompt_windows.go"} {
		if !compiled[filepath.ToSlash(filepath.Join(root, filepath.FromSlash(want)))] {
			t.Errorf("%s is not among the files the platform scans compiled: the scan is not reaching the platform-specific readers", want)
		}
	}
	rootPrefix := filepath.ToSlash(root) + "/"
	seen := map[string]bool{}
	for _, dir := range sortedKeys(dirs) {
		entries, err := os.ReadDir(filepath.FromSlash(dir))
		if err != nil {
			t.Fatalf("reading %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := dir + "/" + name
			if compiled[path] {
				continue
			}
			relative := strings.TrimPrefix(path, rootPrefix)
			if _, exempt := unscannedModuleFiles[relative]; exempt {
				seen[relative] = true
				continue
			}
			t.Errorf("%s is compiled on none of %v, so its environment reads are unread: add the platform it is built for to scannedPlatforms, or list the file in unscannedModuleFiles with the reason the binary never runs it", relative, scannedPlatforms)
		}
	}
	for _, relative := range sortedKeys(unscannedModuleFiles) {
		if strings.TrimSpace(unscannedModuleFiles[relative]) == "" {
			t.Errorf("the exemption for %s has no reason", relative)
		}
		if !seen[relative] {
			t.Errorf("the exemption for %s is not needed: the file is compiled on a scanned platform or is not in a linked package, drop the entry", relative)
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

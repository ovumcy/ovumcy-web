package api

import (
	"bytes"
	"compress/gzip"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// authInputKeys are the request members that are credentials, auth flags or
// security tokens. None of them may be read from the URL query string: a link
// is logged, cached, shared and prefetched, and a value planted in one must not
// stand in for — or shadow — what the request body carries.
var authInputKeys = map[string]bool{
	"password":         true,
	"code":             true,
	"remember_me":      true,
	"email":            true,
	"csrf_token":       true,
	"recovery_code":    true,
	"current_password": true,
	"new_password":     true,
	"confirm_password": true,
	"consent":          true,
}

// queryReadingLookups are the request-level lookups that consult the URL (its
// query string, its path parameters), alone or together with the body. Each
// takes the member name as its first argument as a method (c.Query("code")),
// and as its second as fiber's generic function (fiber.Query[string](c, "code")).
var queryReadingLookups = map[string]bool{"FormValue": true, "Query": true, "Params": true}

// wholeQueryReaders return the entire query string or every query member at
// once, so no key can clear them: nothing in the scanned source uses one, and a
// credential rides through them whatever it is named.
var wholeQueryReaders = map[string]bool{"Queries": true, "QueryString": true}

// queryArgsReaders are the methods of the request URI's query-argument set
// that take a member name.
var queryArgsReaders = map[string]bool{
	"Peek": true, "PeekBytes": true, "PeekMulti": true, "Has": true, "HasBytes": true,
	"GetBool": true, "GetUfloat": true, "GetUint": true, "GetUfloatOrZero": true, "GetUintOrZero": true,
}

// queryRead is one place a member is read from a source that includes the URL
// query string.
type queryRead struct {
	at   token.Pos
	key  string
	how  string
	bulk bool
	note string
	// unresolvedKey marks a lookup whose member name is neither a string literal
	// nor a package constant: the only reads an exemption can clear.
	unresolvedKey bool
	keyArg        ast.Expr
}

func isIdentNamed(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

// lookupExemption names one production function allowed to look a member up
// under a name the guard cannot resolve, because that name is its keyParam.
// Exempting the lookup moves the guard to the function's callers:
// TestExemptLookupCallersPassOnlyDeclaredKeys resolves the function by
// declaration and requires every caller to pass a constant from keys.
type lookupExemption struct {
	file     string // path under the repository root
	receiver string // named receiver type; "" for a plain function
	function string
	keyParam string
	keys     []string
}

func (exemption lookupExemption) declKey() string {
	return exemption.file + " " + exemption.receiver + "." + exemption.function
}

// declKey is the same key for a parsed declaration, so the AST sweep and the
// type-checked caller test name one object.
func declKey(path string, fn *ast.FuncDecl) string {
	receiver := ""
	if fn.Recv != nil && len(fn.Recv.List) == 1 {
		typ := fn.Recv.List[0].Type
		if star, ok := typ.(*ast.StarExpr); ok {
			typ = star.X
		}
		if ident, ok := typ.(*ast.Ident); ok {
			receiver = ident.Name
		}
	}
	return path + " " + receiver + "." + fn.Name.Name
}

// unresolvedKeyExemptions: oidcCallbackValue reads the provider's callback
// parameters from the URL under OIDC_RESPONSE_MODE=query. They are
// provider-originated, not user credentials, and the sealed one-time state
// cookie is what authorises the exchange. The exemption clears only a lookup
// inside that function whose key is keyParam itself — an auth member read by name
// there is still refused — and the sweep fails when an entry no longer clears
// anything, so it cannot outlive the code it excuses.
var unresolvedKeyExemptions = []lookupExemption{{
	file:     "internal/api/oidc_helpers.go",
	receiver: "Handler",
	function: "oidcCallbackValue",
	keyParam: "name",
	keys:     []string{"code", "state", "error"},
}}

// calleeSelector returns the selector a call goes through, looking past the
// type-argument list of a generic call: fiber.Query[string](c, "code") has an
// index expression, not a selector, as its Fun.
func calleeSelector(call *ast.CallExpr) (selector *ast.SelectorExpr, generic bool) {
	fun := call.Fun
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun, generic = f.X, true
	case *ast.IndexListExpr:
		fun, generic = f.X, true
	}
	selector, _ = fun.(*ast.SelectorExpr)
	return selector, generic
}

// receiverChainHasCall reports whether a call to name sits anywhere in the
// receiver chain of expr, so c.Bind().WithoutAutoHandling().Query(&in) is seen
// as reaching Bind() however many modifiers sit in between.
func receiverChainHasCall(expr ast.Expr, name string) bool {
	for {
		switch e := expr.(type) {
		case *ast.CallExpr:
			selector, _ := calleeSelector(e)
			if selector == nil {
				return false
			}
			if selector.Sel.Name == name {
				return true
			}
			expr = selector.X
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		default:
			return false
		}
	}
}

// findQueryReads returns every read in node that takes an auth member from the
// URL. Key names resolve through consts, so a key spelled through a
// package-level constant is judged like the literal; a key that resolves to
// neither is reported as untraceable, as a wrapper such as
// func field(c fiber.Ctx, name string) string { return c.FormValue(name) }
// would otherwise hand a password past the guard. Reads that name no
// member are reported whatever the struct or key, because they hand back a
// password as readily as anything else: a Query or All bind whose receiver chain
// contains Bind() (Bind() itself is refused when held in a variable, where its
// later calls cannot be traced), Queries(), QueryString(), and a QueryArgs()
// that is not used as the direct receiver of one member read with a key that is
// not an auth member (held in a variable, ranged over, passed on).
func findQueryReads(node ast.Node, consts map[string]string) []queryRead {
	var reads []queryRead
	var stack []ast.Node
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		ancestors := stack
		stack = append(stack, n)

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, generic := calleeSelector(call)
		if selector == nil {
			return true
		}
		name := selector.Sel.Name

		switch {
		case !generic && (name == "Query" || name == "All") && receiverChainHasCall(selector.X, "Bind"):
			reads = append(reads, queryRead{at: call.Pos(), how: "Bind()..." + name, bulk: true,
				note: "fills every field of its target, credentials included"})
			return true
		case !generic && name == "Bind" && len(call.Args) == 0 && !isDirectReceiver(ancestors, call):
			reads = append(reads, queryRead{at: call.Pos(), how: "Bind() held outside a call chain", bulk: true,
				note: "its Query and All cannot be traced from here"})
			return true
		case wholeQueryReaders[name]:
			reads = append(reads, queryRead{at: call.Pos(), how: name + "()", bulk: true,
				note: "returns every query member, credentials included"})
			return true
		case !generic && name == "QueryArgs":
			if read, flagged := queryArgsRead(ancestors, call, consts); flagged {
				reads = append(reads, read)
			}
			return true
		case queryReadingLookups[name]:
			// fiber's generic lookups take the request first and the member
			// second; the method forms take the member first.
			// fiber.Query(c, "day", 0) infers its type argument and has no index
			// expression, but it is the same generic function.
			if !generic && isFiberPackage(selector.X) {
				generic = true
			}
			index := 0
			if generic {
				index = 1
			}
			how := name
			if generic {
				how = "generic " + name
			}
			key, resolved := memberKey(call, index, consts)
			switch {
			case resolved && authInputKeys[key]:
				reads = append(reads, queryRead{at: call.Pos(), key: key, how: how})
			case !resolved && len(call.Args) > index:
				reads = append(reads, queryRead{at: call.Pos(), how: how, bulk: true, unresolvedKey: true, keyArg: call.Args[index],
					note: "its member name is not a literal or package constant, so a credential can be read through it unseen"})
			}
		}
		return true
	})
	return reads
}

// isFiberPackage reports whether expr is the identifier of the fiber package.
func isFiberPackage(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "fiber"
}

// isDirectReceiver reports whether call is the receiver of a selector that is
// itself called, given the ancestors that lead to it: c.Bind().Body(..) yes,
// b := c.Bind() no.
func isDirectReceiver(ancestors []ast.Node, call *ast.CallExpr) bool {
	if len(ancestors) == 0 {
		return false
	}
	parent, ok := ancestors[len(ancestors)-1].(*ast.SelectorExpr)
	return ok && parent.X == call
}

// queryArgsRead judges a QueryArgs() call. It is cleared only as the direct
// receiver of a member read whose key is a known non-auth member; a read of an
// auth member names it, and any other use is reported as an untraceable one.
func queryArgsRead(ancestors []ast.Node, call *ast.CallExpr, consts map[string]string) (queryRead, bool) {
	if isDirectReceiver(ancestors, call) && len(ancestors) >= 2 {
		parent, _ := ancestors[len(ancestors)-1].(*ast.SelectorExpr)
		if outer, ok := ancestors[len(ancestors)-2].(*ast.CallExpr); ok && outer.Fun == parent && queryArgsReaders[parent.Sel.Name] {
			if key, ok := memberKey(outer, 0, consts); ok {
				if authInputKeys[key] {
					return queryRead{at: outer.Pos(), key: key, how: "QueryArgs()." + parent.Sel.Name}, true
				}
				return queryRead{}, false
			}
		}
	}
	return queryRead{at: call.Pos(), how: "QueryArgs() outside a direct member read", bulk: true,
		note: "the member it is asked for cannot be traced from here"}, true
}

// memberKey resolves the argument at index of a lookup to a string: a literal,
// or an identifier naming a package-level string constant.
func memberKey(call *ast.CallExpr, index int, consts map[string]string) (string, bool) {
	if len(call.Args) <= index {
		return "", false
	}
	switch arg := call.Args[index].(type) {
	case *ast.BasicLit:
		if arg.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(arg.Value)
		return value, err == nil
	case *ast.Ident:
		value, ok := consts[arg.Name]
		return value, ok
	}
	return "", false
}

func packageStringConsts(files []*ast.File) map[string]string {
	consts := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok || len(valueSpec.Names) != len(valueSpec.Values) {
					continue
				}
				for i, value := range valueSpec.Values {
					lit, ok := value.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					if unquoted, err := strconv.Unquote(lit.Value); err == nil {
						consts[valueSpec.Names[i].Name] = unquoted
					}
				}
			}
		}
	}
	return consts
}

// TestAuthFieldsAreNeverReadFromTheQueryString derives every read of an auth
// member from the production source and refuses any that reaches the URL.
// fiber's FormValue searches the query BEFORE the body, so a `?remember_me=1`
// outranked the body's own value; the class is closed here at every site — a
// handler added later is judged by the same rule, and reads its input through
// bindRequestBody. The one exemption is unresolvedKeyExemptions: a single named
// function may look a member up under a name the guard cannot resolve, because
// a lookup it cannot resolve is otherwise refused as untraceable.
//
// Scope: every non-test Go file under internal/ and cmd/, not only this
// package, each package judged with its own constants. Nothing outside
// internal/api reads the request today; the wider net costs one directory walk.
func TestAuthFieldsAreNeverReadFromTheQueryString(t *testing.T) {
	assertQueryReadSweepAnswersBothWays(t)

	fileSet := token.NewFileSet()
	filesByDir := map[string][]*ast.File{}
	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			parsed, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				return err
			}
			dir := filepath.ToSlash(filepath.Dir(path))
			filesByDir[dir] = append(filesByDir[dir], parsed)
			return nil
		})
		if err != nil {
			t.Fatalf("parse production source under %s: %v", root, err)
		}
	}
	if len(filesByDir["../../internal/api"]) == 0 {
		t.Fatal("no internal/api source parsed: the sweep is measuring nothing")
	}

	// The sweep must see the lookups it polices: the code reads plenty of
	// non-auth members through them, so a scan that found none has stopped
	// recognising calls.
	seenLookups := 0
	exemptKeyParam := map[string]string{}
	for _, exemption := range unresolvedKeyExemptions {
		exemptKeyParam[exemption.declKey()] = exemption.keyParam
	}
	exemptionsUsed := map[string]bool{}
	var violations []string
	for _, files := range filesByDir {
		consts := packageStringConsts(files)
		for _, file := range files {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if selector, generic := calleeSelector(call); selector != nil && !generic && queryReadingLookups[selector.Sel.Name] {
					if _, ok := memberKey(call, 0, consts); ok {
						seenLookups++
					}
				}
				return true
			})
			path := strings.TrimPrefix(filepath.ToSlash(fileSet.Position(file.Pos()).Filename), "../../")
			for _, decl := range file.Decls {
				exemption := ""
				if fn, ok := decl.(*ast.FuncDecl); ok {
					exemption = declKey(path, fn)
				}
				for _, read := range findQueryReads(decl, consts) {
					if param, ok := exemptKeyParam[exemption]; ok && read.unresolvedKey && isIdentNamed(read.keyArg, param) {
						exemptionsUsed[exemption] = true
						continue
					}
					what := read.how + "(" + strconv.Quote(read.key) + ")"
					if read.bulk {
						what = read.how + " (" + read.note + ")"
					}
					violations = append(violations, fileSet.Position(read.at).String()+": "+what)
				}
			}
		}
	}
	if seenLookups == 0 {
		t.Fatal("the sweep found no FormValue/Query lookup at all: it is not reading the code it claims to police")
	}
	for _, exemption := range unresolvedKeyExemptions {
		if !exemptionsUsed[exemption.declKey()] {
			t.Errorf("unresolvedKeyExemptions names %q, which no longer holds a lookup with an unresolvable key: remove the entry", exemption.declKey())
		}
	}
	sort.Strings(violations)
	for _, violation := range violations {
		t.Errorf("%s reads an auth input from a source that includes the URL — read it from the body with bindRequestBody, which never consults it", violation)
	}
}

// assertQueryReadSweepAnswersBothWays anchors the sweep on fixtures it owns: one
// body per read shape that must be flagged and bodies that must not, so a
// finder that stopped recognising a shape cannot report success over a package
// it no longer understands.
func assertQueryReadSweepAnswersBothWays(t *testing.T) {
	t.Helper()

	flagged := map[string]string{
		`_ = c.FormValue("password")`:                                       "FormValue",
		`_ = c.Query("code")`:                                               "Query",
		`_ = c.Request().URI().QueryArgs().Peek("email")`:                   "QueryArgs().Peek",
		`_ = c.Request().URI().QueryArgs().Has("remember_me")`:              "QueryArgs().Has",
		`_ = c.Bind().Query(&in)`:                                           "Bind()...Query",
		`_ = c.Bind().All(&in)`:                                             "Bind()...All",
		`_ = c.Bind().WithoutAutoHandling().Query(&in)`:                     "Bind()...Query",
		`_ = c.Bind().SkipValidation(true).All(&in)`:                        "Bind()...All",
		`b := c.Bind(); _ = b.Query(&in)`:                                   "Bind() held",
		`_ = fiber.Query[string](c, "password")`:                            "generic Query",
		`_ = fiber.Query[string](c, csrfFieldName)`:                         "generic Query",
		`_ = fiber.Params[string](c, "code")`:                               "generic Params",
		`_ = c.Params("password")`:                                          "Params",
		`_ = c.Queries()["code"]`:                                           "Queries()",
		`_ = c.Req().Queries()`:                                             "Queries()",
		`_ = c.Request().URI().QueryString()`:                               "QueryString()",
		`args := c.Request().URI().QueryArgs(); _ = args.Peek("password")`:  "QueryArgs() outside",
		`c.Request().URI().QueryArgs().VisitAll(func(k, v []byte) {})`:      "QueryArgs() outside",
		`_ = c.Request().URI().QueryArgs().Peek(name)`:                      "QueryArgs() outside",
		`_ = c.FormValue(csrfFieldName)`:                                    "FormValue",
		`_ = c.FormValue("csrf_token")`:                                     "FormValue",
		`_ = strings.TrimSpace(c.FormValue("consent"))`:                     "FormValue",
		`_ = c.Request().URI().QueryArgs().Peek("recovery_code")`:           "QueryArgs().Peek",
		`_ = c.FormValue("current_password")`:                               "FormValue",
		`_ = c.FormValue("new_password") + c.FormValue("confirm_password")`: "FormValue",
		`name := "password"; _ = c.FormValue(name)`:                         "FormValue",
		`field := func(c fiber.Ctx, name string) string { return c.FormValue(name) }; _ = field(c, "password")`: "FormValue",
		`_ = c.Query("co" + "de")`:           "Query",
		`_ = c.Params(lookupName())`:         "Params",
		`_ = fiber.Query[string](c, name)`:   "generic Query",
		`_ = fiber.Query(c, "password", "")`: "generic Query",
	}
	consts := map[string]string{"csrfFieldName": "csrf_token"}
	for source, wantHow := range flagged {
		reads := findQueryReads(parseFunctionBodyForTest(t, source), consts)
		switch {
		case len(reads) == 0:
			t.Errorf("the sweep must flag %s", source)
		case !strings.HasPrefix(reads[0].how, wantHow):
			t.Errorf("the sweep flagged %s as %s, want %s", source, reads[0].how, wantHow)
		}
	}

	for _, source := range []string{
		`_ = c.FormValue("lang")`,
		`_ = c.Query("day")`,
		`_ = c.Request().URI().QueryArgs().Has("age_group")`,
		`_ = c.Request().PostArgs().Peek("password")`,
		`_ = bindRequestBody(c, &in)`,
		`_ = logoutURL.Query()`,
		`_ = logoutURL.Query().Get("password")`,
		`_ = fiber.Query[string](c, "day")`,
		`_ = fiber.Query(c, "day", 0)`,
		`_ = fiber.Params[string](c, "id")`,
		`_ = c.Params("id")`,
		`_ = c.Bind().Body(&in)`,
		`_ = c.Bind().WithoutAutoHandling().Body(&in)`,
		`_ = c.Bind().Header(&in)`,
	} {
		if reads := findQueryReads(parseFunctionBodyForTest(t, source), consts); len(reads) != 0 {
			t.Fatalf("the sweep must not flag %s, got %v", source, reads)
		}
	}
}

func isUnprocessable(err error) bool {
	var fiberErr *fiber.Error
	return errors.As(err, &fiberErr) && fiberErr.Code == fiber.StatusUnprocessableEntity
}

// TestBindRequestBodyReadsOnlyTheBody drives the helper through every body
// transport with a conflicting `?field=` in the query: the body's value wins
// where the transport has one, and the query's value is never the answer. A
// body type the helper does not accept for auth inputs (XML, CBOR, MsgPack, a
// vendor "+json") is refused with 422 and fills nothing, even where the binder
// underneath would have decoded it.
func TestBindRequestBodyReadsOnlyTheBody(t *testing.T) {
	type target struct {
		Field string `json:"field" form:"field"`
	}

	gzipped := func(payload string) []byte {
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		if _, err := writer.Write([]byte(payload)); err != nil {
			t.Fatalf("gzip: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
		return buffer.Bytes()
	}
	multipartBody := func() ([]byte, string) {
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		if err := writer.WriteField("field", "from-body"); err != nil {
			t.Fatalf("multipart field: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("multipart close: %v", err)
		}
		return buffer.Bytes(), writer.FormDataContentType()
	}
	multipartBytes, multipartType := multipartBody()

	cases := []struct {
		name        string
		contentType string
		encoding    string
		body        []byte
		want        string
		wantErr     func(error) bool
	}{
		{name: "json", contentType: "application/json", body: []byte(`{"field":"from-body"}`), want: "from-body"},
		{name: "json without the member", contentType: "application/json", body: []byte(`{}`), want: ""},
		{name: "urlencoded", contentType: "application/x-www-form-urlencoded", body: []byte("field=from-body"), want: "from-body"},
		{name: "urlencoded without the member", contentType: "application/x-www-form-urlencoded", body: []byte("other=1"), want: ""},
		{name: "multipart", contentType: multipartType, body: multipartBytes, want: "from-body"},
		{name: "mixed-case form content type", contentType: "Application/X-WWW-Form-Urlencoded", body: []byte("field=from-body"), want: "from-body"},
		{name: "mixed-case form content type without the member", contentType: "Application/X-WWW-Form-Urlencoded", body: []byte("other=1"), want: ""},
		{name: "gzip json", contentType: "application/json", encoding: "gzip", body: gzipped(`{"field":"from-body"}`), want: "from-body"},
		{name: "json with parameters", contentType: "application/json; charset=utf-8", body: []byte(`{"field":"from-body"}`), want: "from-body"},
		{name: "mixed-case json content type", contentType: "Application/JSON", body: []byte(`{"field":"from-body"}`), want: "from-body"},
		{name: "unknown content type", contentType: "text/plain", body: []byte("field=from-body"), wantErr: isUnprocessable},
		{name: "no content type", contentType: "", body: []byte("field=from-body"), wantErr: isUnprocessable},
		// The binder decodes these; the helper accepts none of them for an auth
		// input, and an XML decoder keeps what it read before a syntax error.
		{name: "application/xml", contentType: "application/xml", body: []byte(`<target><Field>from-body</Field></target>`), wantErr: isUnprocessable},
		{name: "text/xml", contentType: "text/xml", body: []byte(`<target><Field>from-body</Field></target>`), wantErr: isUnprocessable},
		{name: "mixed-case xml with parameters", contentType: "Application/XML; charset=utf-8", body: []byte(`<target><Field>from-body</Field></target>`), wantErr: isUnprocessable},
		{name: "partial xml", contentType: "application/xml", body: []byte(`<target><Field>from-body</Field><broken>`), wantErr: isUnprocessable},
		{name: "vendor json", contentType: "application/vnd.api+json", body: []byte(`{"field":"from-body"}`), wantErr: isUnprocessable},
		{name: "cbor", contentType: "application/cbor", body: []byte("\xa1efield\x69from-body"), wantErr: isUnprocessable},
		{name: "msgpack", contentType: "application/msgpack", body: []byte("\x81\xa5field\xa9from-body"), wantErr: isUnprocessable},
		{name: "malformed json", contentType: "application/json", body: []byte(`{"field":`), wantErr: func(err error) bool { return err != nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			var got target
			var bindErr error
			app.Post("/bind", func(c fiber.Ctx) error {
				bindErr = bindRequestBody(c, &got)
				return c.SendStatus(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodPost, "/bind?"+url.Values{"field": {"from-query"}}.Encode(), bytes.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.contentType)
			if tc.encoding != "" {
				request.Header.Set("Content-Encoding", tc.encoding)
			}
			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			_ = response.Body.Close()

			if tc.wantErr != nil {
				if !tc.wantErr(bindErr) {
					t.Fatalf("bind error = %v, want the refusal the case names", bindErr)
				}
				if got.Field != "" {
					t.Fatalf("Field = %q after a refusal, want nothing filled (the query carried %q)", got.Field, "from-query")
				}
				return
			}
			if bindErr != nil {
				t.Fatalf("bind error = %v", bindErr)
			}
			if got.Field != tc.want {
				t.Fatalf("Field = %q, want %q (the query carried %q)", got.Field, tc.want, "from-query")
			}
		})
	}
}

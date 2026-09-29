package api

import (
	"bytes"
	"compress/gzip"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// queryReadingLookups are the request-level lookups that consult the query
// string, alone or together with the body. Each takes the member name as its
// first argument.
var queryReadingLookups = map[string]bool{"FormValue": true, "Query": true}

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
}

// findQueryReads returns every read in node that takes an auth member from the
// query string. Key names resolve through consts, so a key spelled through a
// package-level constant is judged like the literal. Bulk binders
// (Bind().Query, Bind().All) name no member: they fill every field of the
// struct they are handed, a password included, so they are reported whatever
// the struct.
func findQueryReads(node ast.Node, consts map[string]string) []queryRead {
	var reads []queryRead
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := selector.Sel.Name

		if inner, ok := selector.X.(*ast.CallExpr); ok {
			if innerSel, ok := inner.Fun.(*ast.SelectorExpr); ok {
				switch {
				case innerSel.Sel.Name == "Bind" && (name == "Query" || name == "All"):
					reads = append(reads, queryRead{at: call.Pos(), how: "Bind()." + name, bulk: true})
					return true
				case innerSel.Sel.Name == "QueryArgs" && queryArgsReaders[name]:
					if key, ok := memberKey(call, consts); ok && authInputKeys[key] {
						reads = append(reads, queryRead{at: call.Pos(), key: key, how: "QueryArgs()." + name})
					}
					return true
				}
			}
		}

		if queryReadingLookups[name] {
			if key, ok := memberKey(call, consts); ok && authInputKeys[key] {
				reads = append(reads, queryRead{at: call.Pos(), key: key, how: name})
			}
		}
		return true
	})
	return reads
}

// memberKey resolves the first argument of a lookup to a string: a literal, or
// an identifier naming a package-level string constant.
func memberKey(call *ast.CallExpr, consts map[string]string) (string, bool) {
	if len(call.Args) == 0 {
		return "", false
	}
	switch arg := call.Args[0].(type) {
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
// member from the package source and refuses any that reaches the URL query
// string. fiber's FormValue searches the query BEFORE the body, so a
// `?remember_me=1` outranked the body's own value; the class is closed here at
// every site, with no exemption list to keep current — a handler added later is
// judged by the same rule, and reads its input through bindRequestBody.
func TestAuthFieldsAreNeverReadFromTheQueryString(t *testing.T) {
	assertQueryReadSweepAnswersBothWays(t)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fileSet := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, parsed)
	}
	if len(files) == 0 {
		t.Fatal("no production source parsed: the sweep is measuring nothing")
	}
	consts := packageStringConsts(files)

	// The sweep must see the lookups it polices: the package reads plenty of
	// non-auth members through them, so a scan that found none has stopped
	// recognising calls.
	seenLookups := 0
	var violations []string
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && queryReadingLookups[selector.Sel.Name] {
				if _, ok := memberKey(call, consts); ok {
					seenLookups++
				}
			}
			return true
		})
		for _, read := range findQueryReads(file, consts) {
			what := read.how + "(" + strconv.Quote(read.key) + ")"
			if read.bulk {
				what = read.how + " (fills every field of its target, credentials included)"
			}
			violations = append(violations, fileSet.Position(read.at).String()+": "+what)
		}
	}
	if seenLookups == 0 {
		t.Fatal("the sweep found no FormValue/Query lookup at all: it is not reading the package it claims to police")
	}
	sort.Strings(violations)
	for _, violation := range violations {
		t.Errorf("%s reads an auth input from a source that includes the URL query string — read it from the body with bindRequestBody, which never consults the query", violation)
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
		`_ = c.Bind().Query(&in)`:                                           "Bind().Query",
		`_ = c.Bind().All(&in)`:                                             "Bind().All",
		`_ = c.FormValue(csrfFieldName)`:                                    "FormValue",
		`_ = c.FormValue("csrf_token")`:                                     "FormValue",
		`_ = strings.TrimSpace(c.FormValue("consent"))`:                     "FormValue",
		`_ = c.Request().URI().QueryArgs().Peek("recovery_code")`:           "QueryArgs().Peek",
		`_ = c.FormValue("current_password")`:                               "FormValue",
		`_ = c.FormValue("new_password") + c.FormValue("confirm_password")`: "FormValue",
	}
	consts := map[string]string{"csrfFieldName": "csrf_token"}
	for source, wantHow := range flagged {
		reads := findQueryReads(parseFunctionBodyForTest(t, source), consts)
		if len(reads) == 0 {
			t.Fatalf("the sweep must flag %s", source)
		}
		if !strings.HasPrefix(reads[0].how, wantHow) {
			t.Fatalf("the sweep flagged %s as %s, want %s", source, reads[0].how, wantHow)
		}
	}

	for _, source := range []string{
		`_ = c.FormValue("lang")`,
		`_ = c.Query("day")`,
		`_ = c.Request().URI().QueryArgs().Has("age_group")`,
		`_ = c.Request().PostArgs().Peek("password")`,
		`_ = bindRequestBody(c, &in)`,
		`_ = logoutURL.Query()`,
	} {
		if reads := findQueryReads(parseFunctionBodyForTest(t, source), consts); len(reads) != 0 {
			t.Fatalf("the sweep must not flag %s, got %v", source, reads)
		}
	}
}

// TestBindRequestBodyReadsOnlyTheBody drives the helper through every body
// transport with a conflicting `?field=` in the query: the body's value wins
// where the transport has one, and the query's value is never the answer.
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
		{
			name: "unknown content type", contentType: "text/plain", body: []byte("field=from-body"),
			wantErr: func(err error) bool {
				var fiberErr *fiber.Error
				return errors.As(err, &fiberErr) && fiberErr.Code == fiber.StatusUnprocessableEntity
			},
		},
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
				if got.Field == "from-query" {
					t.Fatal("the query value was bound although the body could not be")
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

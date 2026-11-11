package services

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/testpackages"
	"golang.org/x/tools/go/packages"
)

const outboundFixtureFile = "zz_outbound_client_fixture.go"

// outboundClientFiles are the only non-test files under cmd/ and internal/
// that build or drive a client able to reach another host. SECURITY.md names
// two standing egress paths to a third party; the webhook is the one this
// side opens (the calendar feed is polled by the client, not dialled out).
var outboundClientFiles = map[string]string{
	"internal/services/webhook_delivery.go": "the owner's webhook reminders",
	"internal/security/oidc.go":             "the configured identity provider: discovery, code exchange, userinfo; identity claims, no health data",
	"internal/cli/healthcheck.go":           "the container health probe, dialled to loopback",
}

// outboundClientObjects are the declarations that open a connection or carry
// a request to another host, by package path. A type listed here covers its
// methods too, so a client received without its type ever being spelled is
// still found where its Do or Dial is called. A nil set means the whole
// package.
var outboundClientObjects = map[string]map[string]bool{
	"net/http": {
		"Client": true, "Transport": true, "DefaultClient": true, "DefaultTransport": true,
		"Get": true, "Post": true, "Head": true, "PostForm": true,
		"NewRequest": true, "NewRequestWithContext": true,
	},
	"net": {
		"Dial": true, "DialTimeout": true, "DialTCP": true, "DialUDP": true, "DialIP": true, "DialUnix": true,
		"Dialer": true, "Resolver": true, "DefaultResolver": true,
		"LookupHost": true, "LookupIP": true, "LookupAddr": true, "LookupCNAME": true, "LookupMX": true,
		"LookupNS": true, "LookupTXT": true, "LookupSRV": true, "LookupPort": true, "LookupNetIP": true,
	},
	"crypto/tls":                         {"Dial": true, "DialWithDialer": true, "Dialer": true},
	"net/smtp":                           nil,
	"github.com/gofiber/fiber/v3/client": nil,
	"github.com/valyala/fasthttp": {
		"Client": true, "HostClient": true, "PipelineClient": true, "LBClient": true, "TCPDialer": true,
		"Do": true, "DoTimeout": true, "DoDeadline": true, "DoRedirects": true,
		"Get": true, "GetTimeout": true, "GetDeadline": true, "Post": true,
		"Dial": true, "DialTimeout": true, "DialDualStack": true, "DialDualStackTimeout": true,
	},
	"golang.org/x/oauth2":               {"HTTPClient": true, "NewClient": true, "Config": true, "Transport": true},
	"github.com/coreos/go-oidc/v3/oidc": {"NewProvider": true, "ClientContext": true, "NewRemoteKeySet": true, "Provider": true, "RemoteKeySet": true},
}

// isOutboundClientObject reports whether object is one of outboundClientObjects
// or a method of a type listed there.
func isOutboundClientObject(object types.Object) bool {
	if object == nil || object.Pkg() == nil {
		return false
	}
	names, listed := outboundClientObjects[object.Pkg().Path()]
	if !listed {
		return false
	}
	if names == nil {
		return true
	}
	if function, ok := object.(*types.Func); ok && function.Signature().Recv() != nil {
		receiver := function.Signature().Recv().Type()
		if pointer, ok := receiver.(*types.Pointer); ok {
			receiver = pointer.Elem()
		}
		named, ok := receiver.(*types.Named)
		return ok && names[named.Obj().Name()]
	}
	return names[object.Name()]
}

type outboundClientUse struct {
	file      string
	at        string
	object    string
	enclosing string
}

// findOutboundClientUses returns every identifier in the loaded syntax that
// resolves to an outbound client declaration, with the top-level declaration
// it sits in.
func findOutboundClientUses(loaded []*packages.Package) []outboundClientUse {
	var uses []outboundClientUse
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			path := filepath.Clean(pkg.Fset.Position(file.Package).Filename)
			for _, declaration := range file.Decls {
				enclosing := "a package-level declaration"
				if function, ok := declaration.(*ast.FuncDecl); ok {
					enclosing = function.Name.Name
				}
				ast.Inspect(declaration, func(node ast.Node) bool {
					ident, ok := node.(*ast.Ident)
					if !ok {
						return true
					}
					if object := pkg.TypesInfo.Uses[ident]; isOutboundClientObject(object) {
						uses = append(uses, outboundClientUse{
							file:      path,
							at:        pkg.Fset.Position(ident.Pos()).String(),
							object:    object.Pkg().Path() + "." + object.Name(),
							enclosing: enclosing,
						})
					}
					return true
				})
			}
		}
	}
	return uses
}

// TestOutboundNetworkClientsAreAClosedSet holds SECURITY.md's egress claim on
// the side the code controls: every non-test file under cmd/ and internal/
// that builds or drives an outbound client is named above with its reason, and
// every named file still does. A new one means a new path out of the
// instance, which the policy has to name before this list does.
func TestOutboundNetworkClientsAreAClosedSet(t *testing.T) {
	if testpackages.Isolate(t) {
		return
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	found := map[string]string{}
	for _, use := range findOutboundClientUses(withoutEmailGuardFixtures(loadEmailGuardTree(t))) {
		relative, err := filepath.Rel(root, use.file)
		if err != nil {
			t.Fatalf("relativize %s: %v", use.file, err)
		}
		relative = filepath.ToSlash(relative)
		if !strings.HasPrefix(relative, "cmd/") && !strings.HasPrefix(relative, "internal/") {
			continue
		}
		if _, seen := found[relative]; !seen {
			found[relative] = use.object + " at " + use.at
		}
	}
	for _, file := range sortedKeys(found) {
		if _, ok := outboundClientFiles[file]; !ok {
			t.Errorf("%s builds or drives an outbound client (%s) and is not a named egress path: name it in SECURITY.md and here, or reach the network through an existing one", file, found[file])
		}
	}
	for _, file := range sortedKeys(outboundClientFiles) {
		if _, ok := found[file]; !ok {
			t.Errorf("named egress file %s no longer builds or drives an outbound client: drop the entry", file)
		}
	}
}

// TestOutboundClientScanClassifiesItsOwnFixtures proves the detector on each
// shape it claims, overlaid into internal/services and type-checked there, and
// on a bystander that only uses net/http's and net's constants and parsers.
func TestOutboundClientScanClassifiesItsOwnFixtures(t *testing.T) {
	if testpackages.Isolate(t) {
		return
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	fixture := filepath.Join(root, "internal", "services", outboundFixtureFile)
	loaded, err := testpackages.Load(&packages.Config{
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:     root,
		Overlay: map[string][]byte{fixture: []byte(outboundClientFixtureSource)},
	}, "./internal/services")
	if err != nil {
		t.Fatalf("load the fixture package: %v", err)
	}
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			t.Fatalf("%s does not type-check: %v", pkg.PkgPath, packageError)
		}
	}

	found := map[string]bool{}
	for _, use := range findOutboundClientUses(loaded) {
		if use.file == fixture {
			found[use.enclosing] = true
		}
	}
	for _, name := range []string{
		"outboundFixturePackageGet", "outboundFixtureClientLiteral", "outboundFixtureDefaultClient",
		"outboundFixtureTransportLiteral", "outboundFixtureDialer", "outboundFixtureDial",
		"outboundFixtureResolver", "outboundFixtureSMTP", "outboundFixtureOAuthClient",
		"outboundFixtureOIDCProvider", "outboundFixtureFasthttp", "outboundFixtureClientMethodOnly",
		"outboundFixtureTLSDial",
	} {
		if !found[name] {
			t.Errorf("the scan missed %s (found %v)", name, sortedKeys(found))
		}
	}
	if found["outboundFixtureBystander"] {
		t.Error("outboundFixtureBystander only uses status codes, method names, ParseIP and GenerateVerifier; it reaches no host and must not count")
	}
}

const outboundClientFixtureSource = `package services

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/smtp"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/valyala/fasthttp"
	"golang.org/x/oauth2"
)

func outboundFixturePackageGet() { _, _ = http.Get("https://example.invalid") }

func outboundFixtureClientLiteral() any { return &http.Client{} }

func outboundFixtureDefaultClient() any { return http.DefaultClient }

func outboundFixtureTransportLiteral() any { return &http.Transport{} }

func outboundFixtureDialer() any { return &net.Dialer{} }

func outboundFixtureDial() { _, _ = net.Dial("tcp", "example.invalid:80") }

func outboundFixtureResolver(ctx context.Context) { _, _ = net.DefaultResolver.LookupHost(ctx, "example.invalid") }

func outboundFixtureSMTP() { _ = smtp.SendMail("example.invalid:25", nil, "", nil, nil) }

func outboundFixtureOAuthClient(ctx context.Context) any { return oauth2.NewClient(ctx, nil) }

func outboundFixtureOIDCProvider(ctx context.Context) { _, _ = oidc.NewProvider(ctx, "https://example.invalid") }

func outboundFixtureFasthttp() { _, _, _ = fasthttp.Get(nil, "https://example.invalid") }

func outboundFixtureTLSDial() { _, _ = tls.Dial("tcp", "example.invalid:443", nil) }

func outboundFixtureClientSource() *http.Client { return nil }

func outboundFixtureClientMethodOnly() {
	client := outboundFixtureClientSource()
	_, _ = client.Do(nil)
}

func outboundFixtureBystander() (int, string, net.IP, string) {
	return http.StatusOK, http.MethodPost, net.ParseIP("127.0.0.1"), oauth2.GenerateVerifier()
}
`

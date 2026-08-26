package staticassets

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/png" // registers PNG with image.Decode, which names the format it read
	"path"
	"strconv"
	"strings"
	"testing"
)

// `purpose: maskable` is a promise to the install surface, not a filename
// convention: the platform composites the icon under a mask of its own choosing
// — circle, squircle, rounded square, teardrop — and paints nothing behind it.
// Anything the artwork leaves transparent therefore shows through as a hole or a
// corner gap, and an icon that carries its own rounded corners gets rounded
// twice.
//
// The safe zone this file asserts against is the platform convention: the centre
// circle at 80% of the icon's width (radius 0.4w). Inside it the artwork is
// always visible whatever mask is applied; outside it is background that a mask
// may crop into or expose, so outside it the asset must be opaque. The guard
// deliberately says nothing about the pixels INSIDE the safe circle — that is
// artwork, and judging it would fire on a correctly authored future icon that
// happens to place a shape differently.
const maskableSafeZoneFraction = 0.8

// manifestPath is where the web app manifest sits inside the embed FS. Icon
// `src` values are resolved against it by resolveEmbeddedIconPath, exactly as a
// browser resolves them against the manifest's own URL.
const manifestPath = "static/manifest.webmanifest"

type manifestIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

type webManifest struct {
	Icons []manifestIcon `json:"icons"`
}

// hasPurpose reports whether the icon declares the given purpose. `purpose` is a
// space-separated list per the manifest spec ("any maskable" is legal), so a
// substring match would also fire on a hypothetical "maskable-ish" and an
// equality match would miss the list form. The comparison is ASCII
// case-insensitive because the spec's keyword matching is: a browser honours
// "Maskable", and a case-sensitive sweep would skip the very icon it exists to
// judge while the maskable-count anchor below stayed satisfied by a second,
// correctly-cased entry.
func (icon manifestIcon) hasPurpose(want string) bool {
	for _, token := range strings.Fields(icon.Purpose) {
		if strings.EqualFold(token, want) {
			return true
		}
	}
	return false
}

// parseDeclaredSizes splits a manifest `sizes` string into pixel dimensions,
// returning the tokens it could not read alongside them. The unreadable tokens
// are returned rather than dropped on purpose: a `sizes` value this function
// cannot parse is a defect to report, never an absence of constraint. Silently
// yielding an empty set would let `"512"` (the `x` typo'd away) or the vector
// form `"any"` switch the size clause off, and a 192x192 asset shipped under a
// 512 declaration would then pass.
func parseDeclaredSizes(sizes string) ([]image.Point, []string) {
	var (
		parsed   []image.Point
		unparsed []string
	)
	for _, token := range strings.Fields(sizes) {
		width, height, found := strings.Cut(strings.ToLower(token), "x")
		if !found {
			unparsed = append(unparsed, token)
			continue
		}
		parsedWidth, widthErr := strconv.Atoi(width)
		parsedHeight, heightErr := strconv.Atoi(height)
		if widthErr != nil || heightErr != nil {
			unparsed = append(unparsed, token)
			continue
		}
		parsed = append(parsed, image.Point{X: parsedWidth, Y: parsedHeight})
	}
	return parsed, unparsed
}

// resolveEmbeddedIconPath maps a manifest icon `src` onto its path inside the
// embed FS. A browser resolves `src` against the manifest's own URL, so
// "/static/pwa/icon-512.png" and "pwa/icon-512.png" name the same asset; only
// the absolute spelling appears in this repo today, and a guard that understood
// only that one would accuse a correct manifest of not embedding its icon and
// send the reader to the wrong file. An absolute URL names something outside the
// binary and is reported as such rather than mangled into a nonsense path.
//
// The result is confined to the manifest's own directory. Both spellings clean
// their input, and cleaning resolves `..` rather than refusing it:
// path.Join("static", "../pwa/x.png") is "pwa/x.png" and path.Clean of
// "/static/../../x.png" is "/x.png". The embed FS would refuse to open either,
// so nothing is read — but the caller's message would then read "not embedded at
// x.png", which describes neither the manifest's mistake nor this resolver's
// part in it. Naming the traversal here keeps every failure pointing at its own
// cause.
func resolveEmbeddedIconPath(src string) (string, error) {
	if strings.HasPrefix(src, "//") || strings.Contains(src, "://") {
		return "", fmt.Errorf(
			"src %q points off-origin; this guard only judges assets embedded in the binary", src)
	}

	root := path.Dir(manifestPath)
	resolved := path.Join(root, src)
	if strings.HasPrefix(src, "/") {
		resolved = strings.TrimPrefix(path.Clean(src), "/")
	}
	if !strings.HasPrefix(resolved, root+"/") {
		return "", fmt.Errorf(
			"src %q resolves to %q, which escapes %s/ — a path traversal, not a missing asset",
			src, resolved, root)
	}
	return resolved, nil
}

// iconTypeDefect compares an icon's declared media type against the format its
// bytes actually decoded as, and returns "" when they agree or when the manifest
// declares no type (the field is optional per the spec).
//
// Without this, an icon declaring "image/svg+xml" beside purpose=maskable
// reaches the decoder and fails with a message about bytes — "not a PNG file" —
// when the defect is that the manifest promises a media type the asset is not.
// Every other failure in this file names its own cause; this one did not, and a
// parsed-but-unused Type field suggested a check that never happened.
func iconTypeDefect(declaredType, decodedFormat string) string {
	if declaredType == "" || strings.EqualFold(declaredType, "image/"+decodedFormat) {
		return ""
	}
	return fmt.Sprintf("declaring type %q while its bytes decode as %s", declaredType, decodedFormat)
}

// maskableIconDefects returns one line per way the decoded image fails the
// maskable contract, and nil when it holds. Returning findings rather than
// calling t.Errorf keeps the predicate feedable from the fixture test below,
// which is what proves each clause can fail at all.
func maskableIconDefects(img image.Image, declaredSizes string) []string {
	var defects []string
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	// One bad attribute, one complaint. An unreadable token and "no dimensions
	// came out of it" are the same defect said twice, so the unreadable-token
	// line carries the consequence itself and the no-dimensions line is reserved
	// for the case it alone describes: a `sizes` that is absent or whitespace.
	declared, unparsed := parseDeclaredSizes(declaredSizes)
	if len(unparsed) > 0 {
		consequence := "the tokens that did parse still constrain the decoded size"
		if len(declared) == 0 {
			consequence = fmt.Sprintf("leaving the decoded %dx%d unconstrained", width, height)
		}
		defects = append(defects, fmt.Sprintf(
			`declaring sizes %v, which are not WxH pixel dimensions ("any" is the vector form and does not describe a raster PNG) — %s`,
			unparsed, consequence))
	}
	switch {
	case len(declared) > 0:
		matched := false
		for _, size := range declared {
			if size.X == width && size.Y == height {
				matched = true
				break
			}
		}
		if !matched {
			defects = append(defects, fmt.Sprintf(
				"decoded %dx%d, which matches none of the declared sizes %v", width, height, declared))
		}
	case len(unparsed) == 0:
		defects = append(defects, fmt.Sprintf(
			"declaring no sizes at all (%q), leaving the decoded %dx%d unconstrained",
			declaredSizes, width, height))
	}

	// Every clause below needs a square canvas to mean anything: the safe zone is
	// a circle inscribed by the icon's own edge, and on a 512x256 image a radius
	// taken from either side describes a region no mask has. Reporting one clear
	// defect beats following it with a percentage that is arithmetic about
	// nothing.
	if width != height {
		return append(defects, fmt.Sprintf("not square: decoded %dx%d", width, height))
	}

	centre := float64(width) / 2
	radius := maskableSafeZoneFraction / 2 * float64(width)
	radiusSquared := radius * radius
	var outside, transparent, partial int
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			deltaX := float64(x-bounds.Min.X) + 0.5 - centre
			deltaY := float64(y-bounds.Min.Y) + 0.5 - centre
			if deltaX*deltaX+deltaY*deltaY <= radiusSquared {
				continue
			}
			outside++
			// Both arms matter, and the second is the one a real defect trips:
			// a pre-rounded corner arc is antialiased by construction, so its
			// edge is partially transparent rather than empty. The icon this
			// file was written against carried 597 such pixels beside its 10168
			// empty ones.
			switch _, _, _, alpha := img.At(x, y).RGBA(); {
			case alpha == 0:
				transparent++
			case alpha < 0xffff:
				partial++
			}
		}
	}
	if transparent+partial > 0 {
		defects = append(defects, fmt.Sprintf(
			"not full-bleed: of %d pixels outside the %.0f%% safe zone, %d are fully transparent and %d partially so (%.2f%% not opaque)",
			outside, maskableSafeZoneFraction*100, transparent, partial,
			100*float64(transparent+partial)/float64(outside)))
	}
	return defects
}

func readManifest(t *testing.T) webManifest {
	t.Helper()

	raw, err := Files.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read embedded %s: %v", manifestPath, err)
	}
	var manifest webManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode %s: %v", manifestPath, err)
	}
	if len(manifest.Icons) == 0 {
		t.Fatalf("%s declares no icons at all — the manifest, not the assets, is broken", manifestPath)
	}
	return manifest
}

// TestEveryMaskableManifestIconIsSquareFullSizeAndFullBleed reads the icons the
// binary actually ships (through the embed FS, not off disk) and holds each one
// declaring `purpose: maskable` to the contract that declaration makes.
func TestEveryMaskableManifestIconIsSquareFullSizeAndFullBleed(t *testing.T) {
	manifest := readManifest(t)

	declaredMaskable := 0
	for _, icon := range manifest.Icons {
		if !icon.hasPurpose("maskable") {
			continue
		}
		declaredMaskable++

		assetPath, resolveErr := resolveEmbeddedIconPath(icon.Src)
		if resolveErr != nil {
			t.Errorf("icon %q declared in %s: %v", icon.Src, manifestPath, resolveErr)
			continue
		}
		file, err := Files.Open(assetPath)
		if err != nil {
			t.Errorf("icon %q declared in the manifest is not embedded at %q: %v", icon.Src, assetPath, err)
			continue
		}
		// image.Decode rather than png.Decode so the format is named: it is what
		// the declared `type` is checked against below.
		img, format, decodeErr := image.Decode(file)
		// Closed and reported before the decode verdict is acted on: when an
		// embedded file is truncated both errors exist, and the close error is
		// the one pointing at the embed FS rather than at the artwork.
		if closeErr := file.Close(); closeErr != nil {
			t.Errorf("close %s: %v", icon.Src, closeErr)
		}
		if decodeErr != nil {
			t.Errorf("decode %s (manifest declares type %q): %v", icon.Src, icon.Type, decodeErr)
			continue
		}

		defects := maskableIconDefects(img, icon.Sizes)
		if defect := iconTypeDefect(icon.Type, format); defect != "" {
			defects = append(defects, defect)
		}
		for _, defect := range defects {
			t.Errorf("%s declares purpose %q but is %s\n"+
				"\tFix the asset (full-bleed opaque background, artwork inside the safe circle) "+
				"or stop declaring the icon maskable in %s.", icon.Src, icon.Purpose, defect, manifestPath)
		}
	}

	// This counts DECLARATIONS, not icons judged — it is incremented before the
	// resolve, open and decode attempts, and each of those three failure paths
	// reports its own t.Errorf before it continues. So a run can reach zero
	// judged icons only by already being red, and the counter needs no second
	// branch for it; what it uniquely catches is a manifest that stopped
	// declaring a maskable icon at all, which nothing else here would notice.
	//
	// Not an anti-vacuity anchor either — that is the fixture test below, which
	// owns its inputs. This is the product claim the manifest makes: install
	// surfaces get a purpose-built maskable icon rather than the platform's
	// shrink-and-plate fallback. Dropping the declaration is a legitimate choice;
	// making it deliberately means deleting this assertion with it.
	if declaredMaskable == 0 {
		t.Errorf("%s declares no icon with purpose=maskable, so this sweep examined nothing", manifestPath)
	}
}

// solidIcon paints a fully opaque rectangle — the shape a correct maskable asset
// has before any artwork is drawn on it.
func solidIcon(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{R: 0xff, G: 0xf9, B: 0xf0, A: 0xff})
		}
	}
	return img
}

// roundedCornerIcon is the defect this file exists to catch: a pre-rounded
// background whose four corner arcs fall short of opaque. cornerAlpha selects
// which arm of the opacity scan the fixture exercises — 0 for the empty corners
// of a flat pre-rounded plate, an intermediate value for the antialiased edge a
// real rounded corner carries.
func roundedCornerIcon(size, cornerRadius int, cornerAlpha uint8) *image.NRGBA {
	img := solidIcon(size, size)
	for y := range size {
		for x := range size {
			cornerX, cornerY := cornerRadius, cornerRadius
			if x >= size-cornerRadius {
				cornerX = size - cornerRadius - 1
			} else if x >= cornerRadius {
				continue
			}
			if y >= size-cornerRadius {
				cornerY = size - cornerRadius - 1
			} else if y >= cornerRadius {
				continue
			}
			deltaX, deltaY := x-cornerX, y-cornerY
			if deltaX*deltaX+deltaY*deltaY > cornerRadius*cornerRadius {
				img.SetNRGBA(x, y, color.NRGBA{A: cornerAlpha})
			}
		}
	}
	return img
}

// TestMaskableIconDefectsRejectsEachWayAnIconCanFail feeds the predicate one
// input per clause it asserts, so no clause can go green by being unreachable.
// The manifest sweep above then applies a predicate already known to bite.
func TestMaskableIconDefectsRejectsEachWayAnIconCanFail(t *testing.T) {
	t.Run("a full-bleed opaque square passes", func(t *testing.T) {
		if defects := maskableIconDefects(solidIcon(512, 512), "512x512"); len(defects) != 0 {
			t.Fatalf("a correct maskable icon was rejected: %v", defects)
		}
	})

	t.Run("empty corners are rejected", func(t *testing.T) {
		defects := maskableIconDefects(roundedCornerIcon(512, 109, 0), "512x512")
		if !anyDefectContains(defects, "not full-bleed") {
			t.Fatalf("fully transparent corners went unreported: %v", defects)
		}
	})

	// Separate from the case above because it isolates the second arm of the
	// opacity scan: these corners are partially opaque, never empty, so the
	// report must name zero fully transparent pixels and still fail. Narrowing
	// the partial arm (`alpha < 0xffff` → `alpha < 1`) leaves the empty-corner
	// case green and reddens only here.
	t.Run("antialiased partly transparent corners are rejected", func(t *testing.T) {
		defects := maskableIconDefects(roundedCornerIcon(512, 109, 128), "512x512")
		if !anyDefectContains(defects, "not full-bleed") {
			t.Fatalf("partially transparent corners went unreported: %v", defects)
		}
		if !anyDefectContains(defects, "0 are fully transparent") {
			t.Fatalf("expected the report to rest on partial opacity alone: %v", defects)
		}
	})

	// The fixture is non-square AND carries non-opaque pixels, which is what
	// makes the second half of this case bite: a fully opaque oblong produces no
	// safe-zone line whether or not the scan is reached, so it would prove
	// nothing about the early return. With transparency present, dropping the
	// return prints a percentage measured over a circle that overflows the image
	// — arithmetic about a region no mask has.
	t.Run("a non-square icon is rejected without a bogus safe-zone claim", func(t *testing.T) {
		oblong := solidIcon(512, 256)
		oblong.SetNRGBA(0, 0, color.NRGBA{})
		oblong.SetNRGBA(511, 255, color.NRGBA{})

		defects := maskableIconDefects(oblong, "512x256")
		if !anyDefectContains(defects, "not square") {
			t.Fatalf("a 512x256 icon went unreported: %v", defects)
		}
		// The absence of the safe-zone line, not a total defect count: a count
		// also moves when an unrelated clause starts firing (widen this case to
		// declare "512x512" and it becomes 2), and would then fail with a message
		// about a safe-zone claim that is correctly absent.
		if anyDefectContains(defects, "not full-bleed") {
			t.Fatalf("a non-square icon must not carry a safe-zone percentage: %v", defects)
		}
	})

	t.Run("an icon smaller than its declared size is rejected", func(t *testing.T) {
		defects := maskableIconDefects(solidIcon(192, 192), "512x512")
		if !anyDefectContains(defects, "matches none of the declared sizes") {
			t.Fatalf("a 192x192 icon declared as 512x512 went unreported: %v", defects)
		}
	})

	// The three cases below are the point of finding 1: an unreadable `sizes`
	// string must be reported, never treated as "no constraint declared".
	t.Run("a sizes token that is not WxH is reported once", func(t *testing.T) {
		defects := maskableIconDefects(solidIcon(192, 192), "512")
		if !anyDefectContains(defects, "not WxH pixel dimensions") {
			t.Fatalf(`the unparseable token "512" went unreported: %v`, defects)
		}
		if !anyDefectContains(defects, "unconstrained") {
			t.Fatalf("an unreadable sizes string must not silently disable the size clause: %v", defects)
		}
		// One bad attribute, one complaint: the reader is fixing a single
		// `sizes` value and should not have to work out that two lines are the
		// same defect.
		if len(defects) != 1 {
			t.Fatalf("one unreadable sizes string produced %d defects: %v", len(defects), defects)
		}
	})

	t.Run("the vector sizes form is reported on a raster icon", func(t *testing.T) {
		defects := maskableIconDefects(solidIcon(512, 512), "any")
		if !anyDefectContains(defects, "not WxH pixel dimensions") {
			t.Fatalf(`"any" went unreported on a PNG: %v`, defects)
		}
	})

	t.Run("a partly unreadable sizes string still checks the tokens that parsed", func(t *testing.T) {
		defects := maskableIconDefects(solidIcon(192, 192), "512x512 nonsense")
		if !anyDefectContains(defects, "not WxH pixel dimensions") {
			t.Fatalf(`the token "nonsense" went unreported: %v`, defects)
		}
		if !anyDefectContains(defects, "matches none of the declared sizes") {
			t.Fatalf("a readable token beside an unreadable one must still constrain the size: %v", defects)
		}
	})

	t.Run("an absent sizes string is reported", func(t *testing.T) {
		defects := maskableIconDefects(solidIcon(512, 512), "")
		if !anyDefectContains(defects, "declaring no sizes at all") {
			t.Fatalf("a missing sizes declaration went unreported: %v", defects)
		}
	})
}

// TestHasPurposeMatchesTheManifestSpecsTokenRules pins the two ways the purpose
// match can go wrong in opposite directions: missing a legal spelling, and
// firing on a word that merely starts with the keyword.
func TestHasPurposeMatchesTheManifestSpecsTokenRules(t *testing.T) {
	for _, testCase := range []struct {
		purpose string
		want    bool
	}{
		{purpose: "maskable", want: true},
		{purpose: "any maskable", want: true},
		{purpose: "maskable any", want: true},
		{purpose: "  maskable  ", want: true},
		{purpose: "Maskable", want: true},
		{purpose: "ANY MASKABLE", want: true},
		{purpose: "any", want: false},
		{purpose: "maskable-ish", want: false},
		{purpose: "unmaskable", want: false},
		{purpose: "", want: false},
	} {
		icon := manifestIcon{Purpose: testCase.purpose}
		if got := icon.hasPurpose("maskable"); got != testCase.want {
			t.Errorf("hasPurpose(%q) = %v, want %v", testCase.purpose, got, testCase.want)
		}
	}
}

// TestResolveEmbeddedIconPathFollowsTheManifestsOwnURL pins that both legal
// spellings of an icon `src` reach the same embedded file, so a relative one is
// never misreported as a missing asset.
func TestResolveEmbeddedIconPathFollowsTheManifestsOwnURL(t *testing.T) {
	const want = "static/pwa/icon-512-maskable.png"

	for _, src := range []string{
		"/static/pwa/icon-512-maskable.png",
		"pwa/icon-512-maskable.png",
		"./pwa/icon-512-maskable.png",
	} {
		got, err := resolveEmbeddedIconPath(src)
		if err != nil {
			t.Errorf("resolveEmbeddedIconPath(%q) errored: %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("resolveEmbeddedIconPath(%q) = %q, want %q", src, got, want)
		}
	}

	for _, src := range []string{
		"https://cdn.example/icon.png",
		"//cdn.example/icon.png",
	} {
		if got, err := resolveEmbeddedIconPath(src); err == nil {
			t.Errorf("resolveEmbeddedIconPath(%q) = %q, want an off-origin error", src, got)
		}
	}

	// Cleaning resolves `..` rather than refusing it, so without an explicit
	// confinement each of these yields a path outside static/ that the embed FS
	// would reject with a message naming neither the manifest's mistake nor the
	// resolver's part in it.
	for _, src := range []string{
		"../pwa/icon-512-maskable.png",
		"/static/../../icon.png",
		"/../icon.png",
		"/icon.png",
		"/static",
	} {
		got, err := resolveEmbeddedIconPath(src)
		if err == nil {
			t.Errorf("resolveEmbeddedIconPath(%q) = %q, want a traversal error", src, got)
			continue
		}
		if !strings.Contains(err.Error(), "escapes static/") {
			t.Errorf("resolveEmbeddedIconPath(%q) error %q does not name the traversal", src, err)
		}
	}
}

// TestIconTypeDefectComparesTheDeclaredTypeWithTheDecodedFormat pins that the
// manifest's `type` is checked rather than merely parsed.
func TestIconTypeDefectComparesTheDeclaredTypeWithTheDecodedFormat(t *testing.T) {
	for _, testCase := range []struct {
		declared string
		format   string
		wantHit  bool
	}{
		{declared: "image/png", format: "png", wantHit: false},
		{declared: "IMAGE/PNG", format: "png", wantHit: false},
		{declared: "", format: "png", wantHit: false},
		{declared: "image/svg+xml", format: "png", wantHit: true},
		{declared: "image/jpeg", format: "png", wantHit: true},
	} {
		defect := iconTypeDefect(testCase.declared, testCase.format)
		if gotHit := defect != ""; gotHit != testCase.wantHit {
			t.Errorf("iconTypeDefect(%q, %q) = %q, want a defect: %v",
				testCase.declared, testCase.format, defect, testCase.wantHit)
		}
	}
}

func anyDefectContains(defects []string, want string) bool {
	for _, defect := range defects {
		if strings.Contains(defect, want) {
			return true
		}
	}
	return false
}

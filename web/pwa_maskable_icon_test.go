package staticassets

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
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
// `src` values in it are absolute URLs ("/static/pwa/icon-512.png") and the
// embed FS is rooted at "static/", so a leading slash is all that separates the
// two spellings.
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
// equality match would miss the list form.
func (icon manifestIcon) hasPurpose(want string) bool {
	return strings.Contains(" "+strings.Join(strings.Fields(icon.Purpose), " ")+" ", " "+want+" ")
}

// declaredSizes parses the `sizes` string into pixel dimensions. "any" (the
// vector form) yields no dimensions and is reported as such by the caller.
func (icon manifestIcon) declaredSizes() []image.Point {
	var sizes []image.Point
	for _, token := range strings.Fields(icon.Sizes) {
		width, height, found := strings.Cut(strings.ToLower(token), "x")
		if !found {
			continue
		}
		parsedWidth, widthErr := strconv.Atoi(width)
		parsedHeight, heightErr := strconv.Atoi(height)
		if widthErr != nil || heightErr != nil {
			continue
		}
		sizes = append(sizes, image.Point{X: parsedWidth, Y: parsedHeight})
	}
	return sizes
}

// maskableIconDefects returns one line per way the decoded image fails the
// maskable contract, and nil when it holds. Returning findings rather than
// calling t.Errorf keeps the predicate feedable from the fixture test below,
// which is what proves each clause can fail at all.
func maskableIconDefects(img image.Image, declared []image.Point) []string {
	var defects []string
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	if width != height {
		defects = append(defects, fmt.Sprintf("not square: decoded %dx%d", width, height))
	}
	if len(declared) > 0 {
		matched := false
		for _, size := range declared {
			if size.X == width && size.Y == height {
				matched = true
				break
			}
		}
		if !matched {
			defects = append(defects, fmt.Sprintf(
				"decoded %dx%d matches none of the declared sizes %v", width, height, declared))
		}
	}

	centreX, centreY := float64(width)/2, float64(height)/2
	radius := maskableSafeZoneFraction / 2 * float64(width)
	radiusSquared := radius * radius
	var outside, transparent, partial int
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			deltaX := float64(x-bounds.Min.X) + 0.5 - centreX
			deltaY := float64(y-bounds.Min.Y) + 0.5 - centreY
			if deltaX*deltaX+deltaY*deltaY <= radiusSquared {
				continue
			}
			outside++
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

	maskable := 0
	for _, icon := range manifest.Icons {
		if !icon.hasPurpose("maskable") {
			continue
		}
		maskable++

		assetPath := strings.TrimPrefix(icon.Src, "/")
		file, err := Files.Open(assetPath)
		if err != nil {
			t.Errorf("icon %q declared in the manifest is not embedded at %q: %v", icon.Src, assetPath, err)
			continue
		}
		img, decodeErr := png.Decode(file)
		closeErr := file.Close()
		if decodeErr != nil {
			t.Errorf("decode %s: %v", icon.Src, decodeErr)
			continue
		}
		if closeErr != nil {
			t.Errorf("close %s: %v", icon.Src, closeErr)
		}

		for _, defect := range maskableIconDefects(img, icon.declaredSizes()) {
			t.Errorf("%s declares purpose %q but is %s\n"+
				"\tFix the asset (full-bleed opaque background, artwork inside the safe circle) "+
				"or stop declaring the icon maskable in %s.", icon.Src, icon.Purpose, defect, manifestPath)
		}
	}

	// Not an anti-vacuity anchor — that is the fixture test below, which owns its
	// inputs. This is the product claim the manifest makes: install surfaces get a
	// purpose-built maskable icon rather than the platform's shrink-and-plate
	// fallback. Dropping the declaration is a legitimate choice; making it
	// deliberately means deleting this assertion with it.
	if maskable == 0 {
		t.Errorf("%s declares no icon with purpose=maskable, so this sweep examined nothing", manifestPath)
	}
}

// solidIcon paints a fully opaque square — the shape a correct maskable asset
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
// background whose four corner arcs are transparent.
func roundedCornerIcon(size, cornerRadius int) *image.NRGBA {
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
				img.SetNRGBA(x, y, color.NRGBA{})
			}
		}
	}
	return img
}

// TestMaskableIconDefectsRejectsEachWayAnIconCanFail feeds the predicate one
// input per clause it asserts, so no clause can go green by being unreachable.
// The manifest sweep above then applies a predicate already known to bite.
func TestMaskableIconDefectsRejectsEachWayAnIconCanFail(t *testing.T) {
	declared512 := []image.Point{{X: 512, Y: 512}}

	t.Run("a full-bleed opaque square passes", func(t *testing.T) {
		if defects := maskableIconDefects(solidIcon(512, 512), declared512); len(defects) != 0 {
			t.Fatalf("a correct maskable icon was rejected: %v", defects)
		}
	})

	t.Run("transparent corners are rejected", func(t *testing.T) {
		defects := maskableIconDefects(roundedCornerIcon(512, 109), declared512)
		if !anyDefectContains(defects, "not full-bleed") {
			t.Fatalf("transparent corners went unreported: %v", defects)
		}
	})

	t.Run("a non-square icon is rejected", func(t *testing.T) {
		defects := maskableIconDefects(solidIcon(512, 256), []image.Point{{X: 512, Y: 256}})
		if !anyDefectContains(defects, "not square") {
			t.Fatalf("a 512x256 icon went unreported: %v", defects)
		}
	})

	t.Run("an icon smaller than its declared size is rejected", func(t *testing.T) {
		defects := maskableIconDefects(solidIcon(192, 192), declared512)
		if !anyDefectContains(defects, "matches none of the declared sizes") {
			t.Fatalf("a 192x192 icon declared as 512x512 went unreported: %v", defects)
		}
	})
}

func anyDefectContains(defects []string, want string) bool {
	for _, defect := range defects {
		if strings.Contains(defect, want) {
			return true
		}
	}
	return false
}

package staticassets

import (
	"encoding/json"
	"fmt"
	"image/png"
	"strings"
	"testing"
)

// TestManifestScreenshotsShipAndMatchTheirDeclaredSize: the install dialog shows
// these images, and a browser drops a screenshot whose file is missing or whose
// pixels differ from "sizes" — the dialog then falls back to the bare Install
// prompt with no error anywhere. A narrow one must also be portrait, or Chrome
// does not offer it on a phone.
func TestManifestScreenshotsShipAndMatchTheirDeclaredSize(t *testing.T) {
	raw, err := Files.ReadFile("static/manifest.webmanifest")
	if err != nil {
		t.Fatalf("read embedded manifest: %v", err)
	}
	var manifest struct {
		Screenshots []struct {
			Src        string `json:"src"`
			Sizes      string `json:"sizes"`
			Type       string `json:"type"`
			FormFactor string `json:"form_factor"`
			Label      string `json:"label"`
		} `json:"screenshots"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	forms := map[string]int{}
	for _, shot := range manifest.Screenshots {
		forms[shot.FormFactor]++
		if shot.Type != "image/png" || strings.TrimSpace(shot.Label) == "" {
			t.Errorf("%s: want type image/png and a label, got type %q label %q", shot.Src, shot.Type, shot.Label)
		}
		file, err := Files.Open(strings.TrimPrefix(shot.Src, "/"))
		if err != nil {
			t.Errorf("%s: not embedded: %v", shot.Src, err)
			continue
		}
		config, err := png.DecodeConfig(file)
		_ = file.Close()
		if err != nil {
			t.Errorf("%s: not a PNG: %v", shot.Src, err)
			continue
		}
		if got := fmt.Sprintf("%dx%d", config.Width, config.Height); got != shot.Sizes {
			t.Errorf("%s: pixels %s, manifest says %s", shot.Src, got, shot.Sizes)
		}
		if shot.FormFactor == "narrow" && config.Width >= config.Height {
			t.Errorf("%s: a narrow screenshot must be portrait, got %dx%d", shot.Src, config.Width, config.Height)
		}
	}
	if forms["narrow"] == 0 || forms["wide"] == 0 {
		t.Fatalf("want at least one narrow and one wide screenshot, got %v", forms)
	}
}

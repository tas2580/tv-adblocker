package main

import (
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func coloredTestLogo(color [3]byte, scene int, alpha float64) Frame {
	f := compositeTestLogo(48, 36, scene, 0, false)
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			if !logoTestShape(x, y, f.W, f.H) {
				continue
			}
			for c := 0; c < 3; c++ {
				i := (y*f.W+x)*3 + c
				f.Pix[i] = byte(math.Round(alpha*float64(color[c]) + (1-alpha)*float64(f.Pix[i])))
			}
		}
	}
	return f
}

func colorFrame(w, h int, color [3]byte) Frame {
	f := Frame{w, h, make([]byte, w*h*3)}
	for i := 0; i < len(f.Pix); i += 3 {
		copy(f.Pix[i:i+3], color[:])
	}
	return f
}

func calibratedColorTemplate(t *testing.T, color [3]byte, alpha float64) *logoTemplate {
	t.Helper()
	r := Rect{W: 48, H: 36}
	template := prepareLogo("color", coloredTestLogo(color, 0, alpha), r, explicitTestMask(r.W, r.H))
	for scene := 0; scene < 16; scene++ {
		var plane logoPlane
		template.find(coloredTestLogo(color, scene, alpha), &plane, .7)
	}
	if template.color.estimate == nil {
		t.Fatalf("no colour learned for %v, alpha %v (%d samples, %d pairs)", color, alpha, len(template.color.samples), len(template.color.pairs))
	}
	for c := 0; c < 3; c++ {
		// Local background gradients limit the accuracy of an eight-frame
		// estimate, especially for strongly transparent, saturated colours.
		if math.Abs(template.color.estimate.RGB[c]-float64(color[c])) > 20 {
			t.Fatalf("wrong foreground colour for %v, alpha %v: %+v", color, alpha, template.color.estimate)
		}
	}
	return template
}

func TestLogoColorLearnsTransparentWhiteBlackAndColoredLogos(t *testing.T) {
	for _, color := range [][3]byte{{245, 245, 245}, {10, 10, 10}, {245, 170, 45}} {
		for _, alpha := range []float64{.6, .85, 1} {
			t.Run(fmt.Sprintf("%v/%.2f", color, alpha), func(t *testing.T) {
				template := calibratedColorTemplate(t, color, alpha)
				f := colorFrame(48, 36, color)
				var plane logoPlane
				result := template.find(f, &plane, .7)
				if result.Decision != "uncertain" || result.UncertaintyReason != "logo_color_low_contrast" || result.LogoColor == nil {
					t.Fatalf("flat matching colour not protected: %+v", result)
				}
				if result.LogoColor.Confidence < .6 || result.LogoColor.Samples < logoColorMinimumSamples {
					t.Fatalf("invalid calibration: %+v", result.LogoColor)
				}
			})
		}
	}
}

func TestLogoColorNeedsConfirmedChangingBackgrounds(t *testing.T) {
	color := [3]byte{245, 245, 245}
	f := coloredTestLogo(color, 0, .85)
	r := Rect{W: 48, H: 36}
	template := prepareLogo("color", f, r, explicitTestMask(r.W, r.H))
	for i := 0; i < 20; i++ {
		var plane logoPlane
		template.find(f, &plane, .7)
	}
	if template.color.estimate != nil || len(template.color.samples) != 1 {
		t.Fatal("identical frames calibrated the colour")
	}
	for _, f := range []Frame{solidFrame(48, 36, 245), solidFrame(48, 36, 40)} {
		var plane logoPlane
		if result := template.find(f, &plane, .7); result.Decision != "missing" || result.LogoColor != nil {
			t.Fatalf("uncalibrated guard or false positive: %+v", result)
		}
	}
	if len(template.color.samples) != 1 {
		t.Fatal("missing frames trained the colour")
	}
	// Several different frames with too little variation still cannot identify
	// the mixing coefficient; this is separate from duplicate suppression.
	m := logoColorModel{}
	for i := 0; i < logoColorMinimumSamples; i++ {
		m.samples = append(m.samples, solidFrame(2, 1, byte(100+i)))
	}
	if _, ok := m.fitPair(0, 1); ok {
		t.Fatal("low-variation edge accepted")
	}
	// A newly prepared template starts with no calibration, even for the same ID.
	calibrated := calibratedColorTemplate(t, color, .85)
	fresh := prepareLogo(calibrated.id, calibrated.frame, calibrated.rect, calibrated.mask)
	if fresh.color.estimate != nil || len(fresh.color.samples) != 0 {
		t.Fatal("calibration leaked between sessions")
	}
}

func TestLogoColorRejectsWrongColorAndTextureAndKeepsShapePriority(t *testing.T) {
	template := calibratedColorTemplate(t, [3]byte{245, 245, 245}, .85)
	for _, color := range [][3]byte{{40, 40, 40}, {245, 80, 80}, {100, 160, 230}} {
		var plane logoPlane
		if result := template.find(colorFrame(48, 36, color), &plane, .7); result.Decision != "missing" || result.UncertaintyReason != "" {
			t.Fatalf("wrong colour protected: %+v", result)
		}
	}
	f := solidFrame(48, 36, 245)
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			if (x/3+y/3)%2 == 0 {
				for c := 0; c < 3; c++ {
					f.Pix[(y*f.W+x)*3+c] = 215
				}
			}
		}
	}
	if template.logoColorHidden(f, template.rect) {
		t.Fatal("textured region protected")
	}
	var plane logoPlane
	result := template.find(coloredTestLogo([3]byte{245, 245, 245}, 18, .85), &plane, .7)
	if result.Decision != "present" || result.UncertaintyReason != "" {
		t.Fatalf("real shape lost: %+v", result)
	}
	// Returned diagnostics own an immutable estimate, including when learning
	// updates the template on a later frame.
	old := *result.LogoColor
	template.color.estimate.RGB[0] = 17
	if *result.LogoColor != old {
		t.Fatal("previous diagnostic mutated")
	}
}

func TestLogoColorTracksConfirmedPositionAndSearchesBeforeProtecting(t *testing.T) {
	template := calibratedColorTemplate(t, [3]byte{245, 245, 245}, .85)
	template.rect = Rect{230, 8, 48, 36}
	template.last = template.rect
	target := Rect{246, 14, 48, 36}
	f := solidFrame(320, 180, 40)
	insertTestFrame(&f, coloredTestLogo([3]byte{245, 245, 245}, 0, .85), target)
	var plane logoPlane
	result := template.find(f, &plane, .7)
	if result.Decision != "present" || template.last != target {
		t.Fatalf("moved logo not confirmed: %+v", result)
	}
	f = solidFrame(320, 180, 40)
	insertTestFrame(&f, solidFrame(48, 36, 245), target)
	plane = logoPlane{}
	result = template.find(f, &plane, .7)
	if result.Decision != "uncertain" || result.Rect != target || template.last != target {
		t.Fatalf("last confirmed position not protected: %+v", result)
	}
	// A white patch at the old position must not suppress a genuine match
	// elsewhere in the search area or become a newly tracked logo by itself.
	newTarget := Rect{180, 45, 48, 36}
	insertTestFrame(&f, coloredTestLogo([3]byte{245, 245, 245}, 0, .85), newTarget)
	plane = logoPlane{}
	result = template.find(f, &plane, .7)
	if result.Decision != "present" || result.Rect != newTarget || result.UncertaintyReason != "" {
		t.Fatalf("colour guard prevented shape search: %+v", result)
	}
	// The original position also remains protected after a tracked move.
	f = solidFrame(320, 180, 40)
	insertTestFrame(&f, solidFrame(48, 36, 245), template.rect)
	plane = logoPlane{}
	result = template.find(f, &plane, .7)
	if result.Decision != "uncertain" || result.Rect != template.rect || template.last != newTarget {
		t.Fatalf("original position protection changed tracking: %+v", result)
	}
}

func TestLogoColorWaitsForDistantPositionAndProtectsItAfterConfirmation(t *testing.T) {
	color := [3]byte{245, 245, 245}
	template := calibratedColorTemplate(t, color, .85)
	template.rect = Rect{8, 8, 48, 36}
	template.last = template.rect
	// Keep the learned estimate but clear its history so new learning is visible.
	template.color.samples = nil
	target := Rect{300, 180, 48, 36}
	frame := solidFrame(960, 720, 40)
	insertTestFrame(&frame, solidFrame(48, 36, 245), template.rect)
	insertTestFrame(&frame, coloredTestLogo(color, 0, .85), target)
	for n := 1; n <= 3; n++ {
		var plane logoPlane
		result := template.find(frame, &plane, .7)
		if n < 3 {
			if result.UncertaintyReason != "logo_position_pending" || result.Decision != "uncertain" || len(template.color.samples) != 0 || template.last != template.rect {
				t.Fatalf("colour guard overrode pending position on frame %d: %+v", n, result)
			}
		} else if result.Decision != "present" || result.UncertaintyReason != "" || template.last != target || len(template.color.samples) != 1 {
			t.Fatalf("distant position not confirmed before learning: %+v", result)
		}
	}
	// The new position gains the same low-contrast protection as the old one.
	frame = solidFrame(960, 720, 40)
	insertTestFrame(&frame, solidFrame(48, 36, 245), target)
	var plane logoPlane
	result := template.find(frame, &plane, .7)
	if result.Decision != "uncertain" || result.UncertaintyReason != "logo_color_low_contrast" || result.Rect != target || template.last != target {
		t.Fatalf("confirmed distant position not protected: %+v", result)
	}
}

func TestLogoColorAllowsRealAdSwitchAndRequiresShapeForReturn(t *testing.T) {
	template := calibratedColorTemplate(t, [3]byte{245, 245, 245}, .85)
	a, cfg, _, _ := debugTestApp(t, false, false)
	cfg.ProgramCooldownSeconds, cfg.PlaceholderMinSeconds = 0, 0
	a.cfg = cfg
	absent := solidFrame(48, 36, 40)
	for i := 0; i < cfg.MissingFrames; i++ {
		var plane logoPlane
		result := template.find(absent, &plane, .7)
		if result.Decision != "missing" {
			t.Fatalf("real absence protected: %+v", result)
		}
		a.observeLogo(result, template, absent, cfg, a.active.URL)
	}
	if a.status.Mode != "placeholder" {
		t.Fatalf("did not switch to filler: %+v", a.status)
	}
	hidden := solidFrame(48, 36, 245)
	for i := 0; i < 20; i++ {
		var plane logoPlane
		result := template.find(hidden, &plane, .7)
		a.observeLogo(result, template, hidden, cfg, a.active.URL)
		if a.status.Mode != "placeholder" || a.status.Matches != 0 || a.status.Misses != 0 {
			t.Fatalf("colour caused a switch or timed out: %+v", a.status)
		}
	}
	present := coloredTestLogo([3]byte{245, 245, 245}, 0, .85)
	for i := 0; i < cfg.PresentFrames; i++ {
		var plane logoPlane
		result := template.find(present, &plane, .7)
		if result.Decision != "present" {
			t.Fatalf("returning logo not confirmed: %+v", result)
		}
		a.observeLogo(result, template, present, cfg, a.active.URL)
	}
	if a.status.Mode != "sender" {
		t.Fatalf("did not return with a real logo: %+v", a.status)
	}
}

func TestLogoColorLegacyDetectionUsesRawRGB(t *testing.T) {
	color := [3]byte{245, 170, 45}
	ref := coloredTestLogo(color, 0, .85)
	r := Rect{W: 48, H: 36}
	template := prepareLogo("legacy", contrastLogoFrame(ref, 1.5), r, explicitTestMask(r.W, r.H))
	template.frame, template.legacyContrast = ref, 1.5
	for scene := 0; scene < 16; scene++ {
		var plane logoPlane
		template.find(coloredTestLogo(color, scene, .85), &plane, .7)
	}
	if template.color.estimate == nil || math.Abs(template.color.estimate.RGB[0]-float64(color[0])) > 20 || math.Abs(template.color.estimate.RGB[2]-float64(color[2])) > 20 {
		t.Fatalf("legacy calibration used contrast-adjusted greyscale: %+v", template.color.estimate)
	}
}

func loadColorFixture(t *testing.T, name string) Frame {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "prosieben-white-sky", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	b := im.Bounds()
	out := Frame{b.Dx(), b.Dy(), make([]byte, b.Dx()*b.Dy()*3)}
	for y := 0; y < out.H; y++ {
		for x := 0; x < out.W; x++ {
			r, g, b, _ := im.At(x, y).RGBA()
			i := (y*out.W + x) * 3
			out.Pix[i], out.Pix[i+1], out.Pix[i+2] = byte(r>>8), byte(g>>8), byte(b>>8)
		}
	}
	return out
}

func TestLogoColorProSiebenWhiteSkyRegression(t *testing.T) {
	path := filepath.Join("testdata", "prosieben-white-sky")
	f, err := loadReference(filepath.Join(path, "reference.rgb"))
	if err != nil {
		t.Fatal(err)
	}
	mask, err := loadLogoMaskFile(filepath.Join(path, "mask.png"), f)
	if err != nil {
		t.Fatal(err)
	}
	r := Rect{W: f.W, H: f.H}
	template := prepareLogo("regression", f, r, mask)
	for i := 1; i <= 5; i++ {
		var plane logoPlane
		result := template.find(loadColorFixture(t, fmt.Sprintf("calibration-%02d.png", i)), &plane, .75)
		if result.Decision != "present" {
			t.Fatalf("calibration %d: %+v", i, result)
		}
	}
	a, cfg, _, _ := debugTestApp(t, false, false)
	cfg.MissingFrames = 5
	a.cfg = cfg
	for i := 1; i <= 10; i++ {
		frame := loadColorFixture(t, fmt.Sprintf("frame-%02d.png", i))
		var plane logoPlane
		result := template.find(frame, &plane, .75)
		if i >= 6 {
			if result.Decision != "uncertain" || result.UncertaintyReason != "logo_color_low_contrast" {
				t.Fatalf("white-sky frame %d: %+v; calibration %+v", i, result, template.color.estimate)
			}
			if i == 6 {
				a.status.Misses = cfg.MissingFrames - 1
			}
		}
		a.observeLogo(result, template, frame, cfg, a.active.URL)
		if a.status.Mode != "monitoring" || i >= 6 && (a.status.Misses != 0 || a.status.Matches != 0 || a.status.LogoPresent != nil) {
			t.Fatalf("frame %d switched or kept counters: %+v", i, a.status)
		}
	}
	if template.color.estimate == nil {
		t.Fatal("regression did not calibrate itself")
	}
	t.Logf("ProSieben inferred colour: %+v", template.color.estimate)
	// The colour guard never confirms presence while a filler is playing.
	a.status.Mode = "placeholder"
	a.status.Matches = cfg.PresentFrames - 1
	frame := loadColorFixture(t, "frame-10.png")
	var plane logoPlane
	result := template.find(frame, &plane, .75)
	a.observeLogo(result, template, frame, cfg, a.active.URL)
	if a.status.Mode != "placeholder" || a.status.Matches != 0 {
		t.Fatal("guard triggered a return to the channel")
	}
	// Colour and reason survive the same JSON metadata used by status and exports.
	data, err := json.Marshal(a.status.LogoDetection)
	if err != nil {
		t.Fatal(err)
	}
	var decoded LogoResult
	if err = json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.LogoColor == nil || decoded.UncertaintyReason != "logo_color_low_contrast" {
		t.Fatalf("lost diagnostics: %s", data)
	}
}

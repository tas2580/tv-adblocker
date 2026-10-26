package main

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func dmaxFixtureFrame(t *testing.T, name string) Frame {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", "dmax-white-sky", name))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	im, _, err := image.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	b := im.Bounds()
	f := Frame{W: b.Dx(), H: b.Dy(), Pix: make([]byte, b.Dx()*b.Dy()*3)}
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			r, g, b, _ := im.At(x, y).RGBA()
			i := (y*f.W + x) * 3
			f.Pix[i], f.Pix[i+1], f.Pix[i+2] = byte(r>>8), byte(g>>8), byte(b>>8)
		}
	}
	return f
}

func dmaxFixtureTemplate(t *testing.T) *logoTemplate {
	t.Helper()
	root := filepath.Join("testdata", "dmax-white-sky")
	ref, err := loadReference(filepath.Join(root, "reference.rgb"))
	if err != nil {
		t.Fatal(err)
	}
	mask, err := loadLogoMaskFile(filepath.Join(root, "mask.png"), ref)
	if err != nil {
		t.Fatal(err)
	}
	return prepareLogo("dmax", ref, Rect{264, 60, 86, 35}, mask)
}

func dmaxFixtureCapture(t *testing.T, n int) debugCapture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "dmax-white-sky", fmt.Sprintf("fall%d", n), "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var capture debugCapture
	if err := json.Unmarshal(b, &capture); err != nil {
		t.Fatal(err)
	}
	return capture
}

func TestDMAXContrastWithRecordedColorModels(t *testing.T) {
	for n := 1; n <= 3; n++ {
		t.Run(fmt.Sprintf("fall%d", n), func(t *testing.T) {
			template := dmaxFixtureTemplate(t)
			for i, entry := range dmaxFixtureCapture(t, n).Frames {
				if entry.Result.Decision != "missing" {
					continue
				}
				template.color.estimate = entry.Result.LogoColor
				f := dmaxFixtureFrame(t, fmt.Sprintf("fall%d/%s", n, entry.Image))
				check := template.checkLogoContrast(f, template.rect)
				t.Logf("frame %02d: %+v", i+1, check)
				if check == nil || !check.Protected {
					t.Errorf("visible low-contrast logo not protected in frame %d: %+v", i+1, check)
				}
			}
		})
	}
}

func TestDMAXWhiteSkyNoAutomaticSwitch(t *testing.T) {
	for n := 1; n <= 3; n++ {
		t.Run(fmt.Sprintf("fall%d", n), func(t *testing.T) {
			template := dmaxFixtureTemplate(t)
			// Learn from earlier confirmed broadcasts, never from the failing
			// frames or a manually injected white colour.
			for i := 1; i <= 12; i++ {
				crop := dmaxFixtureFrame(t, fmt.Sprintf("calibration-%02d.png", i))
				frame := solidFrame(1280, 720, 40)
				insertTestFrame(&frame, crop, template.rect)
				var plane logoPlane
				if result := template.find(frame, &plane, .5); result.Decision != "present" {
					t.Fatalf("calibration frame %d: %+v", i, result)
				}
			}
			if template.color.estimate == nil {
				t.Fatalf("colour did not calibrate (%d distinct samples)", len(template.color.samples))
			}
			t.Logf("learned colour: %+v", template.color.estimate)
			capture := dmaxFixtureCapture(t, n)
			a, cfg, _, _ := debugTestApp(t, false, false)
			cfg.SimilarityMinimum, cfg.MissingFrames, cfg.PresentFrames = capture.SimilarityMinimum, capture.MissingFrames, capture.PresentFrames
			a.cfg = cfg
			for i, entry := range capture.Frames {
				frame := dmaxFixtureFrame(t, fmt.Sprintf("fall%d/%s", n, entry.Image))
				var plane logoPlane
				result := template.find(frame, &plane, cfg.SimilarityMinimum)
				t.Logf("frame %02d: %s, score %.3f", i+1, result.Decision, result.Score)
				if i >= 7 && result.Decision == "missing" {
					t.Errorf("low-contrast frame %d classified missing: %+v", i+1, result)
				}
				if result.Decision == "uncertain" {
					// Protection must also clear an already imminent switch.
					a.status.Misses = cfg.MissingFrames - 1
				}
				a.observeLogo(result, template, frame, cfg, a.active.URL)
				if a.status.Mode != "monitoring" {
					t.Fatalf("frame %d switched: %+v", i+1, a.status)
				}
				if result.Decision == "uncertain" && (a.status.Misses != 0 || a.status.Matches != 0 || a.status.LogoPresent != nil) {
					t.Fatalf("uncertain frame %d retained switching counters", i+1)
				}
			}
		})
	}
}

func TestFailedSearchRetainsKnownPositionEvidence(t *testing.T) {
	template := dmaxFixtureTemplate(t) // No colour calibration: inspect the failed search.
	frame := dmaxFixtureFrame(t, "fall1/frame-08.jpg")
	var plane logoPlane
	result := template.find(frame, &plane, .5)
	if result.Decision != "missing" || result.Rect == template.rect || len(result.PositionChecks) != 1 {
		t.Fatalf("fixture no longer returns a background search candidate: %+v", result)
	}
	known := result.PositionChecks[0]
	if known.Result.Rect != template.rect || known.Result.Score != 0 || known.Result.RejectionReason != "insufficient_contour_coverage" || known.Contrast != nil {
		t.Fatalf("failed search lost original-position evidence: %+v", known)
	}
}

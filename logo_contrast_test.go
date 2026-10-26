package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestLocalLogoContrastRequiresHiddenAndVisibleSupport(t *testing.T) {
	template := calibratedColorTemplate(t, [3]byte{245, 245, 245}, .85)
	partial := coloredTestLogo([3]byte{245, 245, 245}, 8, .85)
	// Keep the left portion visible and hide the right against white sky.
	for y := 0; y < partial.H; y++ {
		for x := 15; x < partial.W; x++ {
			copy(partial.Pix[(y*partial.W+x)*3:][:3], []byte{245, 245, 245})
		}
	}
	check := template.checkLogoContrast(partial, template.rect)
	if check == nil || !check.Protected || check.VisibleCoverage == 0 || check.HiddenCoverage >= 1 {
		t.Fatalf("partially visible logo not protected: %+v", check)
	}
	// Remove the contours from the observable part, leaving the same sky.
	for y := 0; y < partial.H; y++ {
		for x := 0; x < 15; x++ {
			copy(partial.Pix[(y*partial.W+x)*3:][:3], []byte{40, 40, 40})
		}
	}
	check = template.checkLogoContrast(partial, template.rect)
	if check == nil || check.Protected {
		t.Fatalf("sky concealed missing contours in an observable area: %+v", check)
	}
	// A small matching patch cannot protect an otherwise missing logo.
	f := solidFrame(48, 36, 40)
	insertTestFrame(&f, solidFrame(12, 12, 245), Rect{0, 0, 12, 12})
	if template.logoColorHidden(f, template.rect) {
		t.Fatal("small bright patch protected a missing logo")
	}
}

func TestLocalLogoContrastToleratesOutliersButRejectsTexture(t *testing.T) {
	color := [3]float64{245, 245, 245}
	f := solidFrame(5, 5, 245)
	copy(f.Pix[:3], []byte{20, 20, 20})
	if !logoColorNeighborhoodHidden(f, 2, 2, color) {
		t.Fatal("one background outlier disabled low-contrast protection")
	}
	for i := 0; i < 12; i++ {
		copy(f.Pix[i*3:][:3], []byte{215, 215, 215})
	}
	if logoColorNeighborhoodHidden(f, 2, 2, color) {
		t.Fatal("two-colour texture with zero MAD was protected")
	}
}

func TestLocalLogoContrastNeverMovesOrTrainsOnUncertainFrames(t *testing.T) {
	template := calibratedColorTemplate(t, [3]byte{245, 245, 245}, .85)
	template.rect = Rect{8, 8, 48, 36}
	template.last = template.rect
	f := solidFrame(320, 180, 40)
	insertTestFrame(&f, solidFrame(48, 36, 245), template.rect)
	history := append([]Frame(nil), template.color.samples...)
	estimate := *template.color.estimate
	for i := 0; i < 4; i++ {
		var plane logoPlane
		result := template.find(f, &plane, .7)
		if result.Decision != "uncertain" || template.last != template.rect || template.lastScale != 1 {
			t.Fatalf("colour changed decision or tracked position: %+v", result)
		}
	}
	if !reflect.DeepEqual(history, template.color.samples) || estimate != *template.color.estimate {
		t.Fatal("uncertain frames changed colour learning")
	}
}

func TestKnownPositionChecksKeepTrackedAndOriginalEvidence(t *testing.T) {
	template := calibratedColorTemplate(t, [3]byte{245, 245, 245}, .85)
	template.rect = Rect{8, 8, 48, 36}
	template.last, template.lastScale = Rect{240, 20, 53, 40}, 1.1
	f := solidFrame(320, 180, 40)
	insertTestFrame(&f, solidFrame(48, 36, 245), template.rect)
	var plane logoPlane
	result := template.find(f, &plane, .7)
	if result.Decision != "uncertain" || result.Rect != template.rect || len(result.PositionChecks) != 2 {
		t.Fatalf("original position not protected alongside tracked position: %+v", result)
	}
	tracked, original := result.PositionChecks[0], result.PositionChecks[1]
	if tracked.Result.Rect != template.last || tracked.Result.Scale != 1.1 || tracked.Contrast == nil || tracked.Contrast.Protected {
		t.Fatalf("lost tracked-position evidence: %+v", tracked)
	}
	if original.Result.Rect != template.rect || original.Result.Scale != 1 || original.Contrast == nil || !original.Contrast.Protected {
		t.Fatalf("lost original-position evidence: %+v", original)
	}
}

func TestKnownPositionDiagnosticsSurviveSnapshotsAndExports(t *testing.T) {
	template := calibratedColorTemplate(t, [3]byte{245, 245, 245}, .85)
	a, cfg, _, _ := debugTestApp(t, true, false)
	cfg.ProgramCooldownSeconds, cfg.PlaceholderMinSeconds = 0, 0
	a.cfg = cfg
	f := solidFrame(48, 36, 40)
	var plane logoPlane
	result := template.find(f, &plane, .7)
	if len(result.PositionChecks) != 1 || result.PositionChecks[0].Result.RejectionReason != "insufficient_edge_energy" || result.PositionChecks[0].Contrast == nil || result.PositionChecks[0].Contrast.Protected {
		t.Fatalf("missing known-position evidence: %+v", result)
	}
	a.observeLogo(result, template, f, cfg, a.active.URL)
	stamp := a.logoDiagnostic.Time
	w := httptest.NewRecorder()
	a.handleLogoDiagnostic(w, httptest.NewRequest(http.MethodGet, "/api/logo-diagnostic?time="+url.QueryEscape(stamp), nil))
	var snapshot logoDiagnostic
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &snapshot) != nil || !reflect.DeepEqual(snapshot.Result.PositionChecks, result.PositionChecks) {
		t.Fatalf("live diagnostic lost position evidence: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	a.handleLogoDiagnostic(w, httptest.NewRequest(http.MethodPost, "/api/logo-diagnostic?time="+url.QueryEscape(stamp), strings.NewReader(`{"present":false}`)))
	assertPositionChecksInZIP(t, w.Body.Bytes(), result.PositionChecks, false)
	// Cause an actual automatic debug recording through the existing switch path.
	a.observeLogo(result, template, f, cfg, a.active.URL)
	capture := waitForDebugCapture(t, a)
	if !reflect.DeepEqual(capture.Frames[0].Result.PositionChecks, result.PositionChecks) {
		t.Fatal("saved debug metadata lost known-position evidence")
	}
	w = httptest.NewRecorder()
	a.handleDebugCapture(w, httptest.NewRequest(http.MethodGet, "/api/debug-captures/"+capture.ID+"/download", nil))
	assertPositionChecksInZIP(t, w.Body.Bytes(), result.PositionChecks, true)
	// A protected image cannot return to the channel while the filler plays.
	for i := 0; i < cfg.PresentFrames+1; i++ {
		plane = logoPlane{}
		hidden := solidFrame(48, 36, 245)
		protected := template.find(hidden, &plane, .7)
		a.observeLogo(protected, template, hidden, cfg, a.active.URL)
		if a.status.Mode != "placeholder" || a.status.Matches != 0 || a.status.Misses != 0 {
			t.Fatal("colour protection caused a return from the filler")
		}
	}
	// Previously recorded evidence remains unchanged by later evaluations.
	if !reflect.DeepEqual(a.diagnosticSnapshot(stamp).Result.PositionChecks, result.PositionChecks) {
		t.Fatal("later evaluation mutated an earlier snapshot")
	}
}

func assertPositionChecksInZIP(t *testing.T, data []byte, want []LogoPositionCheck, debug bool) {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range z.File {
		if file.Name != "metadata.json" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		var got []LogoPositionCheck
		if debug {
			var capture debugCapture
			if err := json.Unmarshal(b, &capture); err != nil || len(capture.Frames) == 0 {
				t.Fatalf("invalid debug metadata: %s", b)
			}
			got = capture.Frames[0].Result.PositionChecks
		} else {
			var diagnostic logoDiagnostic
			if err := json.Unmarshal(b, &diagnostic); err != nil {
				t.Fatal(err)
			}
			got = diagnostic.Result.PositionChecks
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ZIP metadata lost known-position evidence: %s", b)
		}
		return
	}
	t.Fatal("ZIP has no metadata.json")
}

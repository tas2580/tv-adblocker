package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func logoTestShape(x, y, w, h int) bool {
	x, y = x*48/w, y*36/h
	return x >= 8 && x < 14 && y >= 6 && y < 30 || y >= 23 && y < 30 && x >= 8 && x < 35 || x >= 29 && x < 36 && y >= 7 && y < 16
}

func compositeTestLogo(w, h, scene int, alpha float64, logo bool) Frame {
	f := Frame{w, h, make([]byte, w*h*3)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			bg := 110 + 55*math.Sin(float64(x+scene*9)/12) + 35*math.Cos(float64(y-scene*7)/9)
			v := bg
			if logo && logoTestShape(x, y, w, h) {
				v = alpha*245 + (1-alpha)*bg
			}
			for c := 0; c < 3; c++ {
				f.Pix[(y*w+x)*3+c] = byte(math.Max(0, math.Min(255, v)))
			}
		}
	}
	return f
}

func insertTestFrame(frame *Frame, logo Frame, r Rect) {
	for y := 0; y < r.H; y++ {
		for x := 0; x < r.W; x++ {
			from := (y*logo.H/r.H*logo.W + x*logo.W/r.W) * 3
			to := ((r.Y+y)*frame.W + r.X + x) * 3
			copy(frame.Pix[to:to+3], logo.Pix[from:from+3])
		}
	}
}

func explicitTestMask(w, h int) []byte {
	mask := make([]byte, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if logoTestShape(x, y, w, h) {
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						xx, yy := x+dx, y+dy
						if xx >= 0 && xx < w && yy >= 0 && yy < h {
							mask[yy*w+xx] = 255
						}
					}
				}
			}
		}
	}
	return mask
}

func TestMaskedLogoAcrossBackgroundsAndExposure(t *testing.T) {
	ref := compositeTestLogo(48, 36, 0, .8, true)
	template := prepareLogo("test", ref, Rect{W: 48, H: 36}, explicitTestMask(48, 36))
	for scene := 0; scene < 8; scene++ {
		f := compositeTestLogo(48, 36, scene, .8, true)
		r := template.compare(f, Rect{W: 48, H: 36}, 1, .8)
		if r.Decision != "present" {
			t.Errorf("scene %d: %+v", scene, r)
		}
		missing := template.compare(compositeTestLogo(48, 36, scene, .8, false), Rect{W: 48, H: 36}, 1, .8)
		if missing.Decision == "present" {
			t.Errorf("background accepted: %+v", missing)
		}
	}
	// Uniform exposure changes and opposite polarity must preserve the shape.
	for _, invert := range []bool{false, true} {
		f := ref.clone()
		for i, v := range f.Pix {
			if invert {
				f.Pix[i] = 255 - v
			} else {
				f.Pix[i] = byte(float64(v)*.7 + 25)
			}
		}
		if r := template.compare(f, Rect{W: 48, H: 36}, 1, .8); r.Decision != "present" {
			t.Errorf("exposure inversion %t: %+v", invert, r)
		}
	}
	noisy := ref.clone()
	for i, v := range noisy.Pix {
		delta := ((i*31 + i*i) % 11) - 5
		noisy.Pix[i] = byte(max(0, min(255, int(v)+delta)))
	}
	if r := template.compare(noisy, Rect{W: 48, H: 36}, 1, .8); r.Decision != "present" {
		t.Errorf("compression noise: %+v", r)
	}
}

func TestFilteredLogoPreservesThinFeaturesWhenShrinking(t *testing.T) {
	f := solidFrame(192, 48, 0)
	for y := 0; y < f.H; y++ {
		i := (y*f.W + 1) * 3
		f.Pix[i], f.Pix[i+1], f.Pix[i+2] = 255, 255, 255
	}
	p := filteredLogo(f, 96, 24)
	if p.lum[10*p.w] < 100 {
		t.Fatal("thin line lost while shrinking")
	}
}

func TestMaskedLogoSearchScalePositionAndNegativeExamples(t *testing.T) {
	ref := compositeTestLogo(48, 36, 0, 1, true)
	rect := Rect{245, 10, 48, 36}
	for _, target := range []Rect{{245, 10, 48, 36}, {209, 23, 48, 36}, {209, 23, 53, 40}, {209, 23, 43, 32}} {
		template := prepareLogo("test", ref, rect, explicitTestMask(48, 36))
		f := solidFrame(320, 180, 90)
		insertTestFrame(&f, ref, target)
		var plane logoPlane
		r := template.find(f, &plane, .8)
		if r.Decision != "present" || absInt(r.Rect.X-target.X) > 2 || absInt(r.Rect.Y-target.Y) > 2 {
			t.Errorf("target %+v: %+v", target, r)
		}
	}
	template := prepareLogo("test", ref, rect, explicitTestMask(48, 36))
	for _, kind := range []string{"flat", "line", "otherCorner", "textured"} {
		f := solidFrame(320, 180, 120)
		switch kind {
		case "line":
			for y := 0; y < f.H; y++ {
				for x := 263; x < 268; x++ {
					i := (y*f.W + x) * 3
					f.Pix[i], f.Pix[i+1], f.Pix[i+2] = 245, 245, 245
				}
			}
		case "otherCorner":
			insertTestFrame(&f, ref, Rect{10, 10, 48, 36})
		case "textured":
			for i := 0; i < len(f.Pix); i += 3 {
				v := byte((i*37 + i*i*3) % 251)
				f.Pix[i], f.Pix[i+1], f.Pix[i+2] = v, v, v
			}
		}
		var plane logoPlane
		if r := template.find(f, &plane, .8); r.Decision == "present" {
			t.Errorf("%s accepted: %+v", kind, r)
		}
	}
}

func TestTemporalLogoSuppressesMovingBackground(t *testing.T) {
	var frames []Frame
	for n := 0; n < 16; n++ {
		frames = append(frames, compositeTestLogo(48, 36, n, .8, true))
	}
	f, mask, err := temporalLogo(frames)
	if err != nil {
		t.Fatal(err)
	}
	template := prepareLogo("test", f, Rect{W: 48, H: 36}, mask)
	for n := 16; n < 21; n++ {
		r := template.compare(compositeTestLogo(48, 36, n, .8, true), Rect{W: 48, H: 36}, 1, .8)
		if r.Decision != "present" {
			t.Errorf("new scene %d: %+v", n, r)
		}
	}
	_, _, err = temporalLogo(frames[:3])
	if err == nil {
		t.Error("accepted too few frames")
	}
	for n := range frames {
		frames[n] = frames[0]
	}
	_, _, err = temporalLogo(frames)
	if err == nil {
		t.Error("accepted static scene")
	}
}

func TestMaskStorageLegacyAndEditorAPI(t *testing.T) {
	f := compositeTestLogo(48, 36, 0, 1, true)
	a := &App{dataDir: t.TempDir(), active: Channel{URL: "test://one"}}
	entry := ReferenceEntry{ID: "legacy", StreamURL: a.active.URL, Rect: Rect{W: 48, H: 36}}
	if err := a.addReference(entry, f); err != nil {
		t.Fatal(err)
	}
	mask, err := a.loadLogoMask(entry.ID, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.maskFile(entry.ID)); !os.IsNotExist(err) {
		t.Fatal("legacy reference was rewritten")
	}
	mask[0] = 123
	if err := a.saveLogoMask(entry.ID, f, mask); err != nil {
		t.Fatal(err)
	}
	got, err := a.loadLogoMask(entry.ID, f)
	if err != nil || !bytes.Equal(mask, got) {
		t.Fatalf("mask round trip: %v", err)
	}
	for _, view := range []string{"", "frame", "transparent"} {
		w := httptest.NewRecorder()
		a.handleLogoMask(w, httptest.NewRequest(http.MethodGet, "/api/logo-mask?id=legacy&view="+view, nil))
		if w.Code != 200 {
			t.Fatalf("view %s: %d %s", view, w.Code, w.Body.String())
		}
		if view == "transparent" {
			im, err := png.Decode(w.Body)
			if err != nil {
				t.Fatal(err)
			}
			_, _, _, alpha := im.At(0, 0).RGBA()
			if byte(alpha>>8) != 123 {
				t.Error("alpha lost")
			}
		}
	}
	data, _ := json.Marshal(map[string]any{"pixels": explicitTestMask(48, 36)})
	w := httptest.NewRecorder()
	a.handleLogoMask(w, httptest.NewRequest(http.MethodPut, "/api/logo-mask?id=legacy", bytes.NewReader(data)))
	if w.Code != 200 {
		t.Fatalf("edit: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	a.handleLogoMask(w, httptest.NewRequest(http.MethodPut, "/api/logo-mask?id=legacy", strings.NewReader(`{"pixels":"AA=="}`)))
	if w.Code != 400 {
		t.Fatal("accepted invalid dimensions")
	}
	a.active.URL = "test://other"
	w = httptest.NewRecorder()
	a.handleLogoMask(w, httptest.NewRequest(http.MethodPut, "/api/logo-mask?id=legacy", bytes.NewReader(data)))
	if w.Code != 404 {
		t.Fatal("edited other channel's reference")
	}
	a.active.URL = entry.StreamURL
	if err := a.removeReference(entry.ID, entry.StreamURL); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.referencesDir(), "legacy.mask.png")); !os.IsNotExist(err) {
		t.Fatal("orphan mask")
	}
}

func TestUncertainObservationsResetSwitchSeriesAndSnapshots(t *testing.T) {
	cfg := defaultConfig()
	a := &App{cfg: cfg, active: Channel{URL: "test://one"}, status: Status{Mode: "monitoring", Misses: 2, Matches: 2}}
	f := compositeTestLogo(48, 36, 0, 1, true)
	ref := prepareLogo("test", f, Rect{W: 48, H: 36}, explicitTestMask(48, 36))
	result := LogoResult{Decision: "uncertain", Score: .8, Rect: Rect{W: 48, H: 36}, ReferenceID: "test", Scale: 1}
	a.observeLogo(result, ref, f, cfg, a.active.URL)
	if a.status.LogoPresent != nil || a.status.Matches != 0 || a.status.Misses != 0 || a.status.Mode != "monitoring" {
		t.Fatalf("uncertain observation: %+v", a.status)
	}
	stamp := a.logoDiagnostic.Time
	result.Score = .81
	a.observeLogo(result, ref, f, cfg, a.active.URL)
	if a.diagnosticSnapshot(stamp) == nil {
		t.Fatal("snapshot replaced")
	}
	w := httptest.NewRecorder()
	a.handleLogoDiagnostic(w, httptest.NewRequest(http.MethodPost, "/api/logo-diagnostic?time="+url.QueryEscape(stamp), strings.NewReader(`{"present":false}`)))
	if w.Code != 200 {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 3 {
		t.Fatal("missing export files")
	}
	for _, score := range []float64{.78, .8, .82} {
		if logoDecision(score, .8) != "uncertain" {
			t.Errorf("%f not uncertain", score)
		}
	}
	if logoDecision(.769, .8) != "missing" || logoDecision(.77, .8) != "uncertain" || logoDecision(.83, .8) != "present" {
		t.Fatal("threshold boundaries")
	}
}

func TestCaptureReusesDecoderAndAbortsChannelChange(t *testing.T) {
	a, pids := previewTestApp(t)
	previewSession(t, a, "one", true)
	waitForPreview(t, func() bool { return previewHasFrame(a) })
	w := httptest.NewRecorder()
	a.handleLogoCapture(w, httptest.NewRequest(http.MethodPost, "/api/logo-capture", strings.NewReader(`{"w":48,"h":36}`)))
	if w.Code != 200 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	if len(pids()) != 1 {
		t.Fatalf("extra decoder: %v", pids())
	}
	a.mu.Lock()
	capture := a.logoCapture
	start := capture.Started
	for n := 1; n < 16; n++ {
		f := compositeTestLogo(48, 36, n, .8, true)
		full := solidFrame(frameWidth, frameHeight, 50)
		insertTestFrame(&full, f, capture.Rect)
		a.sampleLogoCaptureLocked(full, start.Add(time.Duration(n)*time.Second))
	}
	a.mu.Unlock()
	a.finishLogoCapture(capture)
	a.mu.RLock()
	state := capture.State
	a.mu.RUnlock()
	if state != "ready" {
		t.Fatalf("capture state: %s, %s", state, capture.Error)
	}
	a.setActiveChannel(Channel{Name: "Other", URL: "rtsp://tv.test/other"})
	a.mu.RLock()
	defer a.mu.RUnlock()
	if capture.State != "error" || capture.Error != "capture.channelChanged" {
		t.Fatalf("capture survived channel change: %+v", capture)
	}
}

func TestReferenceReloadPreservesPlaybackAndProtection(t *testing.T) {
	a, _ := previewTestApp(t)
	addTestReference(t, a, "existing", a.active.URL)
	a.mu.Lock()
	a.status.Mode = "placeholder"
	a.status.Matches, a.status.Misses = 2, 2
	until := time.Now().Add(time.Minute)
	a.placeholderMinUntil = until
	a.mu.Unlock()
	a.restartMonitor()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.status.Mode != "placeholder" || !a.placeholderMinUntil.Equal(until) || a.status.Matches != 0 || a.status.Misses != 0 {
		t.Fatalf("reload changed playback/protection: %+v", a.status)
	}
}

func TestCapturedMaskEditsAndAutomaticProposalSurviveSave(t *testing.T) {
	f := compositeTestLogo(48, 36, 0, 1, true)
	proposal := explicitTestMask(48, 36)
	a := &App{cfg: defaultConfig(), dataDir: t.TempDir(), active: Channel{URL: "test://one"}, status: Status{Mode: "idle"}}
	a.logoCapture = &logoCapture{ID: "series", State: "ready", stream: a.active, Rect: Rect{W: 48, H: 36}, frame: f, mask: proposal, automatic: append([]byte(nil), proposal...)}
	edited := append([]byte(nil), proposal...)
	edited[0] = 123
	data, _ := json.Marshal(map[string]any{"pixels": edited})
	w := httptest.NewRecorder()
	a.handleLogoMask(w, httptest.NewRequest(http.MethodPut, "/api/logo-mask?capture=series", bytes.NewReader(data)))
	if w.Code != 200 {
		t.Fatalf("edit draft: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	a.handleLogoCapture(w, httptest.NewRequest(http.MethodPost, "/api/logo-capture?id=series", nil))
	if w.Code != 200 {
		t.Fatalf("save draft: %d %s", w.Code, w.Body.String())
	}
	got, err := a.loadLogoMask("series", f)
	if err != nil || !bytes.Equal(got, edited) {
		t.Fatalf("saved mask: %v", err)
	}
	original, err := loadLogoMaskFile(a.automaticMaskFile("series"), f)
	if err != nil || !bytes.Equal(original, proposal) {
		t.Fatalf("lost proposal: %v", err)
	}
	w = httptest.NewRecorder()
	a.handleLogoMask(w, httptest.NewRequest(http.MethodPut, "/api/logo-mask?id=series", strings.NewReader(`{"reset":true}`)))
	if w.Code != 200 {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
	got, err = a.loadLogoMask("series", f)
	if err != nil || !bytes.Equal(got, proposal) {
		t.Fatalf("reset proposal: %v", err)
	}
	refs, err := a.referencesFor(a.active.URL)
	if err != nil || len(refs) != 1 || refs[0].MaskSource != "series" {
		t.Fatalf("reference metadata: %+v %v", refs, err)
	}
	if a.status.Running || a.cancel != nil {
		t.Fatal("saving mask restarted stopped detection")
	}
}

func BenchmarkMaskedLogoSearchFiveReferences(b *testing.B) {
	refs := make([]*logoTemplate, 5)
	for i := range refs {
		rect := Rect{990 + i*3, 50 + i*2, 48, 36}
		f := compositeTestLogo(48, 36, i, .8, true)
		refs[i] = prepareLogo("test", f, rect, explicitTestMask(48, 36))
	}
	frame := solidFrame(frameWidth, frameHeight, 90)
	for i := 0; i < len(frame.Pix); i += 3 {
		v := byte((i*37 + i*i*3) % 251)
		frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2] = v, v, v
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		var plane logoPlane
		for _, ref := range refs {
			ref.find(frame, &plane, .8)
		}
	}
}

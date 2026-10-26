package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func debugTestApp(t *testing.T, enabled bool, failOpen bool) (*App, Config, *logoTemplate, Frame) {
	t.Helper()
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		var result any = []any{}
		if request.Method == "Player.Open" {
			if failOpen {
				return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("failed"))}, nil
			}
			result = "OK"
		}
		body, _ := json.Marshal(map[string]any{"result": result})
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}
	cfg := defaultConfig()
	cfg.DebugEnabled = enabled
	cfg.MissingFrames = 2
	cfg.Kodi = KodiConfig{URL: "http://kodi.test/jsonrpc", Password: "secret"}
	cfg.PlaceholderSource = "kodi-local"
	cfg.KodiPlaceholder = "file:///placeholder.mp4"
	a := &App{cfg: cfg, dataDir: t.TempDir(), active: Channel{Name: "Test", URL: "http://tv.test/stream?token=secret"}, status: Status{Mode: "monitoring"}, httpClient: client}
	frame := compositeTestLogo(48, 36, 0, .8, true)
	ref := prepareLogo("reference", frame, Rect{W: 48, H: 36}, explicitTestMask(48, 36))
	if len(ref.features) < 8 || ref.cells < 3 {
		t.Fatal("test reference is unusable")
	}
	return a, cfg, ref, frame
}

func debugMissingResult() LogoResult {
	return LogoResult{Decision: "missing", ReferenceID: "reference", Rect: Rect{W: 48, H: 36}, Score: .3, Threshold: .8, EdgeScore: .2, StructureScore: .4, Coverage: .5}
}

func waitForDebugCapture(t *testing.T, a *App) *debugCapture {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(a.debugDir())
		for _, entry := range entries {
			if debugCaptureIDPattern.MatchString(entry.Name()) {
				capture, err := a.readDebugCapture(entry.Name())
				if err != nil {
					t.Fatal(err)
				}
				return capture
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("debug capture was not saved")
	return nil
}

func TestDebugCaptureFollowsSuccessfulAutomaticSwitch(t *testing.T) {
	a, cfg, ref, frame := debugTestApp(t, true, false)
	a.observeLogo(debugMissingResult(), ref, frame, cfg, a.active.URL)
	if entries, _ := os.ReadDir(a.debugDir()); len(entries) != 0 {
		t.Fatal("capture saved before the required missing frame count")
	}
	a.observeLogo(debugMissingResult(), ref, frame, cfg, a.active.URL)
	capture := waitForDebugCapture(t, a)
	if capture.Direction != debugToPlaceholder || capture.Channel != "Test" || capture.MissingFrames != 2 || capture.SimilarityMinimum != cfg.SimilarityMinimum || len(capture.Frames) != 2 {
		t.Fatalf("unexpected metadata: %+v", capture)
	}
	if capture.Frames[0].Misses != 1 || capture.Frames[1].Misses != 2 || capture.Frames[1].Result.Score != .3 {
		t.Fatalf("frame and decision mismatch: %+v", capture.Frames)
	}
	metadata, err := os.ReadFile(filepath.Join(a.debugDir(), capture.ID, "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(metadata, []byte("token=secret")) || bytes.Contains(metadata, []byte("Password")) || bytes.Contains(metadata, []byte("secret")) {
		t.Fatal("capture leaked stream URL or password")
	}
	mux := http.NewServeMux()
	a.routes(mux)
	for _, path := range []string{"/api/debug-captures", "/api/debug-captures/" + capture.ID, "/api/debug-captures/" + capture.ID + "/frames/1"} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 200 {
			t.Fatalf("GET %s: %d: %s", path, w.Code, w.Body.String())
		}
		if strings.HasSuffix(path, "/frames/1") {
			image, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes()))
			if err != nil || image.Bounds().Dx() != frame.W || image.Bounds().Dy() != frame.H {
				t.Fatalf("saved frame image: %v, %v", image, err)
			}
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/debug-captures/"+capture.ID+"/download", nil))
	if w.Code != 200 {
		t.Fatalf("download: %d", w.Code)
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil || len(z.File) != 3 {
		t.Fatalf("ZIP files: %v, err: %v", z, err)
	}
}

func TestDebugCaptureFollowsSuccessfulAutomaticReturn(t *testing.T) {
	a, cfg, ref, frame := debugTestApp(t, true, false)
	a.status.Mode = "placeholder"
	a.placeholderMinUntil = time.Now().Add(time.Minute)
	present := debugMissingResult()
	present.Decision, present.Score = "present", .95
	for i := 0; i < 2; i++ {
		a.observeLogo(present, ref, frame, cfg, a.active.URL)
	}
	if a.status.Matches != 0 || a.status.Mode != "placeholder" {
		t.Fatal("placeholder minimum did not prevent automatic return")
	}
	a.placeholderMinUntil = time.Time{}
	for i := 1; i < cfg.PresentFrames; i++ {
		a.observeLogo(present, ref, frame, cfg, a.active.URL)
	}
	if entries, _ := os.ReadDir(a.debugDir()); len(entries) != 0 {
		t.Fatal("capture saved before the required present frame count")
	}
	a.observeLogo(present, ref, frame, cfg, a.active.URL)
	capture := waitForDebugCapture(t, a)
	if capture.Direction != debugToSender || capture.PresentFrames != cfg.PresentFrames || len(capture.Frames) != 2+cfg.PresentFrames {
		t.Fatalf("unexpected return metadata: %+v", capture)
	}
	for i, sample := range capture.Frames {
		want := max(0, i-1)
		if sample.Matches != want || sample.Misses != 0 || sample.Result.Decision != "present" || sample.Result.Score != .95 {
			t.Fatalf("return frame %d: %+v, want %d matches", i, sample, want)
		}
	}
	if a.status.Mode != "sender" || len(a.debugFrames) != 0 {
		t.Fatal("automatic return did not start a fresh debug frame history")
	}
	// The snapshot must keep its frames after recordPlayback clears the buffer.
	w := httptest.NewRecorder()
	a.handleDebugCapture(w, httptest.NewRequest(http.MethodGet, "/api/debug-captures/"+capture.ID+"/frames/4", nil))
	if _, err := jpeg.Decode(bytes.NewReader(w.Body.Bytes())); w.Code != 200 || err != nil {
		t.Fatalf("return frame image: status %d, %v", w.Code, err)
	}
}

func TestDebugCaptureReturnRequiresEnabledSuccessfulAutomaticSwitch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		failOpen bool
		manual   bool
	}{
		{name: "disabled", enabled: false},
		{name: "failed return", enabled: true, failOpen: true},
		{name: "manual placeholder", enabled: true, manual: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, cfg, ref, frame := debugTestApp(t, tc.enabled, tc.failOpen)
			a.status.Mode, a.status.ManualPlaceholder = "placeholder", tc.manual
			present := debugMissingResult()
			present.Decision = "present"
			for i := 0; i < cfg.PresentFrames; i++ {
				a.observeLogo(present, ref, frame, cfg, a.active.URL)
			}
			if tc.manual {
				w := httptest.NewRecorder()
				a.handlePlaySender(w, httptest.NewRequest(http.MethodPost, "/api/play/sender", nil))
				if w.Code != 200 {
					t.Fatalf("manual return: %d: %s", w.Code, w.Body.String())
				}
			}
			if entries, _ := os.ReadDir(a.debugDir()); len(entries) != 0 {
				t.Fatalf("unexpected return capture: %v", entries)
			}
		})
	}
}

func TestDebugCaptureLegacyDirection(t *testing.T) {
	a, _, _, _ := debugTestApp(t, true, false)
	id := "20261022T120000.000000000Z"
	dir := filepath.Join(a.debugDir(), id)
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(`{"id":"`+id+`","frames":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/debug-captures", "/api/debug-captures/" + id} {
		w := httptest.NewRecorder()
		if path == "/api/debug-captures" {
			a.handleDebugCaptures(w, httptest.NewRequest(http.MethodGet, path, nil))
		} else {
			a.handleDebugCapture(w, httptest.NewRequest(http.MethodGet, path, nil))
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"direction":"to-placeholder"`) {
			t.Fatalf("legacy direction for %s: %d: %s", path, w.Code, w.Body.String())
		}
	}
}

func TestDebugCaptureRequiresEnabledSuccessfulAutomaticSwitch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		failOpen bool
	}{
		{"disabled", false, false},
		{"failed switch", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, cfg, ref, frame := debugTestApp(t, tc.enabled, tc.failOpen)
			a.observeLogo(debugMissingResult(), ref, frame, cfg, a.active.URL)
			a.observeLogo(debugMissingResult(), ref, frame, cfg, a.active.URL)
			if entries, _ := os.ReadDir(a.debugDir()); len(entries) != 0 {
				t.Fatalf("unexpected debug files: %v", entries)
			}
		})
	}
	a, _, _, frame := debugTestApp(t, true, false)
	a.rememberDebugFrameLocked(debugMissingResult(), frame)
	if err := a.play("placeholder", false); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(a.debugDir()); len(entries) != 0 {
		t.Fatalf("manual switch created capture: %v", entries)
	}
}

func TestDebugCapturePrunesAndResetsOnChannelChange(t *testing.T) {
	a, _, _, frame := debugTestApp(t, true, false)
	for i := 0; i < 11; i++ {
		a.rememberDebugFrameLocked(debugMissingResult(), frame)
	}
	if len(a.debugFrames) != debugFrameLimit {
		t.Fatalf("retained %d in-memory frames", len(a.debugFrames))
	}
	a.setActiveChannel(Channel{Name: "Other", URL: "http://tv.test/other"})
	if len(a.debugFrames) != 0 {
		t.Fatal("channel change retained previous frames")
	}
	for i := 0; i < 11; i++ {
		id := fmt.Sprintf("20261022T1200%02d.000000000Z", i)
		capture := &debugCapture{ID: id, Time: time.Now().Format(time.RFC3339), Frames: []debugFrame{{Image: "frame-01.jpg", frame: frame}}}
		if err := a.saveDebugCapture(capture); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(a.debugDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != debugCaptureLimit {
		t.Fatalf("retained %d captures", len(entries))
	}
	if _, err := a.readDebugCapture("20261022T120000.000000000Z"); !os.IsNotExist(err) {
		t.Fatalf("oldest capture was not removed: %v", err)
	}
}

func TestDisablingDebugClearsBufferedFrames(t *testing.T) {
	a, _, _, frame := debugTestApp(t, true, false)
	a.configPath = filepath.Join(a.dataDir, "config.json")
	a.rememberDebugFrameLocked(debugMissingResult(), frame)
	next := a.cfg
	next.DebugEnabled = false
	body, _ := json.Marshal(next)
	w := httptest.NewRecorder()
	a.handleConfig(w, httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body)))
	if w.Code != 200 || len(a.debugFrames) != 0 {
		t.Fatalf("disabled debug retained frames: status %d, frames %d", w.Code, len(a.debugFrames))
	}
}

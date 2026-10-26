package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestObservationRecordingLabelsExportAndReplay(t *testing.T) {
	cfg := detectionTestConfig()
	cfg.DetectionMode = "compare"
	a := &App{cfg: cfg, dataDir: t.TempDir(), active: Channel{Name: "Test", URL: "http://test/stream?token=secret"}, status: Status{Running: true}, lastFrame: solidFrame(64, 36, 80)}
	w := httptest.NewRecorder()
	a.handleObservationRecording(w, httptest.NewRequest("POST", "/api/detection/recording", strings.NewReader(`{"action":"start"}`)))
	if w.Code != 200 || a.recording.Purpose != "validation" {
		t.Fatal(w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	a.handleStatus(w, httptest.NewRequest("GET", "/api/status", nil))
	var snapshot struct {
		Recording *ObservationRecordingStatus `json:"recording"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Recording == nil || snapshot.Recording.State != "recording" || snapshot.Recording.ID != a.recording.ID {
		t.Fatalf("active recording missing from status: %+v", snapshot.Recording)
	}
	a.mu.Lock()
	for i := 0; i < 6; i++ {
		o := testObservation(float64(i*2+50), "present")
		a.evaluateDetectionLocked(o.Logo, cfg, &o)
	}
	a.mu.Unlock()
	w = httptest.NewRecorder()
	a.handleObservationRecording(w, httptest.NewRequest("POST", "/api/detection/recording", strings.NewReader(`{"action":"stop"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	a.handleStatus(w, httptest.NewRequest("GET", "/api/status", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Recording == nil || snapshot.Recording.State != "ready" {
		t.Fatalf("finished recording missing from status: %+v", snapshot.Recording)
	}
	id := a.recording.ID
	if a.recording.Observations[0].Time != 0 || a.recording.Observations[5].Time != 10 {
		t.Fatal("recording timeline not normalized")
	}
	w = httptest.NewRecorder()
	a.handleObservationRecording(w, httptest.NewRequest("PUT", "/api/detection/recording?id="+id, strings.NewReader(`{"labels":[{"start":0,"end":10,"kind":"program"}]}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	a.handleObservationRecording(w, httptest.NewRequest("GET", "/api/detection/recording?id="+id+"&download=1", nil))
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(z.File) != 7 {
		t.Fatalf("archive has %d files", len(z.File))
	}
	path := filepath.Join(t.TempDir(), "recording.zip")
	if err := os.WriteFile(path, w.Body.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	var report bytes.Buffer
	if err := runDetectionReplay(path, "", "", "", a.dataDir, &report); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(report.String(), "secret") || strings.Contains(report.String(), "http://test") {
		t.Fatal("stream credentials exported")
	}
	var result ReplayReport
	if err := json.Unmarshal(report.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Classic.ProgramHours <= 0 || result.Classic.FalseSwitches != 0 {
		t.Fatalf("metrics: %+v", result)
	}
	w = httptest.NewRecorder()
	a.handleObservationRecording(w, httptest.NewRequest("GET", "/api/detection/recording?id="+id+"&frame=../config.json", nil))
	if w.Code != 404 {
		t.Fatal("unsafe frame path accepted")
	}
}
func TestValidationRecordingFreezesLearningAndDiscovery(t *testing.T) {
	cfg := detectionTestConfig()
	cfg.DetectionMode = "compare"
	a := &App{cfg: cfg, dataDir: t.TempDir(), active: Channel{URL: "local://test"}, status: Status{Running: true}, recording: &ObservationRecording{Purpose: "validation", State: "recording", started: time.Now(), Settings: replaySettings(cfg)}}
	stamp := 0.
	for cycle := 0; cycle < 6; cycle++ {
		for _, decision := range []string{"present", "present", "present", "missing", "missing", "missing", "missing", "missing", "present", "present", "present"} {
			o := testObservation(stamp, decision)
			a.mu.Lock()
			a.evaluateDetectionLocked(o.Logo, cfg, &o)
			a.mu.Unlock()
			a.processAutomaticFrame(context.Background(), autoTestFrame(int(stamp/2), true), cfg, a.active, nil, &o)
			stamp += 2
		}
	}
	if a.profiles[a.active.URL].Breaks != 0 || a.detection.automatic != nil {
		t.Fatal("validation data adapted detector")
	}
}
func TestHybridCooldownCancellationAndCanceledSample(t *testing.T) {
	cfg := detectionTestConfig()
	cfg.DetectionMode = "hybrid"
	cfg.PauseDetectionDuringProgramLock = false
	a := &App{cfg: cfg, dataDir: t.TempDir(), status: Status{Running: true}, programCooldownUntil: time.Now().Add(time.Minute), active: Channel{URL: "local://test"}}
	a.detection.hybrid = decisionMachine{cooldown: 600, initialized: true, session: 1}
	w := httptest.NewRecorder()
	a.handleCancelProgramCooldown(w, httptest.NewRequest(http.MethodPost, "/api/cancel-program-cooldown", nil))
	if w.Code != 200 || a.detection.hybrid.cooldown != 0 {
		t.Fatal("hybrid retained canceled protection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := testObservation(0, "missing")
	o.ctx = ctx
	a.observeLogoSample(o.Logo, nil, Frame{}, cfg, a.active.URL, &o)
	if a.status.Detection != nil {
		t.Fatal("canceled decoder published a decision")
	}
}

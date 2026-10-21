package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCancelProgramCooldown(t *testing.T) {
	// A resumed monitor must not open a real stream in this test.
	t.Setenv("PATH", t.TempDir())
	for _, mode := range []string{"paused", "running", "stopped"} {
		t.Run(mode, func(t *testing.T) {
			started := time.Now().Add(-time.Minute)
			a := &App{
				cfg: defaultConfig(), dataDir: t.TempDir(),
				active:               Channel{Name: "ZDF", URL: "rtsp://tv.test/zdf"},
				status:               Status{Mode: "sender", Running: mode == "running", Matches: 3, Misses: 2},
				programCooldownUntil: time.Now().Add(5 * time.Minute),
				programStartedAt:     started,
			}
			addTestReference(t, a, "test", a.active.URL)
			t.Cleanup(func() {
				a.handleStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/stop", nil))
			})
			timerCanceled := false
			if mode == "paused" {
				if err := a.startMonitor(); err != nil {
					t.Fatal(err)
				}
				a.mu.Lock()
				cancel := a.resumeCancel
				a.resumeCancel = func() { timerCanceled = true; cancel() }
				a.mu.Unlock()
			}
			mux := http.NewServeMux()
			a.routes(mux)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/cancel-program-cooldown", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("cancel: %d %s", response.Code, response.Body.String())
			}
			a.mu.RLock()
			if !a.programCooldownUntil.IsZero() || a.status.DetectionPaused || a.resumeCancel != nil {
				t.Error("protection or scheduled resume remained active")
			}
			if a.status.Running != (mode != "stopped") {
				t.Errorf("running state = %v", a.status.Running)
			}
			if mode == "paused" && (a.cancel == nil || !timerCanceled) {
				t.Error("paused detector was not resumed or timer not canceled")
			}
			if a.cfg.ProgramCooldownSeconds != 600 || !a.programStartedAt.Equal(started) {
				t.Error("configuration or programme timer changed")
			}
			if a.status.Matches != 0 || a.status.Misses != 0 {
				t.Error("old detector counts were retained")
			}
			resumeID := a.resumeID
			a.mu.RUnlock()

			// Repeating the request is harmless and does not restart detection.
			response = httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/cancel-program-cooldown", nil))
			a.mu.RLock()
			if a.resumeID != resumeID {
				t.Error("repeated cancellation restarted the detector")
			}
			a.mu.RUnlock()
			response = httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/status", nil))
			var status struct {
				Status Status `json:"status"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
				t.Fatal(err)
			}
			if status.Status.ProgramCooldownActive || status.Status.ProgramCooldownRemaining != 0 {
				t.Fatal("API still reports active protection")
			}

			// The next automatic return uses the configured protection again.
			a.recordPlayback("sender", a.cfg, true)
			a.mu.RLock()
			if !a.programCooldownUntil.After(time.Now().Add(9 * time.Minute)) {
				t.Error("future automatic protection was disabled")
			}
			a.mu.RUnlock()
		})
	}
}

func TestCancelProgramCooldownRequiresPOST(t *testing.T) {
	deadline := time.Now().Add(time.Minute)
	a := &App{programCooldownUntil: deadline}
	response := httptest.NewRecorder()
	a.handleCancelProgramCooldown(response, httptest.NewRequest(http.MethodGet, "/api/cancel-program-cooldown", nil))
	if response.Code != http.StatusMethodNotAllowed || !a.programCooldownUntil.Equal(deadline) {
		t.Fatal("GET canceled protection")
	}
}

func TestCancelProgramCooldownRestoresDetection(t *testing.T) {
	a := &App{status: Status{Mode: "monitoring", Running: true}, programCooldownUntil: time.Now().Add(time.Minute)}
	cfg := Config{MissingFrames: 2, PresentFrames: 2}
	a.observe(false, 0, cfg)
	if a.status.Misses != 0 {
		t.Fatal("detection ignored active protection")
	}
	a.handleCancelProgramCooldown(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/cancel-program-cooldown", nil))
	a.observe(false, 0, cfg)
	if a.status.Misses != 1 {
		t.Fatal("detection remained blocked after cancellation")
	}
}

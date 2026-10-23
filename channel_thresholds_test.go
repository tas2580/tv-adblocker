package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChannelSimilarityConfigPersistenceAndFallback(t *testing.T) {
	a := &App{cfg: defaultConfig(), dataDir: t.TempDir()}
	a.configPath = filepath.Join(a.dataDir, "config.json")
	next := a.cfg
	next.SimilarityMinimum = .83
	next.ChannelSimilarityMinimum = map[string]float64{"http://tv.test/a": .72, "http://tv.test/absent": 1}
	body, err := json.Marshal(next)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.handleConfig(w, httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d: %s", w.Code, w.Body.String())
	}
	loaded := &App{cfg: defaultConfig(), configPath: a.configPath}
	if err := loaded.loadConfig(); err != nil {
		t.Fatal(err)
	}
	if got := loaded.cfg.similarityMinimumFor("http://tv.test/a"); got != .72 {
		t.Fatalf("saved override = %v", got)
	}
	if got := loaded.cfg.similarityMinimumFor("http://tv.test/absent"); got != 1 {
		t.Fatalf("channel absent from playlist lost its override: %v", got)
	}
	if got := loaded.cfg.similarityMinimumFor("http://tv.test/b"); got != .83 {
		t.Fatalf("default = %v", got)
	}
	loaded.cfg.SimilarityMinimum = .91
	if got := loaded.cfg.similarityMinimumFor("http://tv.test/b"); got != .91 {
		t.Fatalf("changed default = %v", got)
	}
	if got := loaded.cfg.similarityMinimumFor("http://tv.test/a"); got != .72 {
		t.Fatalf("default change replaced override: %v", got)
	}
	delete(loaded.cfg.ChannelSimilarityMinimum, "http://tv.test/a")
	if got := loaded.cfg.similarityMinimumFor("http://tv.test/a"); got != .91 {
		t.Fatalf("cleared override = %v", got)
	}
	w = httptest.NewRecorder()
	a.handleStatus(w, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	var status struct{ Config Config }
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil || status.Config.ChannelSimilarityMinimum["http://tv.test/a"] != .72 {
		t.Fatalf("status lost channel overrides: %s, %v", w.Body.String(), err)
	}

	legacy := defaultConfig()
	if err := json.Unmarshal([]byte(`{"similarity_minimum":0.77}`), &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.normalize()
	if err := legacy.validateChannelSimilarity(); err != nil || legacy.similarityMinimumFor("http://tv.test/a") != .77 {
		t.Fatalf("legacy configuration no longer inherits the default: %+v, %v", legacy, err)
	}
}

func TestChannelSimilarityRejectsInvalidConfigWithoutChanges(t *testing.T) {
	for _, value := range []string{"0", "-0.1", "1.01", "null", `"0.7"`} {
		t.Run(value, func(t *testing.T) {
			cancelled := false
			a := &App{cfg: defaultConfig(), status: Status{Running: true}, cancel: func() { cancelled = true }}
			a.cfg.ChannelSimilarityMinimum = map[string]float64{"http://tv.test/a": .72}
			a.configPath = filepath.Join(t.TempDir(), "config.json")
			if err := a.saveConfig(); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(a.configPath)
			w := httptest.NewRecorder()
			body := `{"channel_similarity_minimum":{"http://tv.test/a":` + value + `}}`
			a.handleConfig(w, httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body)))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("invalid config accepted: %d %s", w.Code, w.Body.String())
			}
			after, _ := os.ReadFile(a.configPath)
			if cancelled || !a.status.Running || a.cfg.ChannelSimilarityMinimum["http://tv.test/a"] != .72 || !bytes.Equal(before, after) {
				t.Fatal("invalid input changed the running configuration or saved file")
			}
		})
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		c := defaultConfig()
		c.ChannelSimilarityMinimum = map[string]float64{"http://tv.test/a": value}
		if c.validateChannelSimilarity() == nil {
			t.Fatalf("non-finite threshold accepted: %v", value)
		}
	}
	c := defaultConfig()
	c.ChannelSimilarityMinimum = map[string]float64{"http://tv.test/a": .001, "http://tv.test/b": 1}
	if err := c.validateChannelSimilarity(); err != nil {
		t.Fatalf("valid boundary values rejected: %v", err)
	}
}

func TestMonitorUsesChannelSimilarity(t *testing.T) {
	dir := t.TempDir()
	frame := solidFrame(frameWidth, frameHeight, 90)
	ref := compositeTestLogo(48, 36, 0, .8, true)
	rect := Rect{10, 10, 48, 36}
	insertTestFrame(&frame, ref, rect)
	framePath := filepath.Join(dir, "frame.rgb")
	if err := os.WriteFile(framePath, frame.Pix, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nexec /bin/cat \"$TEST_FRAME\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TEST_FRAME", framePath)
	for _, tc := range []struct {
		name, url string
		want      float64
	}{
		{"override", "http://tv.test/a", .72},
		{"default", "http://tv.test/b", .91},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaultConfig()
			cfg.SimilarityMinimum = .91
			cfg.ChannelSimilarityMinimum = map[string]float64{"http://tv.test/a": .72}
			a := &App{cfg: cfg, dataDir: t.TempDir(), active: Channel{Name: "Same name", URL: tc.url}, status: Status{Running: true, Mode: "monitoring"}}
			if err := a.addMaskedReference(ReferenceEntry{ID: "test", StreamURL: tc.url, Rect: rect}, ref, explicitTestMask(48, 36)); err != nil {
				t.Fatal(err)
			}
			if err := a.monitorOnce(context.Background()); !errors.Is(err, io.EOF) {
				t.Fatalf("single-frame decoder: %v", err)
			}
			result := a.status.LogoDetection
			if result == nil || result.Threshold != tc.want || result.Decision != "present" {
				t.Fatalf("detector did not apply threshold %v: %+v", tc.want, result)
			}
		})
	}
}

func TestDebugCaptureUsesChannelSimilarity(t *testing.T) {
	a, cfg, ref, frame := debugTestApp(t, true, false)
	cfg.ChannelSimilarityMinimum = map[string]float64{a.active.URL: .71}
	a.cfg = cfg
	result := debugMissingResult()
	result.Threshold = .71
	a.observeLogo(result, ref, frame, cfg, a.active.URL)
	a.observeLogo(result, ref, frame, cfg, a.active.URL)
	capture := waitForDebugCapture(t, a)
	if capture.SimilarityMinimum != .71 || capture.Frames[0].Result.Threshold != .71 {
		t.Fatalf("capture contains the global default instead of the override: %+v", capture)
	}
}

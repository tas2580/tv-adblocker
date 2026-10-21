package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func addTestReference(t *testing.T, a *App, id, streamURL string) {
	t.Helper()
	err := a.addReference(ReferenceEntry{ID: id, StreamURL: streamURL, Rect: Rect{W: 8, H: 8}}, Frame{8, 8, make([]byte, 8*8*3)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMonitorWithoutChannelReferences(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a := &App{cfg: defaultConfig(), dataDir: t.TempDir()}
	addTestReference(t, a, "other", "rtsp://tv.test/other")
	a.setActiveChannel(Channel{Name: "Test", URL: "rtsp://tv.test/test"})
	// Repeated starts and an existing programme lock must not start a worker
	// or schedule one while the selected channel has no references.
	a.programCooldownUntil = time.Now().Add(time.Minute)
	if err := a.startMonitor(); err != nil {
		t.Fatal(err)
	}
	if a.status.Running || a.status.DetectionPaused || a.cancel != nil || a.resumeCancel != nil || a.status.Mode != "setup" {
		t.Fatalf("detector started without references: %+v", a.status)
	}
	// The worker itself must also avoid launching ffmpeg (which is absent).
	if err := a.monitorOnce(context.Background()); err != nil {
		t.Fatalf("worker attempted decoding without references: %v", err)
	}
}

func TestFirstAndLastReferenceControlMonitor(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	a := &App{cfg: defaultConfig(), dataDir: t.TempDir(), active: Channel{Name: "Test", URL: "rtsp://tv.test/test"}}
	t.Cleanup(func() { a.handleStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/stop", nil)) })
	if err := a.startMonitor(); err != nil {
		t.Fatal(err)
	}
	a.previewFrame = Frame{8, 8, make([]byte, 8*8*3)}
	response := httptest.NewRecorder()
	a.handleReference(response, httptest.NewRequest(http.MethodPost, "/api/reference", strings.NewReader(`{"w":8,"h":8}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("save reference: %d %s", response.Code, response.Body.String())
	}
	a.mu.RLock()
	running := a.status.Running && a.cancel != nil
	a.mu.RUnlock()
	if !running {
		t.Fatal("first reference did not start detection")
	}
	refs, err := a.referencesFor(a.active.URL)
	if err != nil || len(refs) != 1 {
		t.Fatalf("references = %v, %v", refs, err)
	}
	response = httptest.NewRecorder()
	a.handleReferenceItem(response, httptest.NewRequest(http.MethodDelete, "/api/references/"+refs[0].ID, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("delete reference: %d %s", response.Code, response.Body.String())
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.status.Running || a.cancel != nil || a.status.Mode != "setup" {
		t.Fatalf("last reference did not stop detection: %+v", a.status)
	}
}

func TestDeleteLastReferenceCancelsPausedMonitor(t *testing.T) {
	a := &App{cfg: defaultConfig(), dataDir: t.TempDir(), active: Channel{Name: "Test", URL: "rtsp://tv.test/test"}, programCooldownUntil: time.Now().Add(time.Minute)}
	addTestReference(t, a, "last", a.active.URL)
	if err := a.startMonitor(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.handleStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/stop", nil)) })
	response := httptest.NewRecorder()
	a.handleReferenceItem(response, httptest.NewRequest(http.MethodDelete, "/api/references/last", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("delete reference: %d", response.Code)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.status.DetectionPaused || a.resumeCancel != nil || a.status.Running || a.status.Mode != "setup" {
		t.Fatalf("paused monitor retained without references: %+v", a.status)
	}
}

func TestSetupPreviewCapturesSingleFrame(t *testing.T) {
	dir := t.TempDir()
	pix := bytes.Repeat([]byte{1, 2, 3}, frameWidth*frameHeight)
	framePath := filepath.Join(dir, "frame.rgb")
	argsPath := filepath.Join(dir, "args")
	if err := os.WriteFile(framePath, pix, 0600); err != nil {
		t.Fatal(err)
	}
	// Record the invocation and stand in for ffmpeg without opening a stream.
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$TEST_ARGS\"\nexec /bin/cat \"$TEST_FRAME\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TEST_ARGS", argsPath)
	t.Setenv("TEST_FRAME", framePath)
	a := &App{cfg: defaultConfig(), dataDir: dir, active: Channel{Name: "Test", URL: "rtsp://tv.test/test"}}
	if err := a.startMonitor(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	a.handleFrame(response, httptest.NewRequest(http.MethodGet, "/api/frame.bmp", nil))
	if response.Code != http.StatusOK || !bytes.Equal(a.previewFrame.Pix, pix) {
		t.Fatalf("preview capture failed: status %d", response.Code)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil || !strings.Contains(string(args), "-frames:v\n1\n") {
		t.Fatalf("preview did not request a single frame: %s, %v", args, err)
	}
	if a.status.Running || a.cancel != nil {
		t.Fatal("preview started continuous detection")
	}
	a.setActiveChannel(Channel{Name: "Other", URL: "rtsp://tv.test/other"})
	if len(a.previewFrame.Pix) != 0 || len(a.lastFrame.Pix) != 0 {
		t.Fatal("channel switch retained the previous channel's image")
	}
}

func TestPreviewFallsBackWhenFastProbeCannotDecode(t *testing.T) {
	for _, failure := range []string{"exit 1", "exit 0", "printf incomplete; exit 0"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			pix := bytes.Repeat([]byte{4, 5, 6}, frameWidth*frameHeight)
			framePath := filepath.Join(dir, "frame.rgb")
			callsPath := filepath.Join(dir, "calls")
			if err := os.WriteFile(framePath, pix, 0600); err != nil {
				t.Fatal(err)
			}
			// A stream that cannot be decoded with the short probe must be
			// retried without those restrictions, still requesting one frame.
			script := "#!/bin/sh\nfor arg do\nif [ \"$arg\" = '-analyzeduration' ]; then\nprintf 'fast\\n' >> \"$TEST_CALLS\"\n" + failure + "\nfi\ndone\nprintf 'normal\\n' >> \"$TEST_CALLS\"\nexec /bin/cat \"$TEST_FRAME\"\n"
			if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			t.Setenv("TEST_CALLS", callsPath)
			t.Setenv("TEST_FRAME", framePath)
			frame, err := captureFrame(context.Background(), "rtsp://tv.test/test")
			if err != nil || !bytes.Equal(frame.Pix, pix) {
				t.Fatalf("fallback did not produce the preview: %v", err)
			}
			calls, err := os.ReadFile(callsPath)
			if err != nil || string(calls) != "fast\nnormal\n" {
				t.Fatalf("unexpected attempts: %q, %v", calls, err)
			}
		})
	}
}

func TestPreviewCanceledRequestDoesNotStartFFmpeg(t *testing.T) {
	dir := t.TempDir()
	callsPath := filepath.Join(dir, "calls")
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte("#!/bin/sh\nprintf called > \"$TEST_CALLS\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TEST_CALLS", callsPath)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := captureFrame(ctx, "rtsp://tv.test/test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture returned %v", err)
	}
	if _, err := os.Stat(callsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled request started ffmpeg: %v", err)
	}
}

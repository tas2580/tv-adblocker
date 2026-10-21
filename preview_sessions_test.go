package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func waitForPreview(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("preview condition did not become true")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func previewTestApp(t *testing.T) (*App, func() []int) {
	t.Helper()
	dir := t.TempDir()
	framePath, pidPath := filepath.Join(dir, "frame.rgb"), filepath.Join(dir, "pids")
	if err := os.WriteFile(framePath, bytes.Repeat([]byte{1, 2, 3}, frameWidth*frameHeight), 0600); err != nil {
		t.Fatal(err)
	}
	// Supply a frame, then keep the same decoder process alive until canceled.
	script := "#!/bin/sh\nprintf '%s\\n' \"$$\" >> \"$TEST_PIDS\"\n/bin/cat \"$TEST_FRAME\"\nexec /bin/sleep 60\n"
	if err := os.WriteFile(filepath.Join(dir, "ffmpeg"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("TEST_PIDS", pidPath)
	t.Setenv("TEST_FRAME", framePath)
	pids := func() []int {
		b, _ := os.ReadFile(pidPath)
		var result []int
		for _, line := range strings.Fields(string(b)) {
			pid, err := strconv.Atoi(line)
			if err != nil {
				t.Fatal(err)
			}
			result = append(result, pid)
		}
		return result
	}
	a := &App{cfg: defaultConfig(), dataDir: dir}
	a.setActiveChannel(Channel{Name: "Test", URL: "rtsp://tv.test/test"})
	t.Cleanup(func() {
		a.mu.Lock()
		a.previewClients = nil
		a.expirePreviewClientsLocked(time.Now())
		a.mu.Unlock()
		a.handleStop(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/stop", nil))
		waitForPreview(t, func() bool {
			for _, pid := range pids() {
				if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
					return false
				}
			}
			return true
		})
	})
	return a, pids
}

func previewSession(t *testing.T, a *App, id string, active bool) {
	t.Helper()
	w := httptest.NewRecorder()
	a.handlePreviewSession(w, httptest.NewRequest(http.MethodPost, "/api/preview", strings.NewReader(fmt.Sprintf(`{"client_id":%q,"active":%t}`, id, active))))
	if w.Code != http.StatusOK {
		t.Fatalf("preview session: %d %s", w.Code, w.Body.String())
	}
}

func previewHasFrame(a *App) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.lastFrame.Pix) > 0
}

func TestPreviewSessionsShareDecoderAndReleaseIt(t *testing.T) {
	a, pids := previewTestApp(t)
	previewSession(t, a, "one", true)
	waitForPreview(t, func() bool { return previewHasFrame(a) })
	a.mu.RLock()
	if a.status.Running || a.status.Mode != "setup" || a.status.LogoPresent != nil || a.status.Misses != 0 {
		t.Errorf("preview changed detection state: %+v", a.status)
	}
	a.mu.RUnlock()
	previewSession(t, a, "one", true)
	previewSession(t, a, "two", true)
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		a.handleFrame(w, httptest.NewRequest(http.MethodGet, "/api/frame.bmp", nil))
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/bmp" {
			t.Fatalf("cached frame: %d", w.Code)
		}
	}
	if len(pids()) != 1 {
		t.Fatalf("preview opened extra decoders: %v", pids())
	}
	previewSession(t, a, "one", false)
	a.mu.RLock()
	if a.cancel == nil {
		t.Error("closing one browser stopped the other browser's preview")
	}
	a.mu.RUnlock()
	previewSession(t, a, "two", false)
	a.mu.RLock()
	if a.cancel != nil || a.status.Running || a.previewTimer != nil {
		t.Error("closing the last editor retained the decoder or expiry timer")
	}
	a.mu.RUnlock()
}

func TestPreviewLeaseExpiresAfterDisconnectedBrowser(t *testing.T) {
	a, _ := previewTestApp(t)
	previewSession(t, a, "lost", true)
	waitForPreview(t, func() bool { return previewHasFrame(a) })
	a.mu.Lock()
	a.previewClients["lost"] = time.Now().Add(30 * time.Millisecond)
	a.expirePreviewClientsLocked(time.Now())
	a.mu.Unlock()
	waitForPreview(t, func() bool {
		a.mu.RLock()
		defer a.mu.RUnlock()
		return a.cancel == nil && len(a.previewClients) == 0 && a.previewTimer == nil
	})
}

func TestPreviewFollowsChannelSwitch(t *testing.T) {
	a, pids := previewTestApp(t)
	previewSession(t, a, "one", true)
	waitForPreview(t, func() bool { return previewHasFrame(a) })
	a.setActiveChannel(Channel{Name: "Other", URL: "rtsp://tv.test/other"})
	waitForPreview(t, func() bool { return previewHasFrame(a) && len(pids()) == 2 })
	if !errors.Is(syscall.Kill(pids()[0], 0), syscall.ESRCH) {
		waitForPreview(t, func() bool { return errors.Is(syscall.Kill(pids()[0], 0), syscall.ESRCH) })
	}
	a.mu.RLock()
	if a.status.Running || a.active.Name != "Other" {
		t.Error("preview did not follow the selected channel")
	}
	a.mu.RUnlock()
}

func TestPreviewReferenceTransitions(t *testing.T) {
	for _, leaveBeforeDelete := range []bool{false, true} {
		t.Run(fmt.Sprint(leaveBeforeDelete), func(t *testing.T) {
			a, pids := previewTestApp(t)
			previewSession(t, a, "one", true)
			waitForPreview(t, func() bool { return previewHasFrame(a) })
			a.handleFrame(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/frame.bmp", nil))
			w := httptest.NewRecorder()
			a.handleReference(w, httptest.NewRequest(http.MethodPost, "/api/reference", strings.NewReader(`{"w":8,"h":8}`)))
			var ref ReferenceEntry
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &ref) != nil {
				t.Fatalf("save first reference: %d %s", w.Code, w.Body.String())
			}
			waitForPreview(t, func() bool { return len(pids()) == 2 })
			if leaveBeforeDelete {
				previewSession(t, a, "one", false)
			}
			a.mu.RLock()
			if !a.status.Running || a.cancel == nil {
				t.Error("leaving preview stopped actual detection")
			}
			a.mu.RUnlock()
			w = httptest.NewRecorder()
			a.handleReferenceItem(w, httptest.NewRequest(http.MethodDelete, "/api/references/"+ref.ID, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("delete last reference: %d", w.Code)
			}
			a.mu.RLock()
			if a.status.Running || (a.cancel != nil) == leaveBeforeDelete {
				t.Error("deleting the last reference did not respect the preview session")
			}
			a.mu.RUnlock()
			if !leaveBeforeDelete {
				waitForPreview(t, func() bool { return len(pids()) == 3 && previewHasFrame(a) })
			}
		})
	}
}

func TestPreviewSessionDoesNotStartStoppedDetection(t *testing.T) {
	a, pids := previewTestApp(t)
	addTestReference(t, a, "existing", a.active.URL)
	previewSession(t, a, "one", true)
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.status.Running || a.cancel != nil || len(pids()) != 0 {
		t.Error("opening the editor restarted stopped detection")
	}
}

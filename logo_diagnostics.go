package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"time"
)

type logoDiagnostic struct {
	Result LogoResult `json:"result"`
	Time   string     `json:"time"`
	frame  Frame
	mask   []byte
}

// Keep a short history so the two preview images and a manual export label
// refer to exactly the snapshot shown, even while new frames arrive.
func (a *App) diagnosticSnapshot(stamp string) *logoDiagnostic {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if stamp == "" {
		return a.logoDiagnostic
	}
	for _, d := range a.logoDiagnosticHistory {
		if d.Time == stamp {
			return d
		}
	}
	return nil
}

func (a *App) observeLogo(result LogoResult, ref *logoTemplate, frame Frame, cfg Config, streamURL string) {
	a.mu.Lock()
	if a.active.URL != streamURL {
		a.mu.Unlock()
		return
	}
	if ref == nil || len(ref.features) < 8 || ref.cells < 3 {
		result.Decision = "uncertain"
	}
	a.status.LogoDetection = &result
	a.rememberDebugFrameLocked(result, frame)
	if ref != nil && rectInside(frame, result.Rect) {
		f := cropFrame(frame, result.Rect)
		mask := make([]byte, f.W*f.H)
		for y := 0; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				mask[y*f.W+x] = ref.mask[min(ref.frame.H-1, y*ref.frame.H/f.H)*ref.frame.W+min(ref.frame.W-1, x*ref.frame.W/f.W)]
			}
		}
		a.logoDiagnostic = &logoDiagnostic{Result: result, Time: time.Now().Format(time.RFC3339Nano), frame: f, mask: mask}
		a.logoDiagnosticHistory = append(a.logoDiagnosticHistory, a.logoDiagnostic)
		if len(a.logoDiagnosticHistory) > 32 {
			a.logoDiagnosticHistory = a.logoDiagnosticHistory[len(a.logoDiagnosticHistory)-32:]
		}
	}
	if result.Decision == "uncertain" {
		a.status.LogoPresent = nil
		a.status.Similarity = result.Score
		a.status.Matches, a.status.Misses = 0, 0
		a.mu.Unlock()
		return
	}
	a.observeLocked(result.Decision == "present", result.Score, cfg)
}

func (a *App) handleLogoDiagnostic(w http.ResponseWriter, r *http.Request) {
	d := a.diagnosticSnapshot(r.URL.Query().Get("time"))
	if d == nil {
		if r.URL.Query().Get("time") != "" {
			http.Error(w, "diagnostic.expired", http.StatusConflict)
		} else {
			http.Error(w, "diagnostic.empty", 404)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodPost {
		var request struct {
			Present *bool `json:"present"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.Present == nil {
			http.Error(w, "diagnostic.labelRequired", 400)
			return
		}
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		metadata, _ := json.MarshalIndent(struct {
			Result  LogoResult `json:"result"`
			Time    string     `json:"time"`
			Present bool       `json:"logo_present"`
		}{d.Result, d.Time, *request.Present}, "", "  ")
		f, _ := z.Create("metadata.json")
		_, _ = f.Write(metadata)
		f, _ = z.Create("frame.bmp")
		_, _ = f.Write(encodeBMP(d.frame))
		masked, err := encodeLogoPNG(d.frame, d.mask)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		f, _ = z.Create("masked.png")
		_, _ = f.Write(masked)
		if err = z.Close(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="logo-example.zip"`)
		_, _ = w.Write(b.Bytes())
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	switch r.URL.Query().Get("view") {
	case "frame":
		w.Header().Set("Content-Type", "image/bmp")
		_, _ = w.Write(encodeBMP(d.frame))
	case "mask":
		b, err := encodeLogoPNG(d.frame, d.mask)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(b)
	default:
		writeJSON(w, d)
	}
}

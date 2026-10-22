package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func (a *App) maskFile(id string) string { return filepath.Join(a.referencesDir(), id+".mask.png") }

func (a *App) automaticMaskFile(id string) string {
	return filepath.Join(a.referencesDir(), id+".auto-mask.png")
}

func encodeLogoPNG(f Frame, mask []byte) ([]byte, error) {
	im := image.NewNRGBA(image.Rect(0, 0, f.W, f.H))
	for i, alpha := range mask {
		im.SetNRGBA(i%f.W, i/f.W, color.NRGBA{f.Pix[i*3], f.Pix[i*3+1], f.Pix[i*3+2], alpha})
	}
	var out bytes.Buffer
	err := png.Encode(&out, im)
	return out.Bytes(), err
}

func (a *App) saveLogoMask(id string, f Frame, mask []byte) error {
	return saveLogoMaskFile(a.maskFile(id), f, mask)
}

func saveLogoMaskFile(path string, f Frame, mask []byte) error {
	if len(mask) != f.W*f.H {
		return errors.New("invalid mask dimensions")
	}
	im := image.NewGray(image.Rect(0, 0, f.W, f.H))
	copy(im.Pix, mask)
	var out bytes.Buffer
	if err := png.Encode(&out, im); err != nil {
		return err
	}
	// Rename a complete file so a concurrent detector never reads a partial PNG.
	file, err := os.CreateTemp(filepath.Dir(path), ".mask-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(out.Bytes()); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (a *App) loadLogoMask(id string, f Frame) ([]byte, error) {
	return loadLogoMaskFile(a.maskFile(id), f)
}

func loadLogoMaskFile(path string, f Frame) ([]byte, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return automaticLogoMask(f), nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	config, err := png.DecodeConfig(file)
	if err != nil {
		return nil, err
	}
	if config.Width != f.W || config.Height != f.H {
		return nil, errors.New("mask dimensions do not match reference")
	}
	if _, err = file.Seek(0, 0); err != nil {
		return nil, err
	}
	im, err := png.Decode(file)
	if err != nil {
		return nil, err
	}
	mask := make([]byte, f.W*f.H)
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			mask[y*f.W+x] = color.GrayModel.Convert(im.At(x, y)).(color.Gray).Y
		}
	}
	return mask, nil
}

type logoCapture struct {
	ID              string    `json:"id"`
	State           string    `json:"state"`
	Error           string    `json:"error,omitempty"`
	Rect            Rect      `json:"rect"`
	Started         time.Time `json:"started"`
	Duration        int       `json:"duration_seconds"`
	Samples         int       `json:"samples"`
	stream          Channel
	frames          []Frame
	lastSample      time.Time
	frame           Frame
	mask, automatic []byte
}

func captureDifference(a, b Frame) float64 {
	var sum float64
	for i := 0; i < len(a.Pix); i += 3 {
		sum += math.Abs(float64(a.Pix[i]) - float64(b.Pix[i]))
	}
	return sum / float64(a.W*a.H)
}

// A temporal mask needs recurring edge orientation and sufficient background
// changes. Stability of raw colour alone would also select a static backdrop.
func temporalLogo(frames []Frame) (Frame, []byte, error) {
	if len(frames) < 8 {
		return Frame{}, nil, errors.New("capture.tooFew")
	}
	distinct := []Frame{frames[0]}
	for _, f := range frames[1:] {
		unique := true
		for _, other := range distinct {
			if captureDifference(f, other) < 3 {
				unique = false
				break
			}
		}
		if unique {
			distinct = append(distinct, f)
		}
	}
	if len(distinct) < 3 {
		return Frame{}, nil, errors.New("capture.static")
	}
	w, h := frames[0].W, frames[0].H
	ref := Frame{w, h, make([]byte, w*h*3)}
	values := make([]int, len(frames))
	for i := range ref.Pix {
		for n, f := range frames {
			values[n] = int(f.Pix[i])
		}
		sort.Ints(values)
		ref.Pix[i] = byte(values[len(values)/2])
	}
	planes := make([]logoPlane, len(frames))
	for n, f := range frames {
		planes[n] = filteredLogo(f, w, h)
	}
	mask := make([]byte, w*h)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			var cs, sn, count float64
			for _, p := range planes {
				dx, dy := p.dx[i], p.dy[i]
				energy := dx*dx + dy*dy
				if energy < 16 {
					continue
				}
				cs += (dx*dx - dy*dy) / energy
				sn += 2 * dx * dy / energy
				count++
			}
			if count/float64(len(frames)) < .65 {
				continue
			}
			coherence := math.Hypot(cs, sn) / count
			if coherence < .7 {
				continue
			}
			weight := byte(255 * coherence * count / float64(len(frames)))
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					j := (y+dy)*w + x + dx
					mask[j] = max(mask[j], weight)
				}
			}
		}
	}
	t := prepareLogo("", ref, Rect{W: w, H: h}, mask)
	if len(t.features) < 8 || t.cells < 3 {
		return Frame{}, nil, errors.New("capture.noShape")
	}
	return ref, mask, nil
}

// a.mu is held; only cropped frames are retained and at most once a second.
func (a *App) sampleLogoCaptureLocked(f Frame, now time.Time) {
	c := a.logoCapture
	if c == nil || c.State != "recording" {
		return
	}
	if a.active.URL != c.stream.URL {
		c.State, c.Error = "error", "capture.channelChanged"
		c.frames = nil
		return
	}
	if now.Sub(c.Started) > 30*time.Second || now.Sub(c.lastSample) < time.Second || len(c.frames) >= 31 || !rectInside(f, c.Rect) {
		return
	}
	c.frames = append(c.frames, cropFrame(f, c.Rect))
	c.Samples = len(c.frames)
	c.lastSample = now
}

func (a *App) finishLogoCapture(c *logoCapture) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.logoCapture != c || c.State != "recording" {
		return
	}
	if a.active.URL != c.stream.URL {
		c.State, c.Error = "error", "capture.channelChanged"
		c.frames = nil
		return
	}
	frame, mask, err := temporalLogo(c.frames)
	c.frames = nil
	if err != nil {
		c.State, c.Error = "error", err.Error()
		return
	}
	c.frame, c.mask, c.automatic, c.State = frame, mask, append([]byte(nil), mask...), "ready"
}

func (a *App) handleLogoCapture(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if r.Method == http.MethodPost && id == "" {
		var rect Rect
		if !decodeJSON(w, r, &rect) {
			return
		}
		a.mu.Lock()
		initial := a.lastFrame
		if len(initial.Pix) == 0 {
			initial = a.previewFrame
		}
		if a.active.URL == "" || !a.hasPreviewClientsLocked(time.Now()) || !rectInside(initial, rect) || rect.W*rect.H > 131072 {
			a.mu.Unlock()
			http.Error(w, "capture.invalid", 400)
			return
		}
		if a.logoCapture != nil && a.logoCapture.State == "recording" {
			a.mu.Unlock()
			http.Error(w, "capture.busy", 409)
			return
		}
		if old := a.logoCapture; old != nil {
			old.frames = nil
			old.frame = Frame{}
			old.mask, old.automatic = nil, nil
		}
		c := &logoCapture{ID: fmt.Sprintf("%x", time.Now().UnixNano()), State: "recording", Rect: rect, Started: time.Now(), Duration: 30, stream: a.active}
		a.logoCapture = c
		// Explicit capture may need a preview decoder while detection is paused.
		// It uses the same slot and never enables automatic detection.
		if a.cancel == nil {
			ctx, cancel := context.WithCancel(context.Background())
			a.cancel = cancel
			go a.monitor(ctx)
		}
		a.sampleLogoCaptureLocked(initial, time.Now())
		response := *c
		response.frames = nil
		a.mu.Unlock()
		time.AfterFunc(30*time.Second, func() { a.finishLogoCapture(c) })
		time.AfterFunc(10*time.Minute, func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.logoCapture == c {
				a.logoCapture = nil
			}
		})
		writeJSON(w, response)
		return
	}
	a.mu.Lock()
	c := a.logoCapture
	if c == nil || id != c.ID {
		a.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	if c.stream.URL != a.active.URL && c.State != "saved" {
		c.State, c.Error = "error", "capture.channelChanged"
		c.frames = nil
	}
	switch r.Method {
	case http.MethodGet:
		response := *c
		a.mu.Unlock()
		writeJSON(w, response)
	case http.MethodDelete:
		c.frames = nil
		c.frame = Frame{}
		c.mask, c.automatic = nil, nil
		c.State = "canceled"
		a.logoCapture = nil
		a.mu.Unlock()
		writeJSON(w, map[string]bool{"ok": true})
	case http.MethodPost:
		if c.State != "ready" {
			a.mu.Unlock()
			http.Error(w, "capture.notReady", 409)
			return
		}
		entry := ReferenceEntry{ID: c.ID, StreamURL: c.stream.URL, StreamName: c.stream.Name, Rect: c.Rect, CreatedAt: time.Now().Format(time.RFC3339), MaskSource: "series"}
		frame, mask := c.frame, append([]byte(nil), c.mask...)
		// Keep the channel stable through the save; all reference mutations use
		// the same a.mu -> refMu lock order as detector startup.
		if err := a.addMaskedReference(entry, frame, mask, c.automatic); err != nil {
			a.mu.Unlock()
			http.Error(w, err.Error(), 500)
			return
		}
		c.State = "saved"
		restart := a.status.Running || a.status.Mode == "setup"
		a.mu.Unlock()
		if restart {
			a.restartMonitor()
		}
		writeJSON(w, entry)
	default:
		a.mu.Unlock()
		http.Error(w, "method not allowed", 405)
	}
}

func (a *App) addMaskedReference(entry ReferenceEntry, frame Frame, mask []byte, automatic ...[]byte) error {
	// Write the sidecar before publishing the reference in its index.
	if err := os.MkdirAll(a.referencesDir(), 0750); err != nil {
		return err
	}
	if err := a.saveLogoMask(entry.ID, frame, mask); err != nil {
		return err
	}
	proposal := mask
	if len(automatic) > 0 {
		proposal = automatic[0]
	}
	if err := saveLogoMaskFile(a.automaticMaskFile(entry.ID), frame, proposal); err != nil {
		_ = os.Remove(a.maskFile(entry.ID))
		return err
	}
	if err := a.addReference(entry, frame); err != nil {
		_ = os.Remove(a.maskFile(entry.ID))
		_ = os.Remove(a.automaticMaskFile(entry.ID))
		return err
	}
	return nil
}

func (a *App) handleLogoMask(w http.ResponseWriter, r *http.Request) {
	id, captureID := r.URL.Query().Get("id"), r.URL.Query().Get("capture")
	a.mu.Lock()
	var f Frame
	var mask, automatic []byte
	var draft *logoCapture
	source := "series"
	if captureID != "" {
		draft = a.logoCapture
		if draft == nil || draft.ID != captureID || draft.State != "ready" || draft.stream.URL != a.active.URL {
			a.mu.Unlock()
			http.Error(w, "capture.notReady", 409)
			return
		}
		f, mask, automatic = draft.frame, draft.mask, draft.automatic
	} else {
		entry, err := a.referenceByID(id)
		if err != nil || entry.StreamURL != a.active.URL {
			a.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		source = entry.MaskSource
		f, err = loadReference(a.referenceFile(id))
		if err != nil {
			a.mu.Unlock()
			http.Error(w, err.Error(), 500)
			return
		}
		mask, err = a.loadLogoMask(id, f)
		if err != nil {
			a.mu.Unlock()
			http.Error(w, err.Error(), 500)
			return
		}
		automatic, err = loadLogoMaskFile(a.automaticMaskFile(id), f)
		if err != nil {
			a.mu.Unlock()
			http.Error(w, err.Error(), 500)
			return
		}
	}
	if r.Method == http.MethodPut {
		var request struct {
			Pixels []byte `json:"pixels"`
			Reset  bool   `json:"reset"`
		}
		if !decodeJSON(w, r, &request) {
			a.mu.Unlock()
			return
		}
		if request.Reset {
			mask = append([]byte(nil), automatic...)
		} else {
			mask = request.Pixels
		}
		if len(mask) != f.W*f.H {
			a.mu.Unlock()
			http.Error(w, "mask.invalid", 400)
			return
		}
		t := prepareLogo(id, f, Rect{W: f.W, H: f.H}, mask)
		if len(t.features) < 8 || t.cells < 3 {
			a.mu.Unlock()
			http.Error(w, "capture.noShape", 400)
			return
		}
		if draft != nil {
			draft.mask = mask
		} else if err := a.saveLogoMask(id, f, mask); err != nil {
			a.mu.Unlock()
			http.Error(w, err.Error(), 500)
			return
		}
		restart := draft == nil && a.status.Running
		a.mu.Unlock()
		if restart {
			a.restartMonitor()
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	mask = append([]byte(nil), mask...)
	automatic = append([]byte(nil), automatic...)
	a.mu.Unlock()
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch r.URL.Query().Get("view") {
	case "frame":
		w.Header().Set("Content-Type", "image/bmp")
		_, _ = w.Write(encodeBMP(f))
	case "transparent":
		b, err := encodeLogoPNG(f, mask)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(b)
	default:
		writeJSON(w, struct {
			W         int    `json:"w"`
			H         int    `json:"h"`
			Pixels    []byte `json:"pixels"`
			Automatic []byte `json:"automatic"`
			Source    string `json:"source"`
		}{f.W, f.H, mask, automatic, source})
	}
}

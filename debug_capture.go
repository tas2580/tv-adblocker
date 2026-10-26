package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const debugFrameLimit = 10
const debugCaptureLimit = 10

const (
	debugToPlaceholder = "to-placeholder"
	debugToSender      = "to-sender"
)

var debugCaptureIDPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{9}Z$`)

type debugFrame struct {
	Detection *DetectionStatus `json:"detection,omitempty"`
	Time      string           `json:"time"`
	Image     string           `json:"image"`
	Result    LogoResult       `json:"result"`
	Misses    int              `json:"misses"`
	Matches   int              `json:"matches"`
	frame     Frame
}

type debugCapture struct {
	ID                string       `json:"id"`
	Time              string       `json:"time"`
	Channel           string       `json:"channel"`
	Direction         string       `json:"direction"`
	IntervalSeconds   int          `json:"interval_seconds"`
	MissingFrames     int          `json:"missing_frames"`
	PresentFrames     int          `json:"present_frames"`
	SimilarityMinimum float64      `json:"similarity_minimum"`
	UncertaintyBand   float64      `json:"uncertainty_band"`
	Frames            []debugFrame `json:"frames"`
}

// The decoder owns a new pixel slice for each frame, so retaining it here
// does not race with the next read from ffmpeg.
func (a *App) rememberDebugFrameLocked(result LogoResult, frame Frame) {
	if !a.cfg.DebugEnabled {
		return
	}
	entry := debugFrame{Time: time.Now().UTC().Format(time.RFC3339Nano), Result: result, frame: frame, Detection: a.status.Detection}
	switch result.Decision {
	case "missing":
		entry.Misses = a.status.Misses + 1
		if a.programCooldownUntil.After(time.Now()) {
			entry.Misses = 0
		}
	case "present":
		entry.Matches = a.status.Matches + 1
		if a.placeholderMinUntil.After(time.Now()) {
			entry.Matches = 0
		}
	}
	a.debugFrames = append(a.debugFrames, entry)
	if len(a.debugFrames) > debugFrameLimit {
		a.debugFrames[0] = debugFrame{}
		a.debugFrames = a.debugFrames[1:]
	}
}

// Caller holds a.mu. The returned frames remain immutable after this point.
func (a *App) snapshotDebugCaptureLocked(cfg Config, now time.Time, direction string) *debugCapture {
	if !a.cfg.DebugEnabled || len(a.debugFrames) == 0 {
		return nil
	}
	c := &debugCapture{
		ID:                now.UTC().Format("20060102T150405.000000000Z"),
		Time:              now.UTC().Format(time.RFC3339Nano),
		Channel:           a.active.Name,
		Direction:         direction,
		IntervalSeconds:   cfg.IntervalSeconds,
		MissingFrames:     cfg.MissingFrames,
		PresentFrames:     cfg.PresentFrames,
		SimilarityMinimum: cfg.similarityMinimumFor(a.active.URL),
		UncertaintyBand:   .03,
		Frames:            append([]debugFrame(nil), a.debugFrames...),
	}
	for i := range c.Frames {
		c.Frames[i].Image = fmt.Sprintf("frame-%02d.jpg", i+1)
	}
	return c
}

func (a *App) debugDir() string { return filepath.Join(a.dataDir, "debug") }

func (a *App) saveDebugCaptureAndReport(c *debugCapture) {
	if err := a.saveDebugCapture(c); err != nil {
		log.Printf("debug capture: %v", err)
		a.mu.Lock()
		a.status.DebugError = err.Error()
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.status.DebugError = ""
	a.mu.Unlock()
}

func (a *App) saveDebugCapture(c *debugCapture) error {
	a.debugMu.Lock()
	defer a.debugMu.Unlock()
	root := a.debugDir()
	if err := os.MkdirAll(root, 0750); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(root, ".pending-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for _, sample := range c.Frames {
		if err := writeDebugJPEG(filepath.Join(stage, sample.Image), sample.frame); err != nil {
			return err
		}
	}
	metadata, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "metadata.json"), append(metadata, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(stage, filepath.Join(root, c.ID)); err != nil {
		return err
	}
	return a.pruneDebugCaptures()
}

func writeDebugJPEG(path string, f Frame) error {
	if f.W <= 0 || f.H <= 0 || len(f.Pix) != f.W*f.H*3 {
		return fmt.Errorf("invalid debug frame dimensions")
	}
	img := image.NewRGBA(image.Rect(0, 0, f.W, f.H))
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			src, dst := (y*f.W+x)*3, y*img.Stride+x*4
			copy(img.Pix[dst:dst+3], f.Pix[src:src+3])
			img.Pix[dst+3] = 255
		}
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	encodeErr := jpeg.Encode(out, img, &jpeg.Options{Quality: 85})
	closeErr := out.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func (a *App) pruneDebugCaptures() error {
	entries, err := os.ReadDir(a.debugDir())
	if err != nil {
		return err
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() && debugCaptureIDPattern.MatchString(entry.Name()) {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	for len(ids) > debugCaptureLimit {
		if err := os.RemoveAll(filepath.Join(a.debugDir(), ids[0])); err != nil {
			return err
		}
		ids = ids[1:]
	}
	return nil
}

func (a *App) readDebugCapture(id string) (*debugCapture, error) {
	if !debugCaptureIDPattern.MatchString(id) {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(a.debugDir(), id, "metadata.json"))
	if err != nil {
		return nil, err
	}
	var capture debugCapture
	if err := json.Unmarshal(b, &capture); err != nil {
		return nil, err
	}
	// Older recordings only captured switches to the placeholder.
	if capture.Direction == "" {
		capture.Direction = debugToPlaceholder
	}
	return &capture, nil
}

func (a *App) handleDebugCaptures(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries, err := os.ReadDir(a.debugDir())
	if os.IsNotExist(err) {
		writeJSON(w, []debugCapture{})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	list := make([]debugCapture, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !debugCaptureIDPattern.MatchString(entry.Name()) {
			continue
		}
		capture, err := a.readDebugCapture(entry.Name())
		if err != nil {
			continue
		}
		list = append(list, *capture)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID > list[j].ID })
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, list)
}

func (a *App) handleDebugCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/debug-captures/")
	id, action, _ := strings.Cut(path, "/")
	capture, err := a.readDebugCapture(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case action == "":
		writeJSON(w, capture)
	case action == "download":
		var archive bytes.Buffer
		z := zip.NewWriter(&archive)
		for _, name := range append([]string{"metadata.json"}, debugImageNames(capture.Frames)...) {
			contents, err := os.ReadFile(filepath.Join(a.debugDir(), id, name))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			out, err := z.Create(name)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if _, err := out.Write(contents); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if err := z.Close(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="debug-%s.zip"`, id))
		_, _ = w.Write(archive.Bytes())
	case strings.HasPrefix(action, "frames/"):
		index, err := strconv.Atoi(strings.TrimPrefix(action, "frames/"))
		if err != nil || index < 0 || index >= len(capture.Frames) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		http.ServeFile(w, r, filepath.Join(a.debugDir(), id, fmt.Sprintf("frame-%02d.jpg", index+1)))
	default:
		http.NotFound(w, r)
	}
}

func debugImageNames(frames []debugFrame) []string {
	names := make([]string, len(frames))
	for i := range frames {
		names[i] = fmt.Sprintf("frame-%02d.jpg", i+1)
	}
	return names
}

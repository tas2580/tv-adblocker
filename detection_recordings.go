package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ReplaySettings struct {
	Interval int `json:"interval_seconds"`
	Missing  int `json:"missing_frames"`
	Present  int `json:"present_frames"`
	Cooldown int `json:"program_cooldown_seconds"`
	Minimum  int `json:"placeholder_min_seconds"`
}

func replaySettings(c Config) ReplaySettings {
	return ReplaySettings{c.IntervalSeconds, c.MissingFrames, c.PresentFrames, c.ProgramCooldownSeconds, c.PlaceholderMinSeconds}
}
func (s ReplaySettings) config() Config {
	c := defaultConfig()
	c.IntervalSeconds = s.Interval
	c.MissingFrames = s.Missing
	c.PresentFrames = s.Present
	c.ProgramCooldownSeconds = s.Cooldown
	c.PlaceholderMinSeconds = s.Minimum
	c.normalize()
	return c
}

type ObservationLabel struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Kind  string  `json:"kind"`
}
type RecordedObservation struct {
	DetectionObservation
	Classic DetectionDecision `json:"classic"`
	Hybrid  DetectionDecision `json:"hybrid"`
	Image   string            `json:"image,omitempty"`
}
type ObservationRecording struct {
	Purpose        string                `json:"purpose"`
	InitialProfile SenderProfile         `json:"initial_profile"`
	ID             string                `json:"id"`
	Channel        string                `json:"channel"`
	CreatedAt      string                `json:"created_at"`
	State          string                `json:"state"`
	Settings       ReplaySettings        `json:"settings"`
	Observations   []RecordedObservation `json:"observations"`
	Labels         []ObservationLabel    `json:"labels"`
	Error          string                `json:"error,omitempty"`
	started        time.Time
	offset, last   float64
	session        uint64
	imageBytes     int
}

// Status polling needs only the recording lifecycle, not its accumulated images
// and observations. Copy these fields while holding a.mu.
type ObservationRecordingStatus struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Purpose string `json:"purpose"`
	Error   string `json:"error,omitempty"`
}

func (a *App) recordingStatusLocked() *ObservationRecordingStatus {
	if a.recording == nil {
		return nil
	}
	r := a.recording
	return &ObservationRecordingStatus{ID: r.ID, State: r.State, Purpose: r.Purpose, Error: r.Error}
}

type recordedImage struct {
	name string
	data []byte
}

func (r *ObservationRecording) normalizeObservation(o DetectionObservation) DetectionObservation {
	if len(r.Observations) == 0 {
		r.offset = -o.Time
		r.session = o.Session
	} else if r.session != o.Session {
		r.offset = r.last + float64(r.Settings.Interval) - o.Time
		r.session = o.Session
	}
	o.Time += r.offset
	o.Signals.Events = append([]SignalEvent(nil), o.Signals.Events...)
	for i := range o.Signals.Events {
		o.Signals.Events[i].Time += r.offset
	}
	r.last = o.Time
	return o
}

func (a *App) recordObservationLocked(o DetectionObservation, c, h DetectionDecision) {
	r := a.recording
	if r == nil || r.State != "recording" {
		return
	}
	if len(r.Observations) >= 600 || time.Since(r.started) > 10*time.Minute {
		if err := a.saveRecordingLocked(); err != nil {
			r.State = "error"
			r.Error = err.Error()
		}
		return
	}
	o = r.normalizeObservation(o)
	entry := RecordedObservation{DetectionObservation: o, Classic: c, Hybrid: h}
	if len(a.lastFrame.Pix) > 0 && r.imageBytes < 32<<20 {
		f := a.lastFrame
		im := image.NewRGBA(image.Rect(0, 0, f.W, f.H))
		for i := 0; i < f.W*f.H; i++ {
			im.Pix[4*i], im.Pix[4*i+1], im.Pix[4*i+2], im.Pix[4*i+3] = f.Pix[3*i], f.Pix[3*i+1], f.Pix[3*i+2], 255
		}
		var b bytes.Buffer
		if jpeg.Encode(&b, im, &jpeg.Options{Quality: 80}) == nil && r.imageBytes+b.Len() <= 32<<20 {
			entry.Image = fmt.Sprintf("frame-%04d.jpg", len(r.Observations))
			r.imageBytes += b.Len()
			a.recordingFrames = append(a.recordingFrames, recordedImage{entry.Image, b.Bytes()})
		}
	}
	r.Observations = append(r.Observations, entry)
}
func (a *App) recordingPath(id string) string {
	return filepath.Join(a.dataDir, "observations", id+".json")
}
func validRecordingID(id string) bool { return debugCaptureIDPattern.MatchString(id) }
func (a *App) saveRecordingLocked() error {
	r := a.recording
	if r == nil {
		return errors.New("no recording")
	}
	r.State = "ready"
	dir := filepath.Join(a.dataDir, "observations", r.ID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	for _, im := range a.recordingFrames {
		if err := os.WriteFile(filepath.Join(dir, im.name), im.data, 0600); err != nil {
			return err
		}
	}
	if err := atomicJSON(a.recordingPath(r.ID), r); err != nil {
		return err
	}
	a.recordingFrames = nil
	return nil
}
func (a *App) handleObservationRecording(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id != "" && !validRecordingID(id) {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && id != "" {
		b, err := os.ReadFile(a.recordingPath(id))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var recording ObservationRecording
		if json.Unmarshal(b, &recording) != nil {
			http.Error(w, "invalid recording", 500)
			return
		}
		if frame := r.URL.Query().Get("frame"); frame != "" {
			for _, o := range recording.Observations {
				if o.Image == frame && filepath.Base(frame) == frame {
					http.ServeFile(w, r, filepath.Join(a.dataDir, "observations", id, frame))
					return
				}
			}
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("download") == "1" {
			w.Header().Set("Content-Type", "application/zip")
			w.Header().Set("Content-Disposition", `attachment; filename="detection-`+id+`.zip"`)
			z := zip.NewWriter(w)
			defer z.Close()
			meta, err := z.Create("observations.json")
			if err != nil {
				return
			}
			if _, err = meta.Write(b); err != nil {
				return
			}
			for _, o := range recording.Observations {
				if o.Image == "" || filepath.Base(o.Image) != o.Image {
					continue
				}
				data, err := os.ReadFile(filepath.Join(a.dataDir, "observations", id, o.Image))
				if err == nil {
					f, _ := z.Create(o.Image)
					if f != nil {
						_, _ = f.Write(data)
					}
				}
			}
			return
		}
		writeJSON(w, recording)
		return
	}
	if r.Method == http.MethodPut && id != "" {
		var request struct {
			Labels []ObservationLabel `json:"labels"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		if err := validateObservationLabels(request.Labels); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		b, err := os.ReadFile(a.recordingPath(id))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		var recording ObservationRecording
		if json.Unmarshal(b, &recording) != nil {
			http.Error(w, "invalid recording", 500)
			return
		}
		recording.Labels = request.Labels
		if err := atomicJSON(a.recordingPath(id), recording); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, recording)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if r.Method == http.MethodGet {
		writeJSON(w, a.recording)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "GET, POST or PUT required", 405)
		return
	}
	var request struct {
		Action  string `json:"action"`
		Purpose string `json:"purpose"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	switch request.Action {
	case "start":
		if request.Purpose == "" {
			request.Purpose = "validation"
		}
		if request.Purpose != "training" && request.Purpose != "validation" {
			http.Error(w, "purpose must be training or validation", 400)
			return
		}
		if a.recording != nil && a.recording.State == "recording" {
			http.Error(w, "recording already running", 409)
			return
		}
		if a.active.URL == "" || !a.status.Running || a.cfg.DetectionMode == "classic" || a.cfg.DetectionMode == "" {
			http.Error(w, "start detection in compare or hybrid mode first", 409)
			return
		}
		if a.recording != nil && len(a.recordingFrames) > 0 {
			if err := a.saveRecordingLocked(); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		now := time.Now()
		a.recording = &ObservationRecording{ID: now.UTC().Format("20060102T150405.000000000Z"), Channel: a.active.Name, CreatedAt: now.UTC().Format(time.RFC3339Nano), State: "recording", Settings: replaySettings(a.cfg), Observations: []RecordedObservation{}, Labels: []ObservationLabel{}, started: now}
		a.recordingFrames = nil
		a.loadProfilesLocked()
		a.recording.Purpose = request.Purpose
		a.recording.InitialProfile = a.profiles[a.active.URL]
		started := a.recording
		time.AfterFunc(10*time.Minute, func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			if a.recording == started && started.State == "recording" {
				if err := a.saveRecordingLocked(); err != nil {
					started.State = "error"
					started.Error = err.Error()
				}
			}
		})
	case "stop":
		if err := a.saveRecordingLocked(); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
	default:
		http.Error(w, "action must be start or stop", 400)
		return
	}
	writeJSON(w, a.recording)
}
func (a *App) handleObservationRecordings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", 405)
		return
	}
	files, err := os.ReadDir(filepath.Join(a.dataDir, "observations"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, err.Error(), 500)
		return
	}
	type summary struct {
		ID        string `json:"id"`
		Channel   string `json:"channel"`
		CreatedAt string `json:"created_at"`
		Samples   int    `json:"samples"`
	}
	out := []summary{}
	for _, f := range files {
		id := strings.TrimSuffix(f.Name(), ".json")
		if f.IsDir() || !validRecordingID(id) {
			continue
		}
		b, err := os.ReadFile(a.recordingPath(id))
		if err != nil {
			continue
		}
		var rec ObservationRecording
		if json.Unmarshal(b, &rec) == nil {
			out = append(out, summary{rec.ID, rec.Channel, rec.CreatedAt, len(rec.Observations)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	writeJSON(w, out)
}

func validateObservationLabels(labels []ObservationLabel) error {
	if len(labels) > 1000 {
		return errors.New("too many labels")
	}
	end := 0.0
	for _, l := range labels {
		if math.IsNaN(l.Start) || math.IsNaN(l.End) || math.IsInf(l.Start, 0) || math.IsInf(l.End, 0) || l.Start < end || l.End <= l.Start || l.Kind != "program" && l.Kind != "ad" {
			return errors.New("labels must be ordered, non-overlapping program/ad intervals")
		}
		end = l.End
	}
	return nil
}

type ReplayMetrics struct {
	FalseSwitches        int       `json:"false_switches"`
	ProgramHours         float64   `json:"program_hours"`
	FalseSwitchesPerHour float64   `json:"false_switches_per_hour"`
	MissedBreaks         int       `json:"missed_breaks"`
	AdDelays             []float64 `json:"ad_delays_seconds"`
	ReturnDelays         []float64 `json:"return_delays_seconds"`
	UnreturnedBreaks     int       `json:"unreturned_breaks"`
}
type ReplayReport struct {
	Classic             ReplayMetrics         `json:"classic"`
	Hybrid              ReplayMetrics         `json:"hybrid"`
	Observations        []RecordedObservation `json:"observations"`
	EmpiricalAcceptance string                `json:"empirical_acceptance"`
}

func replayObservations(rec ObservationRecording) (ReplayReport, error) {
	out := ReplayReport{Observations: []RecordedObservation{}, EmpiricalAcceptance: "pending_real_labeled_transitions"}
	if err := validateObservationLabels(rec.Labels); err != nil {
		return out, err
	}
	if len(rec.Observations) > 100000 {
		return out, errors.New("too many observations")
	}
	cfg := rec.Settings.config()
	var classic, hybrid decisionMachine
	last := -1.0
	for _, entry := range rec.Observations {
		o := entry.DetectionObservation
		if math.IsNaN(o.Time) || math.IsInf(o.Time, 0) || o.Time < last {
			return out, errors.New("observation times must be finite and ordered")
		}
		last = o.Time
		// Recorded physical locks belong to the original playback. Each replay
		// engine instead applies its own virtual cooldown and minimum ad time.
		o.ProgramLocked, o.PlaceholderLocked = false, false
		c := classic.step(o, cfg, false, SenderProfile{})
		h := hybrid.step(o, cfg, true, rec.InitialProfile)
		out.Observations = append(out.Observations, RecordedObservation{DetectionObservation: o, Classic: c, Hybrid: h, Image: entry.Image})
	}
	out.Classic = measureReplay(out.Observations, rec.Labels, false)
	out.Hybrid = measureReplay(out.Observations, rec.Labels, true)
	return out, nil
}
func measureReplay(obs []RecordedObservation, labels []ObservationLabel, hybrid bool) ReplayMetrics {
	m := ReplayMetrics{AdDelays: []float64{}, ReturnDelays: []float64{}}
	decision := func(o RecordedObservation) DetectionDecision {
		if hybrid {
			return o.Hybrid
		}
		return o.Classic
	}
	for i, l := range labels {
		if l.Kind == "program" {
			// Count only time covered by actual observations, not gaps/offline time.
			for j := 1; j < len(obs); j++ {
				if obs[j].Session != obs[j-1].Session {
					continue
				}
				start, end := math.Max(l.Start, obs[j-1].Time), math.Min(l.End, obs[j].Time)
				if end > start && end-start <= 30 {
					m.ProgramHours += (end - start) / 3600
				}
			}
			for _, o := range obs {
				if o.Time >= l.Start && o.Time < l.End && decision(o).Action == "placeholder" {
					m.FalseSwitches++
				}
			}
			continue
		}
		found := false
		for _, o := range obs {
			if o.Time >= l.Start && o.Time < l.End && (decision(o).State == "ad" || decision(o).State == "suspected_program") {
				m.AdDelays = append(m.AdDelays, o.Time-l.Start)
				found = true
				break
			}
		}
		if !found {
			m.MissedBreaks++
			continue
		}
		end := math.Inf(1)
		for j := i + 1; j < len(labels); j++ {
			if labels[j].Kind == "ad" {
				end = labels[j].Start
				break
			}
		}
		returned := false
		for _, o := range obs {
			if o.Time >= l.End && o.Time < end && decision(o).Action == "sender" {
				m.ReturnDelays = append(m.ReturnDelays, o.Time-l.End)
				returned = true
				break
			}
		}
		if !returned {
			m.UnreturnedBreaks++
		}
	}
	if m.ProgramHours > 0 {
		m.FalseSwitchesPerHour = float64(m.FalseSwitches) / m.ProgramHours
	}
	return m
}
func (a *App) handleDetectionReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}
	var rec ObservationRecording
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&rec); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	report, err := replayObservations(rec)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	writeJSON(w, report)
}

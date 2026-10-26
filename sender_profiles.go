package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
)

type SenderProfile struct {
	Breaks          int       `json:"breaks"`
	Active          bool      `json:"active"`
	TypicalDuration float64   `json:"typical_duration_seconds"`
	TransitionRate  float64   `json:"transition_rate"`
	Durations       []float64 `json:"durations,omitempty"`
	Transitions     []bool    `json:"transitions,omitempty"`
	Blocked         []Rect    `json:"blocked_regions,omitempty"`
}

func (p *SenderProfile) add(duration float64, transition bool) {
	p.Durations = append(p.Durations, duration)
	p.Transitions = append(p.Transitions, transition)
	if len(p.Durations) > 32 {
		p.Durations = p.Durations[len(p.Durations)-32:]
	}
	if len(p.Transitions) > 32 {
		p.Transitions = p.Transitions[len(p.Transitions)-32:]
	}
	p.Breaks++
	sorted := append([]float64(nil), p.Durations...)
	sort.Float64s(sorted)
	p.TypicalDuration = sorted[len(sorted)/2]
	n := 0
	for _, v := range p.Transitions {
		if v {
			n++
		}
	}
	p.TransitionRate = float64(n) / float64(len(p.Transitions))
	p.Active = p.Breaks >= 5
}

type profileTracker struct {
	session             uint64
	program             bool
	missing, present    int
	start               float64
	inBreak, transition bool
}

// No classification output is fed back as ground truth. Only an uninterrupted
// observed logo -> absence -> logo cycle can contribute to the profile.
func (p *profileTracker) observe(o DetectionObservation, cfg Config) (float64, bool, bool) {
	if p.session != o.Session {
		*p = profileTracker{session: o.Session}
	}
	if o.Manual || o.ProgramLocked || o.Logo.Decision == "uncertain" || o.Logo.UncertaintyReason != "" {
		*p = profileTracker{session: o.Session}
		return 0, false, false
	}
	if o.Logo.Decision == "missing" {
		p.present = 0
		p.missing++
		if p.program && !p.inBreak && p.missing >= cfg.MissingFrames {
			p.start = o.Time - float64((cfg.MissingFrames-1)*cfg.IntervalSeconds)
			p.inBreak = true
			p.transition = transitionSignal(o)
		}
		return 0, false, false
	}
	p.present++
	p.missing = 0
	if p.present < cfg.PresentFrames {
		return 0, false, false
	}
	if p.inBreak {
		d := o.Time - float64((cfg.PresentFrames-1)*cfg.IntervalSeconds) - p.start
		tr := p.transition || transitionSignal(o)
		*p = profileTracker{session: o.Session, program: true, present: cfg.PresentFrames}
		return d, tr, d >= 5 && d <= 1800
	}
	p.program = true
	return 0, false, false
}

func atomicJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".json-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(append(b, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (a *App) loadProfilesLocked() {
	if a.profilesLoaded {
		return
	}
	a.profilesLoaded = true
	a.profiles = map[string]SenderProfile{}
	b, err := os.ReadFile(filepath.Join(a.dataDir, "sender-profiles.json"))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err == nil {
		err = json.Unmarshal(b, &a.profiles)
	}
	if err != nil {
		a.status.LastError = "sender profiles: " + err.Error()
		a.profiles = map[string]SenderProfile{}
	}
	if a.profiles == nil {
		a.profiles = map[string]SenderProfile{}
	}
}
func (a *App) saveProfilesLocked() error {
	return atomicJSON(filepath.Join(a.dataDir, "sender-profiles.json"), a.profiles)
}

// All operations address the active sender; stream credentials never need to
// appear in the query string or exported profile.
func (a *App) handleDetectionProfile(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.loadProfilesLocked()
	url := a.active.URL
	if url == "" {
		a.mu.Unlock()
		http.Error(w, "no active channel", 409)
		return
	}
	if r.Method == http.MethodDelete {
		if a.recording != nil && a.recording.State == "recording" {
			if err := a.saveRecordingLocked(); err != nil {
				a.mu.Unlock()
				http.Error(w, err.Error(), 500)
				return
			}
		}
		old := a.profiles[url]
		delete(a.profiles, url)
		if err := a.saveProfilesLocked(); err != nil {
			a.profiles[url] = old
			a.mu.Unlock()
			http.Error(w, err.Error(), 500)
			return
		}
		a.resetDetectionLocked()
	} else if r.Method != http.MethodGet {
		a.mu.Unlock()
		http.Error(w, "GET or DELETE required", 405)
		return
	}
	profile := a.profiles[url]
	a.mu.Unlock()
	writeJSON(w, profile)
}

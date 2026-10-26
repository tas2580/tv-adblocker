package main

import (
	"context"
	"math"
	"time"
)

type ChannelDetectionConfig struct {
	AutomaticReferences *bool `json:"automatic_references,omitempty"`
	SenderLearning      *bool `json:"sender_learning,omitempty"`
}

func enabledOverride(global, channel *bool) bool {
	if channel != nil {
		return *channel
	}
	return global == nil || *global
}
func (c Config) autoReferencesFor(url string) bool {
	return c.DetectionMode != "" && c.DetectionMode != "classic" && enabledOverride(c.AutomaticReferences, c.ChannelDetection[url].AutomaticReferences)
}
func (c Config) senderLearningFor(url string) bool {
	return c.DetectionMode != "" && c.DetectionMode != "classic" && enabledOverride(c.SenderLearning, c.ChannelDetection[url].SenderLearning)
}

type SignalEvent struct {
	Time  float64 `json:"time"`
	Kind  string  `json:"kind"`
	Value float64 `json:"value,omitempty"`
}
type SignalAvailability struct {
	VisibleFormat bool `json:"visible_format"`
	Black         bool `json:"black"`
	Silence       bool `json:"silence"`
	Scene         bool `json:"scene"`
	Format        bool `json:"format"`
	Timing        bool `json:"timing"`
}
type SignalSnapshot struct {
	Available SignalAvailability `json:"available"`
	Events    []SignalEvent      `json:"events"`
}
type DetectionObservation struct {
	ctx               context.Context
	Time              float64        `json:"time"`
	Session           uint64         `json:"session"`
	Logo              LogoResult     `json:"logo"`
	Signals           SignalSnapshot `json:"signals"`
	ProgramLocked     bool           `json:"program_locked,omitempty"`
	PlaceholderLocked bool           `json:"placeholder_locked,omitempty"`
	Manual            bool           `json:"manual,omitempty"`
}
type DetectionDecision struct {
	State     string  `json:"state"`
	Reason    string  `json:"reason"`
	Action    string  `json:"action,omitempty"`
	Misses    int     `json:"misses"`
	Matches   int     `json:"matches"`
	Remaining float64 `json:"remaining_seconds"`
}
type DetectionStatus struct {
	Mode           string               `json:"mode"`
	Classic        DetectionDecision    `json:"classic"`
	Hybrid         DetectionDecision    `json:"hybrid"`
	Observation    DetectionObservation `json:"observation"`
	Profile        SenderProfile        `json:"profile"`
	AutomaticState string               `json:"automatic_state"`
	Error          string               `json:"error,omitempty"`
}

// Both engines are pure: timestamps and locks are input, never wall clock or Kodi.
// Their virtual playback allows compare/replay to follow divergent decisions.
type decisionMachine struct {
	freshStrong                                      int
	ad                                               bool
	misses, matches                                  int
	last, conflictUntil, deadline, cooldown, minimum float64
	session                                          uint64
	initialized, conflicted, expired                 bool
}

func transitionSignal(o DetectionObservation) bool {
	black, silence := -1e9, -1e9
	for _, e := range o.Signals.Events {
		if e.Time > o.Time || o.Time-e.Time > 6 {
			continue
		}
		if e.Kind == "black" && o.Signals.Available.Black {
			black = e.Time
		}
		if e.Kind == "silence" && o.Signals.Available.Silence {
			silence = e.Time
		}
	}
	return math.Abs(black-silence) <= 1 && black > -1e8 && silence > -1e8
}

func learnedTransitionSignal(o DetectionObservation) bool {
	boundary, scene := false, false
	for _, e := range o.Signals.Events {
		if e.Time > o.Time || o.Time-e.Time > 3 {
			continue
		}
		if e.Kind == "black" && o.Signals.Available.Black || e.Kind == "silence" && o.Signals.Available.Silence {
			boundary = true
		}
		if e.Kind == "scene" && o.Signals.Available.Scene || e.Kind == "format" && (o.Signals.Available.Format || o.Signals.Available.VisibleFormat) {
			scene = true
		}
	}
	return boundary && scene
}

func (m *decisionMachine) step(o DetectionObservation, cfg Config, hybrid bool, profile SenderProfile) DetectionDecision {
	gap := math.Max(30, float64(cfg.IntervalSeconds*3))
	if !m.initialized || o.Session != m.session || o.Time < m.last || o.Time-m.last > gap {
		ad := m.ad
		cooldown, minimum := -1., -1.
		if m.initialized {
			cooldown, minimum = m.cooldown, m.minimum
			if o.Time < m.last {
				cooldown += o.Time - m.last
				minimum += o.Time - m.last
			}
		}
		*m = decisionMachine{initialized: true, ad: ad, session: o.Session, conflictUntil: -1, cooldown: -1, minimum: -1}
		m.cooldown, m.minimum = cooldown, minimum
	}
	m.last = o.Time
	out := DetectionDecision{State: "program", Reason: "logo_present"}
	if m.ad {
		out.State, out.Reason = "ad", "logo_missing"
	}
	if o.Manual {
		m.freshStrong = 0
		m.misses, m.matches, m.deadline, m.conflicted, m.expired = 0, 0, 0, false, false
		out.Reason = "manual"
		return out
	}
	switch o.Logo.Decision {
	case "present":
		m.freshStrong = 0
		m.matches++
		m.misses = 0
		m.deadline, m.conflicted, m.expired = 0, false, false
		if m.ad {
			out.State, out.Reason = "suspected_program", "confirming_presence"
			if o.PlaceholderLocked || o.Time < m.minimum {
				m.matches = 0
				out.Reason = "placeholder_lock"
			} else if m.matches >= cfg.PresentFrames {
				m.ad = false
				m.cooldown = o.Time + float64(cfg.ProgramCooldownSeconds)
				out.State, out.Reason, out.Action = "program", "confirmed_presence", "sender"
			}
		}
	case "missing":
		strong := o.Logo.UncertaintyReason == "" && o.Logo.Score < o.Logo.Threshold-.15
		if strong {
			m.freshStrong++
		} else {
			m.freshStrong = 0
		}
		m.misses++
		m.matches = 0
		if !m.ad {
			out.State, out.Reason = "suspected_ad", "confirming_absence"
			if o.ProgramLocked || o.Time < m.cooldown {
				m.misses = 0
				m.deadline = 0
				m.conflicted = false
				out.State, out.Reason = "program", "program_lock"
			} else if m.misses >= cfg.MissingFrames {
				if hybrid && (m.conflictUntil >= o.Time || m.conflicted) {
					if m.deadline == 0 {
						m.deadline = o.Time + 10
						m.conflicted = true
					}
					out.Reason = "conflicting_observations"
					// Require new, unequivocal evidence even when a sender profile is active.
					resolved := strong && m.misses >= cfg.MissingFrames+2
					if transitionSignal(o) && strong {
						resolved = true
					}
					if profile.Active && profile.TransitionRate >= .5 && strong && learnedTransitionSignal(o) {
						resolved = true
					}
					if m.expired && m.freshStrong >= cfg.MissingFrames {
						m.expired = false
						m.deadline = o.Time + 10
						resolved = true
					}
					if m.expired || o.Time >= m.deadline {
						if !m.expired {
							m.freshStrong = 0
							m.misses = 0
						}
						m.expired = true
						out.State, out.Reason = "program", "unresolved_kept_program"
					} else if resolved {
						out.Action = "placeholder"
					} else {
						out.Remaining = math.Max(0, m.deadline-o.Time)
					}
				} else {
					out.Action = "placeholder"
				}
				if out.Action != "" {
					m.ad = true
					m.minimum = o.Time + float64(cfg.PlaceholderMinSeconds)
					out.State, out.Reason = "ad", "confirmed_absence"
				}
			}
		}
	default:
		m.freshStrong = 0
		m.misses, m.matches = 0, 0
		m.conflictUntil = o.Time + 10
		if hybrid && m.conflicted && o.Time >= m.deadline {
			m.expired = true
		}
		if m.ad {
			out.Reason = "uncertain_logo"
		} else {
			out.State, out.Reason = "program", "uncertain_logo"
		}
	}
	out.Misses, out.Matches = m.misses, m.matches
	return out
}

type detectionSession struct {
	automaticState  string
	classic, hybrid decisionMachine
	started         time.Time
	epoch           uint64
	profileTracker  profileTracker
	automatic       *automaticSearch
	lastSample      DetectionObservation
}

func (a *App) resetDetectionLocked() {
	epoch := a.detection.epoch + 1
	a.detection = detectionSession{epoch: epoch, started: time.Now()}
	if a.status.Mode == "placeholder" || a.status.Mode == "manual-placeholder" {
		a.detection.classic.ad = true
		a.detection.hybrid.ad = true
	}
	a.status.Detection = nil
}

// a.mu is held. Every published snapshot is immutable after publication.
func (a *App) evaluateDetectionLocked(result LogoResult, cfg Config, sample *DetectionObservation) {
	if a.detection.started.IsZero() {
		a.resetDetectionLocked()
	}
	o := DetectionObservation{Time: time.Since(a.detection.started).Seconds(), Session: a.detection.epoch, Logo: result}
	if sample != nil {
		o = *sample
		o.Logo = result
	}
	o.ProgramLocked = a.programCooldownUntil.After(time.Now())
	o.PlaceholderLocked = a.placeholderMinUntil.After(time.Now())
	o.Manual = a.status.ManualPlaceholder
	a.loadProfilesLocked()
	profile := a.profiles[a.active.URL]
	c := a.detection.classic.step(o, cfg, false, SenderProfile{})
	h := a.detection.hybrid.step(o, cfg, true, profile)
	if cfg.senderLearningFor(a.active.URL) && !o.Manual && (a.recording == nil || a.recording.State != "recording" || a.recording.Purpose != "validation") {
		if duration, transition, ok := a.detection.profileTracker.observe(o, cfg); ok {
			profile.add(duration, transition)
			a.profiles[a.active.URL] = profile
			if err := a.saveProfilesLocked(); err != nil {
				a.status.LastError = err.Error()
			}
		}
	} else {
		a.detection.profileTracker = profileTracker{}
	}
	a.detection.lastSample = o
	state := "disabled"
	if cfg.autoReferencesFor(a.active.URL) {
		state = "searching"
		if a.detection.automaticState != "" {
			state = a.detection.automaticState
		}
		if a.recording != nil && a.recording.State == "recording" && a.recording.Purpose == "validation" {
			state = "frozen"
		}
	}
	mode := cfg.DetectionMode
	if mode == "" {
		mode = "classic"
	}
	a.status.Detection = &DetectionStatus{Mode: mode, Classic: c, Hybrid: h, Observation: o, Profile: profile, AutomaticState: state}
	a.recordObservationLocked(o, c, h)
}

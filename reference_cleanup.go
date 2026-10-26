package main

import (
	"errors"
	"os"
	"time"
)

const (
	referenceProgrammeGrace = 5 * time.Minute
	referenceUnusedLimit    = 30 * time.Minute
)

// All cleanup state belongs to the active sender and is protected by a.mu.
// Durations survive decoder restarts, but never bridge an unobserved interval.
type referenceCleanup struct {
	title, pendingTitle string
	unused, review      map[string]time.Duration
	missing             map[string]bool
	decoder             *referenceLearningSession
	last                time.Time
	sampleTime          float64
	sampleSession       uint64
}

// a.mu is held. Empty/failed EPG polls break confirmation without forgetting
// the last known programme. The first known title only establishes a baseline.
func (a *App) observeReferenceProgrammeLocked(title string) {
	c := &a.referenceCleanup
	if title == "" || title == c.title {
		c.pendingTitle = ""
		return
	}
	if c.title == "" {
		c.title = title
		return
	}
	if c.pendingTitle != title {
		c.pendingTitle = title
		return
	}
	entries, err := a.referencesFor(a.active.URL)
	if err != nil {
		a.status.LastError = "automatic reference cleanup: " + err.Error()
		return // Retry on the next matching poll before committing the title.
	}
	c.title, c.pendingTitle = title, ""
	c.review = make(map[string]time.Duration)
	for _, entry := range entries {
		if entry.Automatic && !entry.Disabled {
			c.review[entry.ID] = 0
		}
	}
	c.last, c.missing = time.Time{}, nil
}

// step sees only eligible programme frames. Each reference needs definite
// absence at both ends of an interval; uncertainty pauses its own clock even
// when another logo confirms that the sender is still showing a programme.
func (c *referenceCleanup) step(entries []ReferenceEntry, results []LogoResult, elapsed time.Duration) map[string]bool {
	if c.unused == nil {
		c.unused = make(map[string]time.Duration)
	}
	byID := make(map[string]LogoResult, len(results))
	for _, result := range results {
		byID[result.ReferenceID] = result
	}
	active, missing, expired := make(map[string]bool), make(map[string]bool), make(map[string]bool)
	for _, entry := range entries {
		if !entry.Automatic || entry.Disabled {
			continue
		}
		id := entry.ID
		active[id] = true
		result := byID[id]
		if result.Decision == "present" && result.UncertaintyReason == "" {
			c.unused[id] = 0
			delete(c.review, id)
			continue
		}
		if result.Decision != "missing" || result.UncertaintyReason != "" {
			continue
		}
		missing[id] = true
		if c.missing[id] {
			c.unused[id] += elapsed
			if age, reviewing := c.review[id]; reviewing {
				c.review[id] = age + elapsed
			}
		}
		age, reviewing := c.review[id]
		if c.unused[id] >= referenceUnusedLimit || reviewing && age >= referenceProgrammeGrace {
			expired[id] = true
		}
	}
	for id := range c.unused {
		if !active[id] {
			delete(c.unused, id)
		}
	}
	for id := range c.review {
		if !active[id] {
			delete(c.review, id)
		}
	}
	c.missing = missing
	return expired
}

func (a *App) cleanupAutomaticReferences(session *referenceLearningSession, refs []*logoTemplate, results []LogoResult, cfg Config, sample *DetectionObservation, now time.Time) []*logoTemplate {
	if session == nil {
		return refs
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if session.ctx.Err() != nil || a.referenceLearning != session || a.active.URL != session.streamURL {
		return refs
	}
	c := &a.referenceCleanup
	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	elapsed := now.Sub(c.last)
	continuous := c.decoder == session && !c.last.IsZero() && elapsed > 0 && elapsed <= 3*interval
	if sample != nil {
		continuous = continuous && sample.Session == c.sampleSession && sample.Time > c.sampleTime && sample.Time-c.sampleTime <= float64(3*cfg.IntervalSeconds)
		c.sampleTime, c.sampleSession = sample.Time, sample.Session
	}
	c.decoder, c.last = session, now
	// Cap even a delayed interval to one frame's broadcast duration. Neither
	// stalled decoding nor a burst of buffered frames can age references early.
	if !continuous {
		elapsed, c.missing = 0, nil
	} else {
		elapsed = min(elapsed, interval)
	}
	present := false
	for _, result := range results {
		present = present || result.Decision == "present" && result.UncertaintyReason == ""
	}
	eligible := a.status.Running && !a.status.DetectionPaused && !a.status.ManualPlaceholder &&
		(a.status.Mode == "sender" || a.status.Mode == "monitoring") &&
		a.status.Matches >= max(1, cfg.PresentFrames) && present && !a.programCooldownUntil.After(now) &&
		(a.recording == nil || a.recording.State != "recording" || a.recording.Purpose != "validation")
	if !eligible {
		c.missing = nil
		return refs
	}
	// Use the same mu -> refMu order as manual edits. Recheck saved metadata so
	// disabling a reference always protects it, including during a decoder reload.
	a.refMu.Lock()
	defer a.refMu.Unlock()
	index, err := a.loadReferenceIndex()
	if err != nil {
		c.missing = nil
		a.status.LastError = "automatic reference cleanup: " + err.Error()
		return refs
	}
	var entries []ReferenceEntry
	for _, entry := range index.References {
		if entry.StreamURL == session.streamURL {
			entries = append(entries, entry)
		}
	}
	expired := c.step(entries, results, elapsed)
	if len(expired) == 0 {
		return refs
	}
	kept := make([]ReferenceEntry, 0, len(index.References))
	for _, entry := range index.References {
		if entry.StreamURL != session.streamURL || !expired[entry.ID] {
			kept = append(kept, entry)
		}
	}
	if err := a.saveReferenceIndex(referenceIndex{References: kept}); err != nil {
		a.status.LastError = "automatic reference cleanup: " + err.Error()
		return refs // Keep live templates until the index change succeeds.
	}
	var fileErrors []error
	for id := range expired {
		if err := a.removeReferenceFiles(id); err != nil {
			fileErrors = append(fileErrors, err)
		}
		delete(c.unused, id)
		delete(c.review, id)
		delete(c.missing, id)
	}
	if err := errors.Join(fileErrors...); err != nil {
		a.status.LastError = "automatic reference cleanup: " + err.Error()
	}
	live := make([]*logoTemplate, 0, len(refs))
	for _, ref := range refs {
		if !expired[ref.id] {
			live = append(live, ref)
		}
	}
	// Automatic expiry does not suppress regions. Start discovery afresh with
	// the newly available slots, using the existing decoder connection.
	a.detection.automatic = nil
	a.detection.automaticState = "searching"
	return live
}

func (a *App) removeReferenceFiles(id string) error {
	var errs []error
	for _, path := range []string{a.referenceFile(id), a.maskFile(id), a.automaticMaskFile(id)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

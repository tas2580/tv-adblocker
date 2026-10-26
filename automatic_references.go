package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"time"
)

const (
	automaticValidationSeconds = 300
	automaticValidationSamples = 30
	automaticMaxAbsenceSeconds = 10
)

type autoCandidate struct {
	rect                       Rect
	frame                      Frame
	mask                       []byte
	template                   *logoTemplate
	validationStart, lastMatch float64
	tested, matched            int
}
type automaticSearch struct {
	state       string
	start, last float64
	frames      [4][]Frame
	candidates  []*autoCandidate
	session     uint64
}

// Discovery uses stricter recurrence and edge strength than a manually chosen
// capture. Smooth scene gradients must not become autonomous references.
func discoveryMask(frames []Frame) []byte {
	w, h := frames[0].W, frames[0].H
	planes := make([]logoPlane, len(frames))
	for i, f := range frames {
		planes[i] = filteredLogo(f, w, h)
	}
	mask := make([]byte, w*h)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			var cs, sn, count float64
			for _, p := range planes {
				dx, dy := p.dx[i], p.dy[i]
				e := dx*dx + dy*dy
				if e < 100 {
					continue
				}
				cs += (dx*dx - dy*dy) / e
				sn += 2 * dx * dy / e
				count++
			}
			if count/float64(len(frames)) < .9 || math.Hypot(cs, sn)/math.Max(1, count) < .95 {
				continue
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					mask[(y+dy)*w+x+dx] = 255
				}
			}
		}
	}
	return mask
}

func overlapsLogo(a, b Rect) bool {
	w, h := max(0, min(a.X+a.W, b.X+b.W)-max(a.X, b.X)), max(0, min(a.Y+a.H, b.Y+b.H)-max(a.Y, b.Y))
	return float64(w*h)/float64(max(1, min(a.W*a.H, b.W*b.H))) > .5
}

// Connected components of stable contour cells, rather than bounding all
// stationary pixels in a corner (which would include studio walls and bars).
func stableLogoRegions(mask []byte, w, h int) []Rect {
	cw, ch := (w+3)/4, (h+3)/4
	grid := make([]bool, cw*ch)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if mask[y*w+x] > 100 {
				grid[(y/4)*cw+x/4] = true
			}
		}
	}
	var regions []Rect
	for i, v := range grid {
		if !v {
			continue
		}
		grid[i] = false
		queue := []int{i}
		x0, x1, y0, y1 := i%cw, i%cw, i/cw, i/cw
		for q := 0; q < len(queue); q++ {
			j := queue[q]
			x, y := j%cw, j/cw
			x0 = min(x0, x)
			x1 = max(x1, x)
			y0 = min(y0, y)
			y1 = max(y1, y)
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if nx < 0 || nx >= cw || ny < 0 || ny >= ch {
						continue
					}
					k := ny*cw + nx
					if grid[k] {
						grid[k] = false
						queue = append(queue, k)
					}
				}
			}
		}
		r := Rect{X: max(0, x0*4-3), Y: max(0, y0*4-3)}
		r.W = min(w, (x1+1)*4+3) - r.X
		r.H = min(h, (y1+1)*4+3) - r.Y
		if len(queue) < 4 || r.W < automaticLogoMinDimension || r.H < automaticLogoMinDimension ||
			r.W > w*3/4 || r.H > h*3/4 || r.W >= 8*r.H || r.H >= 8*r.W {
			continue
		}
		if automaticLogoShape(cropLogoMask(mask, w, r), r.W, r.H) {
			regions = append(regions, r)
		}
	}
	return regions
}

func (s *automaticSearch) observe(f Frame, o DetectionObservation, minimum float64, intervalSeconds int, blocked []Rect) []*autoCandidate {
	if s.session != o.Session || s.last >= 0 && (o.Time < s.last || o.Time-s.last > float64(max(10, 3*intervalSeconds))) {
		*s = automaticSearch{state: "searching", session: o.Session, start: o.Time, last: -1}
	}
	if o.Time-s.last < 1 {
		return nil
	}
	s.last = o.Time
	if len(s.candidates) > 0 {
		s.state = "validating"
		var ready, keep []*autoCandidate
		for _, c := range s.candidates {
			c.tested++
			present := c.template.compare(f, c.rect, 1, minimum).Decision == "present"
			if present {
				c.matched++
				c.lastMatch = o.Time
			} else if o.Time-c.lastMatch > automaticMaxAbsenceSeconds {
				continue
			}
			if o.Time-c.validationStart < automaticValidationSeconds || c.tested < automaticValidationSamples {
				keep = append(keep, c)
				continue
			}
			if present && float64(c.matched)/float64(c.tested) >= .9 {
				// The save path activates one reference per frame. Other validated
				// candidates must not lose their five minutes of evidence.
				if len(ready) == 0 {
					ready = append(ready, c)
				} else {
					keep = append(keep, c)
				}
			}
		}
		s.candidates = keep
		if len(keep) == 0 {
			s.start = o.Time
			s.frames = [4][]Frame{}
			s.state = "searching"
		}
		return ready
	}
	quarters := []Rect{{0, 0, f.W / 4, f.H / 4}, {3 * f.W / 4, 0, f.W / 4, f.H / 4}, {0, 3 * f.H / 4, f.W / 4, f.H / 4}, {3 * f.W / 4, 3 * f.H / 4, f.W / 4, f.H / 4}}
	for i, q := range quarters {
		if len(s.frames[i]) < 31 {
			s.frames[i] = append(s.frames[i], cropFrame(f, q))
		}
	}
	if o.Time-s.start < 30 || len(s.frames[0]) < 8 {
		return nil
	}
	for corner, frames := range s.frames {
		mask := discoveryMask(frames)
		for _, r := range stableLogoRegions(mask, frames[0].W, frames[0].H) {
			full := r
			full.X += quarters[corner].X
			full.Y += quarters[corner].Y
			blockedHere := false
			for _, b := range blocked {
				if overlapsLogo(full, b) {
					blockedHere = true
				}
			}
			if blockedHere {
				continue
			}
			crops := make([]Frame, len(frames))
			for i, frame := range frames {
				crops[i] = cropFrame(frame, r)
			}
			ref, cm, err := temporalLogo(crops)
			if err != nil || !automaticLogoShape(cm, ref.W, ref.H) {
				continue
			}
			t := prepareLogo("", ref, full, cm)
			if len(t.features) < 8 || t.cells < 3 {
				continue
			}
			s.candidates = append(s.candidates, &autoCandidate{rect: full, frame: ref, mask: cm, template: t, validationStart: o.Time, lastMatch: o.Time})
			if len(s.candidates) >= 4 {
				break
			}
		}
		if len(s.candidates) >= 4 {
			break
		}
	}
	s.frames = [4][]Frame{}
	s.start = o.Time
	if len(s.candidates) > 0 {
		s.state = "validating"
	}
	return nil
}

func (a *App) processAutomaticFrame(ctx context.Context, f Frame, cfg Config, active Channel, refs []*logoTemplate, sample *DetectionObservation) *logoTemplate {
	if sample == nil || ctx.Err() != nil {
		return nil
	}
	a.mu.Lock()
	if ctx.Err() != nil || a.active.URL != active.URL || a.status.ManualPlaceholder || a.programCooldownUntil.After(time.Now()) {
		a.mu.Unlock()
		return nil
	}
	if a.recording != nil && a.recording.State == "recording" && a.recording.Purpose == "validation" {
		a.mu.Unlock()
		return nil
	}
	a.loadProfilesLocked()
	if a.detection.automatic == nil {
		a.detection.automatic = &automaticSearch{state: "searching", start: sample.Time, last: -1, session: sample.Session}
	}
	blocked := append([]Rect(nil), a.profiles[active.URL].Blocked...)
	entries, err := a.referencesFor(active.URL)
	if err != nil {
		a.status.LastError = err.Error()
		a.mu.Unlock()
		return nil
	}
	count := 0
	for _, e := range entries {
		if e.Automatic {
			count++
		}
		if e.Disabled {
			blocked = append(blocked, e.Rect)
		}
	}
	// Merge designs that already match; a genuinely different logo at the same
	// location can still be learned without replacing a manual reference.
	for _, ref := range refs {
		r := ref.last
		if !rectInside(f, r) {
			r = ref.rect
		}
		if ref.compare(f, r, 1, cfg.similarityMinimumFor(active.URL)).Decision == "present" {
			blocked = append(blocked, r)
		}
	}
	if count >= 4 {
		a.detection.automaticState = "complete"
		a.mu.Unlock()
		return nil
	}
	search := a.detection.automatic
	a.mu.Unlock()
	ready := search.observe(f, *sample, math.Max(.8, cfg.similarityMinimumFor(active.URL)), cfg.IntervalSeconds, blocked)
	a.mu.Lock()
	defer a.mu.Unlock()
	if ctx.Err() != nil || a.active.URL != active.URL || a.detection.automatic != search {
		return nil
	}
	if a.recording != nil && a.recording.State == "recording" && a.recording.Purpose == "validation" {
		return nil
	}
	a.detection.automaticState = search.state
	for _, c := range ready {
		if ctx.Err() != nil {
			return nil
		}
		if !automaticLogoShape(c.mask, c.frame.W, c.frame.H) {
			continue
		}
		duplicate := false
		for _, e := range entries {
			if e.Disabled && overlapsLogo(c.rect, e.Rect) {
				duplicate = true
			}
		}
		for _, r := range blocked {
			if overlapsLogo(c.rect, r) {
				duplicate = true
			}
		}
		if duplicate {
			continue
		}
		entry := ReferenceEntry{ID: fmt.Sprintf("auto-%x", time.Now().UnixNano()), StreamURL: active.URL, StreamName: active.Name, Rect: c.rect, MaskSource: "series", Automatic: true, CreatedAt: time.Now().UTC().Format(time.RFC3339), ValidatedAt: time.Now().UTC().Format(time.RFC3339), ValidationRate: float64(c.matched) / float64(c.tested)}
		if err := a.addMaskedReference(entry, c.frame, c.mask); err != nil {
			a.status.LastError = err.Error()
			return nil
		}
		t := prepareLogo(entry.ID, c.frame, c.rect, c.mask)
		return t
	}
	return nil
}

func (a *App) handleAutomaticReferenceSetting(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Disabled bool `json:"disabled"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	id := r.URL.Path[len("/api/references/"):]
	a.mu.Lock()
	a.refMu.Lock()
	index, err := a.loadReferenceIndex()
	found := false
	if err == nil {
		for i, e := range index.References {
			if e.ID == id && e.StreamURL == a.active.URL && e.Automatic {
				index.References[i].Disabled = request.Disabled
				found = true
				break
			}
		}
	}
	if err == nil && found {
		err = a.saveReferenceIndex(index)
	}
	a.refMu.Unlock()
	running := a.status.Running
	a.mu.Unlock()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	if running {
		a.restartMonitor()
	}
	writeJSON(w, map[string]bool{"ok": true})
}

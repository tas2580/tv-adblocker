package main

import (
	"context"
	"errors"
	"os"
)

// These fields describe files and the current decoder session. They are added
// only to GET /api/references, never written into the saved reference index.
type referenceDetails struct {
	ReferenceEntry
	Data referenceData `json:"data"`
}

type referenceData struct {
	Image         string             `json:"image"`
	Source        string             `json:"source"`
	Mask          string             `json:"mask"`
	AutomaticMask string             `json:"automatic_mask"`
	ShapeUsable   bool               `json:"shape_usable"`
	Color         referenceColorData `json:"color"`
}

type referenceColorData struct {
	State          string             `json:"state"`
	Samples        int                `json:"samples"`
	MinimumSamples int                `json:"minimum_samples"`
	Estimate       *LogoColorEstimate `json:"estimate,omitempty"`
}

func (a *App) inspectReferenceData(entry ReferenceEntry, contrast float64) referenceData {
	out := referenceData{Image: "available", Source: entry.MaskSource,
		Mask: "missing", AutomaticMask: "missing",
		Color: referenceColorData{State: "unavailable", MinimumSamples: logoColorMinimumSamples}}
	if out.Source != "single" && out.Source != "series" {
		out.Source = "unknown"
	}
	f, err := loadReference(a.referenceFile(entry.ID))
	if err != nil {
		out.Image = "invalid"
		if errors.Is(err, os.ErrNotExist) {
			out.Image = "missing"
		}
		// File presence is still useful when the image itself cannot be read;
		// dimensions and usability cannot be checked without a valid image.
		out.Mask = referenceFilePresence(a.maskFile(entry.ID))
		out.AutomaticMask = referenceFilePresence(a.automaticMaskFile(entry.ID))
		return out
	}
	mask, state := inspectReferenceMask(a.maskFile(entry.ID), f)
	out.Mask = state
	_, out.AutomaticMask = inspectReferenceMask(a.automaticMaskFile(entry.ID), f)
	if state == "missing" {
		out.Mask = "generated"
		mask = automaticLogoMask(f)
		f = contrastLogoFrame(f, contrast)
	}
	if state == "invalid" || f.W != entry.Rect.W || f.H != entry.Rect.H || !rectInside(Frame{W: frameWidth, H: frameHeight}, entry.Rect) {
		return out
	}
	t := prepareLogo(entry.ID, f, entry.Rect, mask)
	out.ShapeUsable = len(t.features) >= 8 && t.cells >= 3
	if out.ShapeUsable {
		out.Color.State = "inactive"
	}
	return out
}

func referenceFilePresence(path string) string {
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if err != nil {
		return "invalid"
	}
	return "available"
}

func inspectReferenceMask(path string, f Frame) ([]byte, string) {
	state := referenceFilePresence(path)
	if state != "available" {
		return nil, state
	}
	mask, err := loadLogoMaskFile(path, f)
	if err != nil {
		return nil, "invalid"
	}
	return mask, "available"
}

// The decoder owns mutable templates. Publish small value snapshots under mu
// so HTTP handlers never read their training buffers or estimates concurrently.
type referenceLearningSession struct {
	ctx       context.Context
	streamURL string
	colors    map[string]referenceColorData
}

func (a *App) beginReferenceLearning(ctx context.Context, streamURL string, refs []*logoTemplate) *referenceLearningSession {
	session := &referenceLearningSession{ctx: ctx, streamURL: streamURL}
	a.mu.Lock()
	if ctx.Err() != nil || a.active.URL != streamURL {
		a.mu.Unlock()
		return nil
	}
	a.referenceLearning = session
	a.mu.Unlock()
	a.publishReferenceLearning(session, refs)
	return session
}

func (a *App) publishReferenceLearning(session *referenceLearningSession, refs []*logoTemplate) {
	if session == nil || session.ctx.Err() != nil {
		return
	}
	colors := make(map[string]referenceColorData, len(refs))
	for _, ref := range refs {
		data := referenceColorData{State: "waiting", Samples: len(ref.color.samples), MinimumSamples: logoColorMinimumSamples}
		if data.Samples > 0 {
			data.State = "learning"
		}
		if ref.color.estimate != nil {
			data.State = "ready"
			estimate := *ref.color.estimate
			data.Estimate = &estimate
		}
		colors[ref.id] = data
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.referenceLearning == session && session.ctx.Err() == nil && a.active.URL == session.streamURL {
		session.colors = colors
	}
}

func (a *App) endReferenceLearning(session *referenceLearningSession) {
	if session == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.referenceLearning == session {
		a.referenceLearning = nil
	}
}

func (a *App) referenceColorSnapshot(streamURL string) map[string]referenceColorData {
	a.mu.RLock()
	defer a.mu.RUnlock()
	session := a.referenceLearning
	if session == nil || session.ctx.Err() != nil || session.streamURL != streamURL {
		return nil
	}
	// Published maps and estimates are immutable; later publications replace the
	// entire map. Keep this snapshot alive after releasing the lock for file I/O.
	return session.colors
}

package main

import (
	"errors"
	"fmt"
	"math"
)

const (
	automaticLogoMinDimension = 20
	automaticLogoMinPixels    = 200
	automaticLogoMinSpread    = .05
)

// Measure the supported shape in the original 1280x720 decoder coordinates,
// before template resizing. Padding around a narrow contour is not logo area.
func automaticLogoShape(mask []byte, w, h int) bool {
	if w < automaticLogoMinDimension || h < automaticLogoMinDimension || len(mask) != w*h {
		return false
	}
	x0, y0, x1, y1 := w, h, -1, -1
	var n, sx, sy, sxx, syy, sxy float64
	for i, weight := range mask {
		if weight <= 100 {
			continue
		}
		x, y := i%w, i/w
		x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
		n++
		sx, sy = sx+float64(x), sy+float64(y)
		sxx, syy, sxy = sxx+float64(x*x), syy+float64(y*y), sxy+float64(x*y)
	}
	if n < automaticLogoMinPixels || x1-x0+1 < automaticLogoMinDimension || y1-y0+1 < automaticLogoMinDimension {
		return false
	}
	// The covariance eigenvalues distinguish a two-dimensional shape from a
	// straight edge at any angle, including diagonals with a large bounding box.
	xx, yy, xy := sxx/n-(sx/n)*(sx/n), syy/n-(sy/n)*(sy/n), sxy/n-(sx/n)*(sy/n)
	difference := math.Hypot(xx-yy, 2*xy)
	small, large := (xx+yy-difference)/2, (xx+yy+difference)/2
	return large > 0 && small >= automaticLogoMinSpread*large
}

func cropLogoMask(mask []byte, w int, r Rect) []byte {
	crop := make([]byte, r.W*r.H)
	for y := 0; y < r.H; y++ {
		copy(crop[y*r.W:(y+1)*r.W], mask[(r.Y+y)*w+r.X:(r.Y+y)*w+r.X+r.W])
	}
	return crop
}

// Run before starting playback polling or decoders. Only active automatic
// references with readable images/masks and a definitely unsuitable shape are
// removed; manual, disabled, and unreadable references are preserved.
func (a *App) pruneInvalidAutomaticReferences() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refMu.Lock()
	defer a.refMu.Unlock()
	index, err := a.loadReferenceIndex()
	if err != nil {
		return err
	}
	kept := make([]ReferenceEntry, 0, len(index.References))
	var removed []string
	var errs []error
	for _, entry := range index.References {
		if !entry.Automatic || entry.Disabled {
			kept = append(kept, entry)
			continue
		}
		frame, err := loadReference(a.referenceFile(entry.ID))
		var mask []byte
		if err == nil {
			mask, err = a.loadLogoMask(entry.ID, frame)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("reference %s: %w", entry.ID, err))
		}
		if err != nil || automaticLogoShape(mask, frame.W, frame.H) {
			kept = append(kept, entry)
		} else {
			removed = append(removed, entry.ID)
		}
	}
	if len(removed) == 0 {
		return errors.Join(errs...)
	}
	if err := a.saveReferenceIndex(referenceIndex{References: kept}); err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, id := range removed {
		if err := a.removeReferenceFiles(id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

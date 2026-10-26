package main

import (
	"math"
	"sort"
)

const (
	logoColorHistory        = 8
	logoColorMinimumSamples = 6
)

// The foreground colour is learned in memory, separately for each template and
// decoder session. Confidence measures agreement of spatial edge-pair fits; it
// is not the probability that a logo is present.
type LogoColorEstimate struct {
	RGB        [3]float64 `json:"rgb"`
	Confidence float64    `json:"confidence"`
	Samples    int        `json:"samples"`
}

type logoColorPair struct {
	a, b, cell int
}

type logoColorModel struct {
	pairs    []logoColorPair
	samples  []Frame
	estimate *LogoColorEstimate
}

func (t *logoTemplate) learnLogoColor(frame Frame, r Rect) {
	if !rectInside(frame, r) {
		return
	}
	m := &t.color
	if m.pairs == nil {
		m.pairs = make([]logoColorPair, 0)
		seen := make(map[[2]int]bool)
		for _, e := range t.features {
			length := math.Hypot(e.dx, e.dy)
			if e.weight < .25 || length < 6 {
				continue
			}
			dx, dy := int(math.Round(2*e.dx/length)), int(math.Round(2*e.dy/length))
			x0, y0, x1, y1 := e.x-dx, e.y-dy, e.x+dx, e.y+dy
			if min(x0, x1) < 0 || min(y0, y1) < 0 || max(x0, x1) >= t.w || max(y0, y1) >= t.h {
				continue
			}
			a, b := y0*t.w+x0, y1*t.w+x1
			key := [2]int{min(a, b), max(a, b)}
			if !seen[key] {
				m.pairs = append(m.pairs, logoColorPair{a, b, e.cell})
				seen[key] = true
			}
		}
	}
	if len(m.pairs) < 12 {
		return
	}
	sample := sampleLogoColor(frame, r, t.w, t.h)
	for _, previous := range m.samples {
		var difference float64
		for i, v := range sample.Pix {
			difference += math.Abs(float64(v) - float64(previous.Pix[i]))
		}
		if difference/float64(len(sample.Pix)) < 3 {
			return // Repeated/static backgrounds cannot identify transparency.
		}
	}
	m.samples = append(m.samples, sample)
	if len(m.samples) > logoColorHistory {
		copy(m.samples, m.samples[1:])
		m.samples = m.samples[:logoColorHistory]
	}
	if len(m.samples) < logoColorMinimumSamples {
		return
	}
	m.fit()
}

// Resample raw RGB, including for legacy templates whose shape detector uses
// contrast-adjusted greyscale. A moved/resized logo keeps the same sample grid.
func sampleLogoColor(frame Frame, r Rect, w, h int) Frame {
	out := Frame{w, h, make([]byte, w*h*3)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx := math.Max(0, (float64(x)+.5)*float64(r.W)/float64(w)-.5)
			fy := math.Max(0, (float64(y)+.5)*float64(r.H)/float64(h)-.5)
			x0, y0 := min(r.W-1, int(fx)), min(r.H-1, int(fy))
			x1, y1 := min(r.W-1, x0+1), min(r.H-1, y0+1)
			a, b := fx-float64(x0), fy-float64(y0)
			for c := 0; c < 3; c++ {
				p := func(x, y int) float64 { return float64(frame.Pix[((r.Y+y)*frame.W+r.X+x)*3+c]) }
				v := (1-b)*((1-a)*p(x0, y0)+a*p(x1, y0)) + b*((1-a)*p(x0, y1)+a*p(x1, y1))
				out.Pix[(y*w+x)*3+c] = byte(math.Round(v))
			}
		}
	}
	return out
}

// For two pixels across the same overlay edge, P = s*Q + (1-s)*C.
// The slope s accounts for their different opacities; C is the unblended logo
// colour. Only fits with changing background, low residuals and physical colour
// values contribute. Neither an average reference colour nor an arbitrary
// bright patch is sufficient to enable the guard.
func (m *logoColorModel) fitPair(a, b int) ([3]float64, bool) {
	var pm, qm [3]float64
	n := float64(len(m.samples))
	for _, sample := range m.samples {
		for c := 0; c < 3; c++ {
			pm[c] += float64(sample.Pix[a*3+c]) / n
			qm[c] += float64(sample.Pix[b*3+c]) / n
		}
	}
	var variance, foregroundVariance, covariance, contrast float64
	for _, sample := range m.samples {
		for c := 0; c < 3; c++ {
			p, q := float64(sample.Pix[a*3+c])-pm[c], float64(sample.Pix[b*3+c])-qm[c]
			variance += q * q
			foregroundVariance += p * p
			covariance += p * q
		}
	}
	for c := 0; c < 3; c++ {
		contrast += math.Abs(pm[c]-qm[c]) / 3
	}
	if variance/(3*n) < 64 || contrast < 20 {
		return [3]float64{}, false
	}
	// Both sides contain compression noise and small background gradients.
	// Orthogonal regression avoids biasing the slope (and inferred saturation)
	// downwards by treating one noisy side as an exact independent variable.
	slope := 0.0
	if covariance > 0 {
		difference := variance - foregroundVariance
		slope = 2 * covariance / (difference + math.Hypot(difference, 2*covariance))
	} else if covariance/variance < -.05 {
		return [3]float64{}, false
	}
	if slope < -.05 || slope > .85 {
		return [3]float64{}, false
	}
	slope = math.Max(0, slope)
	var color [3]float64
	for c := 0; c < 3; c++ {
		color[c] = (pm[c] - slope*qm[c]) / (1 - slope)
		if color[c] < -16 || color[c] > 271 {
			return [3]float64{}, false
		}
	}
	var residual float64
	for _, sample := range m.samples {
		for c := 0; c < 3; c++ {
			d := float64(sample.Pix[a*3+c]) - slope*float64(sample.Pix[b*3+c]) - (1-slope)*color[c]
			residual += d * d
		}
	}
	return color, residual/(3*n) <= 36
}

func (m *logoColorModel) fit() {
	type candidate struct {
		rgb  [3]float64
		cell int
	}
	var candidates []candidate
	for _, pair := range m.pairs {
		for _, endpoints := range [][2]int{{pair.a, pair.b}, {pair.b, pair.a}} {
			if rgb, ok := m.fitPair(endpoints[0], endpoints[1]); ok {
				candidates = append(candidates, candidate{rgb, pair.cell})
			}
		}
	}
	if len(candidates) < 12 {
		return
	}
	var center [3]float64
	for c := 0; c < 3; c++ {
		values := make([]float64, len(candidates))
		for i, candidate := range candidates {
			values[i] = candidate.rgb[c]
		}
		center[c] = logoColorMedian(values)
	}
	var inliers []candidate
	var cells [16]bool
	for _, candidate := range candidates {
		if logoColorDistance(candidate.rgb, center) <= 18 {
			inliers = append(inliers, candidate)
			cells[candidate.cell] = true
		}
	}
	count := 0
	for _, yes := range cells {
		if yes {
			count++
		}
	}
	if len(inliers) < 12 || count < 3 || float64(len(inliers))/float64(len(candidates)) < .6 {
		return
	}
	for c := 0; c < 3; c++ {
		values := make([]float64, len(inliers))
		for i, candidate := range inliers {
			values[i] = candidate.rgb[c]
		}
		center[c] = logoColorMedian(values)
	}
	var spread float64
	for _, candidate := range inliers {
		spread += logoColorDistance(candidate.rgb, center) / float64(len(inliers))
	}
	confidence := float64(len(inliers)) / float64(len(candidates)) * (1 - spread/32)
	if confidence < .6 {
		return
	}
	for c := range center {
		center[c] = math.Max(0, math.Min(255, center[c]))
	}
	m.estimate = &LogoColorEstimate{RGB: center, Confidence: confidence, Samples: len(m.samples)}
}

func logoColorDistance(a, b [3]float64) float64 {
	return math.Max(math.Abs(a[0]-b[0]), math.Max(math.Abs(a[1]-b[1]), math.Abs(a[2]-b[2])))
}

func logoColorMedian(values []float64) float64 {
	sort.Float64s(values)
	n := len(values)
	return (values[(n-1)/2] + values[n/2]) / 2
}

func (t *logoTemplate) logoColorHidden(frame Frame, r Rect) bool {
	check := t.checkLogoContrast(frame, r)
	return check != nil && check.Protected
}

// Coverage is weighted by the same contour features as the shape detector.
// Hidden and visible support are disjoint; colour never confirms presence.
type LogoContrastCheck struct {
	HiddenCoverage    float64 `json:"hidden_coverage"`
	VisibleCoverage   float64 `json:"visible_coverage"`
	SupportedCoverage float64 `json:"supported_coverage"`
	HiddenCells       int     `json:"hidden_cells"`
	TotalCells        int     `json:"total_cells"`
	Protected         bool    `json:"protected"`
}

func (t *logoTemplate) checkLogoContrast(frame Frame, r Rect) *LogoContrastCheck {
	if t.color.estimate == nil || !rectInside(frame, r) || len(t.features) < 8 || t.cells < 3 {
		return nil
	}
	sample := sampleLogoColor(frame, r, t.w, t.h)
	crop := cropFrame(frame, r)
	if t.legacyContrast != 0 {
		crop = contrastLogoFrame(crop, t.legacyContrast)
	}
	p := filteredLogo(crop, t.w, t.h)
	var total, hidden, visible float64
	var cells [16]bool
	for _, e := range t.features {
		total += e.weight
		if logoColorNeighborhoodHidden(sample, e.x, e.y, t.color.estimate.RGB) {
			hidden += e.weight
			cells[e.cell] = true
			continue
		}
		i := e.y*t.w + e.x
		fm, rm := math.Hypot(p.dx[i], p.dy[i]), math.Hypot(e.dx, e.dy)
		if fm >= 2 && math.Abs(p.dx[i]*e.dx+p.dy[i]*e.dy) >= .75*fm*rm {
			visible += e.weight
		}
	}
	check := &LogoContrastCheck{TotalCells: t.cells}
	for _, hidden := range cells {
		if hidden {
			check.HiddenCells++
		}
	}
	if total > 0 {
		check.HiddenCoverage = hidden / total
		check.VisibleCoverage = visible / total
		check.SupportedCoverage = (hidden + visible) / total
	}
	// At least half the contour must genuinely be ambiguous, in several cells.
	// The rest must mostly retain the expected contour, so a small bright patch
	// cannot hide the disappearance of an otherwise clearly observable logo.
	check.Protected = check.HiddenCoverage >= .5 && check.SupportedCoverage >= .85 && check.HiddenCells >= 3
	return check
}

// A small neighbourhood along each masked contour tolerates isolated poles,
// compression noise and gradual sky gradients. Median absolute deviation
// rejects texture without letting one dark pixel dominate the whole rectangle.
func logoColorNeighborhoodHidden(f Frame, x, y int, color [3]float64) bool {
	var values [3][25]float64
	n, close := 0, 0
	for yy := max(0, y-2); yy <= min(f.H-1, y+2); yy++ {
		for xx := max(0, x-2); xx <= min(f.W-1, x+2); xx++ {
			i := (yy*f.W + xx) * 3
			pixel := [3]float64{float64(f.Pix[i]), float64(f.Pix[i+1]), float64(f.Pix[i+2])}
			if logoColorDistance(pixel, color) <= 32 {
				close++
			}
			for c := 0; c < 3; c++ {
				values[c][n] = pixel[c]
			}
			n++
		}
	}
	if float64(close)/float64(n) < .8 {
		return false
	}
	for c := 0; c < 3; c++ {
		median := logoColorMedian(values[c][:n])
		// MAD alone is zero for a two-colour texture with a slight majority.
		// Also bound the central 60% range, ignoring isolated outliers only.
		if math.Abs(median-color[c]) > 24 || values[c][(n-1)*4/5]-values[c][(n-1)/5] > 18 {
			return false
		}
		for i := 0; i < n; i++ {
			values[c][i] = math.Abs(values[c][i] - median)
		}
		if logoColorMedian(values[c][:n]) > 6 {
			return false
		}
	}
	return true
}

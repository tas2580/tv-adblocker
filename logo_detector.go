package main

import (
	"math"
	"sort"
)

// Prepared templates retain their aspect ratio. Gradients are measured before
// masking: multiplying the image by its mask would create artificial edges.
type logoPlane struct {
	w, h        int
	lum, dx, dy []float64
	structure   []float64
}

// Normalize against a small neighbourhood to remove slow illumination and
// background gradients without clipping faint logo detail.
func (p *logoPlane) normalizeStructure() {
	if p.structure != nil {
		return
	}
	p.structure = make([]float64, len(p.lum))
	stride := p.w + 1
	sum, squares := make([]float64, stride*(p.h+1)), make([]float64, stride*(p.h+1))
	for y := 0; y < p.h; y++ {
		var row, row2 float64
		for x := 0; x < p.w; x++ {
			v := p.lum[y*p.w+x]
			row += v
			row2 += v * v
			i := (y+1)*stride + x + 1
			sum[i] = sum[i-stride] + row
			squares[i] = squares[i-stride] + row2
		}
	}
	for y := 0; y < p.h; y++ {
		for x := 0; x < p.w; x++ {
			x0, x1, y0, y1 := max(0, x-3), min(p.w, x+4), max(0, y-3), min(p.h, y+4)
			area := float64((x1 - x0) * (y1 - y0))
			a, b, c, d := y0*stride+x0, y0*stride+x1, y1*stride+x0, y1*stride+x1
			mean := (sum[d] - sum[b] - sum[c] + sum[a]) / area
			variance := (squares[d]-squares[b]-squares[c]+squares[a])/area - mean*mean
			p.structure[y*p.w+x] = (p.lum[y*p.w+x] - mean) / math.Sqrt(math.Max(0, variance)+25)
		}
	}
}

func filteredLogo(f Frame, w, h int) logoPlane {
	p := logoPlane{w: w, h: h, lum: make([]float64, w*h), dx: make([]float64, w*h), dy: make([]float64, w*h)}
	nx, ny := max(1, int(math.Ceil(float64(f.W)/float64(w)))), max(1, int(math.Ceil(float64(f.H)/float64(h))))
	// Area sampling when shrinking, bilinear interpolation when enlarging.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if w == f.W && h == f.H {
				p.lum[y*w+x] = pixelLuminance(f, x, y)
				continue
			}
			var sum float64
			for j := 0; j < ny; j++ {
				for i := 0; i < nx; i++ {
					fx := (float64(x)+(float64(i)+.5)/float64(nx))*float64(f.W)/float64(w) - .5
					fy := (float64(y)+(float64(j)+.5)/float64(ny))*float64(f.H)/float64(h) - .5
					fx, fy = math.Max(0, fx), math.Max(0, fy)
					x0, y0 := min(f.W-1, int(fx)), min(f.H-1, int(fy))
					x1, y1 := min(f.W-1, x0+1), min(f.H-1, y0+1)
					a, b := fx-float64(x0), fy-float64(y0)
					sum += (1-b)*((1-a)*pixelLuminance(f, x0, y0)+a*pixelLuminance(f, x1, y0)) + b*((1-a)*pixelLuminance(f, x0, y1)+a*pixelLuminance(f, x1, y1))
				}
			}
			p.lum[y*w+x] = sum / float64(nx*ny)
		}
	}
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			p.dx[i] = (-p.lum[i-w-1] + p.lum[i-w+1] - 2*p.lum[i-1] + 2*p.lum[i+1] - p.lum[i+w-1] + p.lum[i+w+1]) / 8
			p.dy[i] = (-p.lum[i-w-1] - 2*p.lum[i-w] - p.lum[i-w+1] + p.lum[i+w-1] + 2*p.lum[i+w] + p.lum[i+w+1]) / 8
		}
	}
	return p
}

func automaticLogoMask(f Frame) []byte {
	p := filteredLogo(f, f.W, f.H)
	mask := make([]byte, f.W*f.H)
	var peak float64
	for i := range p.dx {
		peak = math.Max(peak, math.Hypot(p.dx[i], p.dy[i]))
	}
	if peak < 4 {
		return mask
	}
	for y := 1; y < f.H-1; y++ {
		for x := 1; x < f.W-1; x++ {
			i := y*f.W + x
			strength := math.Hypot(p.dx[i], p.dy[i])
			if strength < math.Max(4, peak*.18) {
				continue
			}
			weight := byte(math.Min(255, 255*strength/peak))
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					j := (y+dy)*f.W + x + dx
					mask[j] = max(mask[j], weight)
				}
			}
		}
	}
	return mask
}

type logoFeature struct {
	x, y, cell     int
	dx, dy, weight float64
}
type logoTemplate struct {
	id             string
	rect           Rect
	frame          Frame
	mask           []byte
	w, h           int
	features       []logoFeature
	coarse         []logoFeature
	luminance      []float64
	cells          int
	last           Rect
	lastScale      float64
	pending        Rect
	pendingScale   float64
	pendingFrames  int
	legacyContrast float64
	color          logoColorModel
}

func prepareLogo(id string, ref Frame, rect Rect, mask []byte) *logoTemplate {
	if mask == nil {
		mask = automaticLogoMask(ref)
	}
	scale := math.Min(1, 96/float64(max(ref.W, ref.H)))
	w, h := max(8, int(math.Round(float64(ref.W)*scale))), max(8, int(math.Round(float64(ref.H)*scale)))
	p := filteredLogo(ref, w, h)
	p.normalizeStructure()
	t := &logoTemplate{id: id, rect: rect, frame: ref, mask: mask, w: w, h: h, last: rect, lastScale: 1, luminance: p.structure}
	var groups [16][]logoFeature
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			i := y*w + x
			weight := float64(mask[min(ref.H-1, y*ref.H/h)*ref.W+min(ref.W-1, x*ref.W/w)]) / 255
			if weight < .04 || math.Hypot(p.dx[i], p.dy[i]) < 3 {
				continue
			}
			cell := min(3, y*4/h)*4 + min(3, x*4/w)
			groups[cell] = append(groups[cell], logoFeature{x, y, cell, p.dx[i], p.dy[i], weight})
		}
	}
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		t.cells++
		sort.Slice(group, func(i, j int) bool {
			return math.Hypot(group[i].dx, group[i].dy)*group[i].weight > math.Hypot(group[j].dx, group[j].dy)*group[j].weight
		})
		t.features = append(t.features, group[:min(20, len(group))]...)
		t.coarse = append(t.coarse, group[:min(2, len(group))]...)
	}
	return t
}

type LogoResult struct {
	Decision          string              `json:"decision"`
	ReferenceID       string              `json:"reference_id,omitempty"`
	Rect              Rect                `json:"rect"`
	Scale             float64             `json:"scale"`
	Score             float64             `json:"score"`
	EdgeScore         float64             `json:"edge_score"`
	StructureScore    float64             `json:"structure_score"`
	Coverage          float64             `json:"coverage"`
	Threshold         float64             `json:"threshold"`
	LogoColor         *LogoColorEstimate  `json:"logo_color,omitempty"`
	UncertaintyReason string              `json:"uncertainty_reason,omitempty"`
	RejectionReason   string              `json:"rejection_reason,omitempty"`
	PositionChecks    []LogoPositionCheck `json:"position_checks,omitempty"`
}

// Position checks contain only direct shape comparisons, never nested checks.
// Keep them even when the search returns a stronger background candidate.
type LogoPositionCheck struct {
	Result   LogoResult         `json:"result"`
	Contrast *LogoContrastCheck `json:"contrast,omitempty"`
}

func logoDecision(score, threshold float64) string {
	if score+1e-12 >= math.Min(1, threshold+.03) {
		return "present"
	}
	if score < math.Max(0, threshold-.03)-1e-12 {
		return "missing"
	}
	return "uncertain"
}

func (t *logoTemplate) compare(frame Frame, r Rect, scale, minimum float64) LogoResult {
	return t.comparePlane(frame, r, scale, minimum, nil)
}

func (t *logoTemplate) comparePlane(frame Frame, r Rect, scale, minimum float64, plane *logoPlane) LogoResult {
	threshold := minimum
	if r.X != t.rect.X || r.Y != t.rect.Y || r.W != t.rect.W || r.H != t.rect.H {
		threshold = math.Max(.85, minimum)
	}
	out := LogoResult{Decision: "missing", ReferenceID: t.id, Rect: r, Scale: scale, Threshold: threshold}
	if !rectInside(frame, r) {
		out.RejectionReason = "outside_frame"
		return out
	}
	if len(t.features) < 8 || t.cells < 3 {
		out.RejectionReason = "insufficient_features"
		return out
	}
	var p logoPlane
	stride, base := t.w, 0
	if plane != nil && plane.w > 0 && r.W == t.w && r.H == t.h && t.legacyContrast == 0 {
		plane.normalizeStructure()
		p = *plane
		stride = plane.w
		base = r.Y*stride + r.X
	} else {
		crop := cropFrame(frame, r)
		if t.legacyContrast != 0 {
			crop = contrastLogoFrame(crop, t.legacyContrast)
		}
		p = filteredLogo(crop, t.w, t.h)
		p.normalizeStructure()
	}
	return t.comparePreparedPlane(&p, stride, base, out)
}

// The capture selector reuses normalized planes across all candidate pairs.
// Live detection supplies either a crop plane or an offset in the full frame.
func (t *logoTemplate) comparePreparedPlane(p *logoPlane, stride, base int, out LogoResult) LogoResult {
	var dot, fe, re, total, matched float64
	var cells [16]bool
	for _, e := range t.features {
		i := base + e.y*stride + e.x
		dx, dy := p.dx[i], p.dy[i]
		fm, rm := math.Hypot(dx, dy), math.Hypot(e.dx, e.dy)
		d := dx*e.dx + dy*e.dy
		dot += e.weight * d
		fe += e.weight * fm * fm
		re += e.weight * rm * rm
		total += e.weight
		if fm >= 2 && math.Abs(d) >= .75*fm*rm {
			matched += e.weight
			cells[e.cell] = true
		}
	}
	if fe < 1 || re < 1 || total == 0 {
		out.RejectionReason = "insufficient_edge_energy"
		return out
	}
	out.EdgeScore = math.Min(1, math.Abs(dot)/math.Sqrt(fe*re))
	out.Coverage = matched / total
	count := 0
	for _, yes := range cells {
		if yes {
			count++
		}
	}
	// Independent spatial support prevents a single strong line from dominating.
	if count < max(3, (t.cells+1)/2) {
		out.RejectionReason = "insufficient_spatial_support"
		return out
	}
	if out.Coverage < .45 {
		out.RejectionReason = "insufficient_contour_coverage"
		return out
	}
	// Locally normalized intensity structure, insensitive to exposure and to a
	// global polarity reversal on very bright programme backgrounds.
	var fs, rs, weight float64
	for _, e := range t.features {
		i := e.y*t.w + e.x
		fs += e.weight * p.structure[base+e.y*stride+e.x]
		rs += e.weight * t.luminance[i]
		weight += e.weight
	}
	var cov, fv, rv float64
	for _, e := range t.features {
		i := e.y*t.w + e.x
		f, r := p.structure[base+e.y*stride+e.x]-fs/weight, t.luminance[i]-rs/weight
		cov += e.weight * f * r
		fv += e.weight * f * f
		rv += e.weight * r * r
	}
	if fv*rv > 1 {
		out.StructureScore = math.Min(1, math.Abs(cov)/math.Sqrt(fv*rv))
	}
	out.Score = .85*(.7*out.EdgeScore+.3*out.Coverage) + .15*out.StructureScore
	out.Decision = logoDecision(out.Score, out.Threshold)
	if out.Decision == "missing" {
		out.RejectionReason = "similarity_below_threshold"
	} else if out.Decision == "uncertain" {
		out.RejectionReason = "similarity_uncertainty_band"
	}
	return out
}

// All templates share the decoded frame's gradient map. Full mask comparisons
// run only for the strongest spatially distinct candidates.
func (t *logoTemplate) find(frame Frame, plane *logoPlane, minimum float64) LogoResult {
	checks := []LogoPositionCheck{{Result: t.compare(frame, t.last, t.lastScale, minimum)}}
	if t.last != t.rect {
		checks = append(checks, LogoPositionCheck{Result: t.compare(frame, t.rect, 1, minimum)})
	}
	best := t.findShape(frame, plane, minimum, checks)
	if best.Decision == "present" {
		t.learnLogoColor(frame, best.Rect)
	}
	// Search for a real shape first. Colour alone must never confirm presence,
	// move the tracked position, or hide a confirmed match from another reference.
	for i := range checks {
		check := &checks[i]
		if check.Result.Decision != "present" {
			check.Contrast = t.checkLogoContrast(frame, check.Result.Rect)
		}
		if best.Decision == "missing" && check.Contrast != nil && check.Contrast.Protected {
			best = check.Result
			best.Decision, best.UncertaintyReason = "uncertain", "logo_color_low_contrast"
		}
	}
	best.PositionChecks = checks
	if t.color.estimate != nil {
		estimate := *t.color.estimate
		best.LogoColor = &estimate
	}
	return best
}

func (t *logoTemplate) findShape(frame Frame, plane *logoPlane, minimum float64, checks []LogoPositionCheck) LogoResult {
	best := checks[0].Result
	if best.Decision == "present" {
		t.pendingFrames = 0
		return best
	}
	if t.last != t.rect {
		if r := checks[1].Result; betterLogoResult(r, best) {
			best = r
		}
		if best.Decision == "present" {
			t.last, t.lastScale = best.Rect, best.Scale
			t.pendingFrames = 0
			return best
		}
	}
	if len(t.coarse) < 8 || t.cells < 3 {
		t.pendingFrames = 0
		return best
	}
	if plane.w == 0 {
		*plane = filteredLogo(frame, frame.W, frame.H)
	}
	scales := t.prepareSearchScales(frame)
	best = t.searchShape(frame, plane, minimum, scales, false, best)
	if best.Decision == "present" {
		t.last, t.lastScale = best.Rect, best.Scale
		t.pendingFrames = 0
		return best
	}
	// Each stage has its own candidate budget: nearby background contours must
	// not crowd a genuine distant logo out of the full-corner search.
	best = t.searchShape(frame, plane, minimum, scales, true, best)
	if best.Decision != "present" {
		t.pendingFrames = 0
		return best
	}
	if t.pendingFrames == 0 || best.Scale != t.pendingScale || absInt(best.Rect.X-t.pending.X) > 4 || absInt(best.Rect.Y-t.pending.Y) > 4 {
		t.pending, t.pendingScale, t.pendingFrames = best.Rect, best.Scale, 1
	} else {
		t.pendingFrames++
	}
	if t.pendingFrames < 3 {
		best.Decision, best.UncertaintyReason = "uncertain", "logo_position_pending"
	} else {
		t.last, t.lastScale = best.Rect, best.Scale
		t.pendingFrames = 0
	}
	return best
}

type logoSearchBounds struct {
	minX, maxX, minY, maxY int
}

func (b logoSearchBounds) contains(x, y int) bool {
	return x >= b.minX && x <= b.maxX && y >= b.minY && y <= b.maxY
}

type logoSearchPoint struct {
	offset         int
	dx, dy, weight float64
}

type logoSearchScale struct {
	w, h          int
	scale, energy float64
	points        []logoSearchPoint
	local, corner logoSearchBounds
}

func (t *logoTemplate) prepareSearchScales(frame Frame) []logoSearchScale {
	scales := make([]logoSearchScale, 0, 5)
	for _, s := range []float64{1, .95, 1.05, .9, 1.1} {
		w, h := max(8, int(math.Round(float64(t.rect.W)*s))), max(8, int(math.Round(float64(t.rect.H)*s)))
		corner := logoSearchBounds{0, frame.W - w, 0, frame.H - h}
		// Keep the logo centre in the reference's quadrant, including during
		// candidate refinement and for logos touching a frame boundary.
		if t.rect.X+t.rect.W/2 < frame.W/2 {
			corner.maxX = min(corner.maxX, frame.W/2-w/2-1)
		} else {
			corner.minX = max(corner.minX, frame.W/2-w/2)
		}
		if t.rect.Y+t.rect.H/2 < frame.H/2 {
			corner.maxY = min(corner.maxY, frame.H/2-h/2-1)
		} else {
			corner.minY = max(corner.minY, frame.H/2-h/2)
		}
		local := logoSearchBounds{
			max(corner.minX, t.rect.X-logoSearchX), min(corner.maxX, t.rect.X+logoSearchX),
			max(corner.minY, t.rect.Y-logoSearchY), min(corner.maxY, t.rect.Y+logoSearchY),
		}
		search := logoSearchScale{w: w, h: h, scale: s, local: local, corner: corner, points: make([]logoSearchPoint, len(t.coarse))}
		for i, e := range t.coarse {
			search.points[i] = logoSearchPoint{e.y*h/t.h*frame.W + e.x*w/t.w, e.dx * e.weight, e.dy * e.weight, e.weight}
			search.energy += e.weight * (e.dx*e.dx + e.dy*e.dy)
		}
		scales = append(scales, search)
	}
	return scales
}

func (t *logoTemplate) searchShape(frame Frame, plane *logoPlane, minimum float64, scales []logoSearchScale, expanded bool, best LogoResult) LogoResult {
	type candidate struct {
		x, y, size int
		score      float64
	}
	var candidates []candidate
	for size, s := range scales {
		bounds := s.local
		if expanded {
			bounds = s.corner
		}
		for y := bounds.minY; y <= bounds.maxY; y += logoSearchStep {
			for x := bounds.minX; x <= bounds.maxX; x += logoSearchStep {
				if expanded && s.local.contains(x, y) {
					continue
				}
				var dot, fe float64
				base := y*frame.W + x
				for _, e := range s.points {
					i := base + e.offset
					dx, dy := plane.dx[i], plane.dy[i]
					dot += dx*e.dx + dy*e.dy
					fe += e.weight * (dx*dx + dy*dy)
				}
				if fe < 1 || s.energy < 1 {
					continue
				}
				score := math.Abs(dot) / math.Sqrt(fe*s.energy)
				if score < .35 || len(candidates) == 12 && score <= candidates[11].score {
					continue
				}
				c := candidate{x, y, size, score}
				duplicate := -1
				for i, old := range candidates {
					if absInt(x-old.x) < 8 && absInt(y-old.y) < 8 && size == old.size {
						duplicate = i
						break
					}
				}
				if duplicate >= 0 {
					if score > candidates[duplicate].score {
						candidates[duplicate] = c
					}
				} else {
					candidates = append(candidates, c)
				}
				sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
				if len(candidates) > 12 {
					candidates = candidates[:12]
				}
			}
		}
	}
	for _, c := range candidates {
		s := scales[c.size]
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				x, y := c.x+dx, c.y+dy
				if !s.corner.contains(x, y) || expanded && s.local.contains(x, y) || !expanded && !s.local.contains(x, y) {
					continue
				}
				r := t.comparePlane(frame, Rect{x, y, s.w, s.h}, s.scale, minimum, plane)
				// Prefer confirmed matches over a higher score subject to a stricter threshold.
				if betterLogoResult(r, best) {
					best = r
				}
			}
		}
	}
	return best
}

func contrastLogoFrame(f Frame, strength float64) Frame {
	out := f.clone()
	for i := 0; i < len(out.Pix); i += 3 {
		v := byte(contrastLuminance(f.Pix[i], f.Pix[i+1], f.Pix[i+2], strength))
		out.Pix[i], out.Pix[i+1], out.Pix[i+2] = v, v, v
	}
	return out
}

func betterLogoResult(a, b LogoResult) bool {
	rank := func(s string) int {
		switch s {
		case "present":
			return 2
		case "uncertain":
			return 1
		}
		return 0
	}
	return rank(a.Decision) > rank(b.Decision) || rank(a.Decision) == rank(b.Decision) && a.Score > b.Score
}

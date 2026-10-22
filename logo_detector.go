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
	legacyContrast float64
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
	Decision       string  `json:"decision"`
	ReferenceID    string  `json:"reference_id,omitempty"`
	Rect           Rect    `json:"rect"`
	Scale          float64 `json:"scale"`
	Score          float64 `json:"score"`
	EdgeScore      float64 `json:"edge_score"`
	StructureScore float64 `json:"structure_score"`
	Coverage       float64 `json:"coverage"`
	Threshold      float64 `json:"threshold"`
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
	if !rectInside(frame, r) || len(t.features) < 8 || t.cells < 3 {
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
	if count < max(3, (t.cells+1)/2) || out.Coverage < .45 {
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
	out.Decision = logoDecision(out.Score, threshold)
	return out
}

// All templates share the decoded frame's gradient map. Full mask comparisons
// run only for the strongest spatially distinct candidates.
func (t *logoTemplate) find(frame Frame, plane *logoPlane, minimum float64) LogoResult {
	best := t.compare(frame, t.last, t.lastScale, minimum)
	if best.Decision == "present" {
		return best
	}
	if t.last != t.rect {
		if r := t.compare(frame, t.rect, 1, minimum); betterLogoResult(r, best) {
			best = r
		}
		if best.Decision == "present" {
			t.last, t.lastScale = best.Rect, best.Scale
			return best
		}
	}
	if len(t.coarse) < 8 || t.cells < 3 {
		return best
	}
	if plane.w == 0 {
		*plane = filteredLogo(frame, frame.W, frame.H)
	}
	minX, maxX := max(0, t.rect.X-logoSearchX), min(frame.W-t.rect.W, t.rect.X+logoSearchX)
	minY, maxY := max(0, t.rect.Y-logoSearchY), min(frame.H-t.rect.H, t.rect.Y+logoSearchY)
	type candidate struct {
		x, y, w, h   int
		scale, score float64
	}
	var candidates []candidate
	for _, s := range []float64{1, .95, 1.05, .9, 1.1} {
		w, h := max(8, int(math.Round(float64(t.rect.W)*s))), max(8, int(math.Round(float64(t.rect.H)*s)))
		type point struct {
			offset         int
			dx, dy, weight float64
		}
		points := make([]point, len(t.coarse))
		var re float64
		for i, e := range t.coarse {
			points[i] = point{e.y*h/t.h*frame.W + e.x*w/t.w, e.dx * e.weight, e.dy * e.weight, e.weight}
			re += e.weight * (e.dx*e.dx + e.dy*e.dy)
		}
		for y := minY; y <= min(maxY, frame.H-h); y += 2 {
			for x := minX; x <= min(maxX, frame.W-w); x += 2 {
				if (x+w/2 < frame.W/2) != (t.rect.X+t.rect.W/2 < frame.W/2) || (y+h/2 < frame.H/2) != (t.rect.Y+t.rect.H/2 < frame.H/2) {
					continue
				}
				var dot, fe float64
				base := y*frame.W + x
				for _, e := range points {
					i := base + e.offset
					dx, dy := plane.dx[i], plane.dy[i]
					dot += dx*e.dx + dy*e.dy
					fe += e.weight * (dx*dx + dy*dy)
				}
				if fe < 1 || re < 1 {
					continue
				}
				score := math.Abs(dot) / math.Sqrt(fe*re)
				if score < .35 || len(candidates) == 12 && score <= candidates[11].score {
					continue
				}
				c := candidate{x, y, w, h, s, score}
				duplicate := -1
				for i, old := range candidates {
					if absInt(x-old.x) < 8 && absInt(y-old.y) < 8 && s == old.scale {
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
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				r := t.comparePlane(frame, Rect{c.x + dx, c.y + dy, c.w, c.h}, c.scale, minimum, plane)
				// Prefer confirmed matches over a higher score subject to a stricter threshold.
				if betterLogoResult(r, best) {
					best = r
				}
			}
		}
	}
	if best.Decision == "present" {
		t.last, t.lastScale = best.Rect, best.Scale
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

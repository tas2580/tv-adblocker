package main

import (
	"math"
	"sort"
)

const (
	logoSearchX        = 220
	logoSearchY        = 120
	logoSearchStep     = 2
	logoSearchTopCount = 12
	logoSearchMinimum  = .85
	logoSearchFeatures = 96
)

type logoCandidate struct {
	x, y  int
	score float64
}

type logoEdge struct {
	x, y   int
	dx, dy float64
	energy float64
}

// findLogo keeps the original fixed-position match and searches the same
// corner only when it fails. An off-position match needs a stronger score
// because searching many locations increases the chance of an accidental hit.
func findLogo(frame, ref Frame, rect Rect, minimum, contrast float64) (float64, bool, error) {
	score, err := compareLogo(frame, ref, rect, contrast)
	if err != nil || score >= minimum {
		return score, err == nil && score >= minimum, err
	}
	shifted, err := searchLogoCorner(frame, ref, rect, contrast)
	if err != nil {
		return score, false, err
	}
	if shifted > score {
		score = shifted
	}
	return score, shifted >= math.Max(minimum, logoSearchMinimum), nil
}

func searchLogoCorner(frame, ref Frame, rect Rect, contrast float64) (float64, error) {
	if ref.W != rect.W || ref.H != rect.H {
		return 0, nil
	}
	minX := max(0, rect.X-logoSearchX)
	maxX := min(frame.W-rect.W, rect.X+logoSearchX)
	minY := max(0, rect.Y-logoSearchY)
	maxY := min(frame.H-rect.H, rect.Y+logoSearchY)
	// A reference's centre defines its corner. Do not cross either midline.
	if rect.X+rect.W/2 < frame.W/2 {
		maxX = min(maxX, frame.W/2-rect.W/2-1)
	} else {
		minX = max(minX, frame.W/2-rect.W/2)
	}
	if rect.Y+rect.H/2 < frame.H/2 {
		maxY = min(maxY, frame.H/2-rect.H/2-1)
	} else {
		minY = max(minY, frame.H/2-rect.H/2)
	}
	if minX > maxX || minY > maxY {
		return 0, nil
	}

	edges, edgeEnergy := logoReferenceEdges(ref, contrast)
	if len(edges) == 0 {
		return 0, nil
	}
	grayWidth := maxX + rect.W - minX
	grayHeight := maxY + rect.H - minY
	gray := make([]float64, grayWidth*grayHeight)
	for y := 0; y < grayHeight; y++ {
		for x := 0; x < grayWidth; x++ {
			i := ((minY+y)*frame.W + minX + x) * 3
			gray[y*grayWidth+x] = contrastLuminance(frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2], contrast)
		}
	}

	var top [logoSearchTopCount]logoCandidate
	topLen := 0
	for y := minY; y <= maxY; y += logoSearchStep {
		for x := minX; x <= maxX; x += logoSearchStep {
			if x == rect.X && y == rect.Y {
				continue
			}
			var dot, frameEnergy float64
			base := (y-minY)*grayWidth + x - minX
			for _, edge := range edges {
				i := base + edge.y*grayWidth + edge.x
				dx := gray[i+1] - gray[i-1]
				dy := gray[i+grayWidth] - gray[i-grayWidth]
				dot += dx*edge.dx + dy*edge.dy
				frameEnergy += dx*dx + dy*dy
			}
			if frameEnergy < 1 {
				continue
			}
			score := dot / math.Sqrt(frameEnergy*edgeEnergy)
			if score > 0 {
				addLogoCandidate(&top, &topLen, logoCandidate{x, y, score})
			}
		}
	}

	best := 0.0
	for i := 0; i < topLen; i++ {
		candidate := top[i]
		for y := max(minY, candidate.y-4); y <= min(maxY, candidate.y+4); y++ {
			for x := max(minX, candidate.x-4); x <= min(maxX, candidate.x+4); x++ {
				score, err := compareLogo(frame, ref, Rect{x, y, rect.W, rect.H}, contrast)
				if err != nil {
					return 0, err
				}
				if score > best {
					best = score
				}
			}
		}
	}
	return best, nil
}

// Use strong native-resolution edges as a cheap translation-invariant
// prefilter. Flat references have no searchable shape and are kept at their
// original location. The exact comparison still makes the final decision.
func logoReferenceEdges(ref Frame, contrast float64) ([]logoEdge, float64) {
	if ref.W < 3 || ref.H < 3 {
		return nil, 0
	}
	gray := make([]float64, ref.W*ref.H)
	for y := 0; y < ref.H; y++ {
		for x := 0; x < ref.W; x++ {
			i := (y*ref.W + x) * 3
			gray[y*ref.W+x] = contrastLuminance(ref.Pix[i], ref.Pix[i+1], ref.Pix[i+2], contrast)
		}
	}
	edges := make([]logoEdge, 0, (ref.W-2)*(ref.H-2))
	for y := 1; y < ref.H-1; y++ {
		for x := 1; x < ref.W-1; x++ {
			i := y*ref.W + x
			dx := gray[i+1] - gray[i-1]
			dy := gray[i+ref.W] - gray[i-ref.W]
			energy := dx*dx + dy*dy
			if energy >= 64 {
				edges = append(edges, logoEdge{x, y, dx, dy, energy})
			}
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].energy > edges[j].energy })
	if len(edges) > logoSearchFeatures {
		edges = edges[:logoSearchFeatures]
	}
	var energy float64
	for _, edge := range edges {
		energy += edge.energy
	}
	return edges, energy
}

// Keep the strongest coarse candidates from distinct nearby locations so a
// single textured patch cannot crowd out the actual logo.
func addLogoCandidate(top *[logoSearchTopCount]logoCandidate, length *int, candidate logoCandidate) {
	for i := 0; i < *length; i++ {
		if absInt(top[i].x-candidate.x) < 12 && absInt(top[i].y-candidate.y) < 12 {
			if candidate.score <= top[i].score {
				return
			}
			top[i] = candidate
			for i > 0 && top[i].score > top[i-1].score {
				top[i], top[i-1] = top[i-1], top[i]
				i--
			}
			return
		}
	}
	if *length == logoSearchTopCount {
		if candidate.score <= top[*length-1].score {
			return
		}
		top[*length-1] = candidate
	} else {
		top[*length] = candidate
		*length++
	}
	for i := *length - 1; i > 0 && top[i].score > top[i-1].score; i-- {
		top[i], top[i-1] = top[i-1], top[i]
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

package main

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestTemporalLogoPreservesDetailLostByMedian(t *testing.T) {
	for _, size := range [][2]int{{48, 36}, {192, 144}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			var frames []Frame
			levels := []byte{40, 120, 200}
			for group := range levels {
				for offset := byte(0); offset < 9; offset += 3 {
					f := solidFrame(size[0], size[1], levels[(group+1)%len(levels)]+offset)
					for y := 0; y < f.H; y++ {
						for x := 0; x < f.W; x++ {
							if logoTestShape(x, y, f.W, f.H) {
								for c := 0; c < 3; c++ {
									f.Pix[(y*f.W+x)*3+c] = levels[group] + offset
								}
							}
						}
					}
					frames = append(frames, f)
				}
			}
			median := seriesTestMedian(frames)
			if !bytes.Equal(median.Pix, solidFrame(size[0], size[1], 123).Pix) {
				t.Fatal("fixture no longer loses all logo detail in the pixel median")
			}
			ref, mask, err := temporalLogo(frames)
			if err != nil {
				t.Fatal(err)
			}
			assertSeriesOriginal(t, ref, frames)
			template := prepareLogo("series", ref, Rect{W: ref.W, H: ref.H}, mask)
			for i, f := range frames {
				if result := template.compare(f, template.rect, 1, .8); result.Decision != "present" {
					t.Errorf("frame %d: %+v", i, result)
				}
			}
			// The saved reference owns its pixels after capture buffers are released.
			saved := append([]byte(nil), ref.Pix...)
			for _, f := range frames {
				clear(f.Pix)
			}
			if !bytes.Equal(ref.Pix, saved) {
				t.Fatal("reference aliases an input frame")
			}
		})
	}
}

func TestTemporalLogoRejectsChangingBackgroundWithoutShape(t *testing.T) {
	var frames []Frame
	for i := 0; i < 8; i++ {
		frames = append(frames, solidFrame(48, 36, byte(30+i*20)))
	}
	if _, _, err := temporalLogo(frames); err == nil || err.Error() != "capture.noShape" {
		t.Fatalf("accepted changing flat backgrounds: %v", err)
	}
}

func TestTemporalLogoSAT1HeldOutBroadcasts(t *testing.T) {
	var frames []Frame
	for i := 1; i <= 8; i++ {
		frames = append(frames, seriesTestFrame(t, fmt.Sprintf("capture-%02d.png", i)))
	}
	ref, mask, err := temporalLogo(frames)
	if err != nil {
		t.Fatal(err)
	}
	assertSeriesOriginal(t, ref, frames)
	template := prepareLogo("series", ref, Rect{W: ref.W, H: ref.H}, mask)

	median := seriesTestMedian(frames)
	medianTemplate := prepareLogo("median", median, template.rect, mask)
	oldRef, err := loadReference(filepath.Join("testdata", "sat1-series", "old-reference.rgb"))
	if err != nil {
		t.Fatal(err)
	}
	oldMask, err := loadLogoMaskFile(filepath.Join("testdata", "sat1-series", "old-mask.png"), oldRef)
	if err != nil {
		t.Fatal(err)
	}
	oldTemplate := prepareLogo("old", oldRef, template.rect, oldMask)
	present, oldPresent, medianPresent := 0, 0, 0
	for _, recording := range []struct {
		name        string
		first, last int
	}{
		{"194014", 1, 5},
		{"194449", 6, 7},
		{"194631", 8, 17},
	} {
		t.Run(recording.name, func(t *testing.T) {
			a, cfg, _, _ := debugTestApp(t, false, false)
			cfg.SimilarityMinimum, cfg.MissingFrames = .75, 5
			a.cfg = cfg
			for i := recording.first; i <= recording.last; i++ {
				f := seriesTestFrame(t, fmt.Sprintf("present-%02d.png", i))
				result := template.compare(f, template.rect, 1, cfg.SimilarityMinimum)
				old := oldTemplate.compare(f, template.rect, 1, cfg.SimilarityMinimum)
				med := medianTemplate.compare(f, template.rect, 1, cfg.SimilarityMinimum)
				t.Logf("frame %02d: selected %.3f (%s), old %.3f (%s), same-series median %.3f (%s)", i, result.Score, result.Decision, old.Score, old.Decision, med.Score, med.Decision)
				if result.Decision == "present" {
					present++
				}
				if old.Decision == "present" {
					oldPresent++
				}
				if med.Decision == "present" {
					medianPresent++
				}
				a.observeLogo(result, template, f, cfg, a.active.URL)
				if a.status.Mode != "monitoring" {
					t.Fatalf("visible logo triggered a switch at frame %d: %+v", i, a.status)
				}
				negative := seriesTestFrame(t, fmt.Sprintf("absent-%02d.png", i))
				if result := template.compare(negative, template.rect, 1, cfg.SimilarityMinimum); result.Decision != "missing" {
					t.Errorf("logo-free frame %d: %+v", i, result)
				}
			}
		})
	}
	if present < 5 || present <= oldPresent || present <= medianPresent {
		t.Errorf("insufficient improvement: selected %d present, old %d, same-series median %d", present, oldPresent, medianPresent)
	}
}

func assertSeriesOriginal(t *testing.T, ref Frame, frames []Frame) {
	t.Helper()
	for _, f := range frames {
		if f.W == ref.W && f.H == ref.H && bytes.Equal(f.Pix, ref.Pix) {
			return
		}
	}
	t.Fatal("reference is not an original capture frame")
}

func seriesTestMedian(frames []Frame) Frame {
	ref := frames[0].clone()
	values := make([]int, len(frames))
	for i := range ref.Pix {
		for n, f := range frames {
			values[n] = int(f.Pix[i])
		}
		sort.Ints(values)
		ref.Pix[i] = byte(values[len(values)/2])
	}
	return ref
}

func seriesTestFrame(t *testing.T, name string) Frame {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "sat1-series", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	bounds := im.Bounds()
	out := Frame{W: bounds.Dx(), H: bounds.Dy(), Pix: make([]byte, bounds.Dx()*bounds.Dy()*3)}
	for y := 0; y < out.H; y++ {
		for x := 0; x < out.W; x++ {
			r, g, b, _ := im.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := (y*out.W + x) * 3
			out.Pix[i], out.Pix[i+1], out.Pix[i+2] = byte(r>>8), byte(g>>8), byte(b>>8)
		}
	}
	return out
}

func BenchmarkTemporalLogoMaximumCapture(b *testing.B) {
	frames := make([]Frame, 31)
	for i := range frames {
		frames[i] = compositeTestLogo(512, 256, i, .8, true)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := temporalLogo(frames); err != nil {
			b.Fatal(err)
		}
	}
}

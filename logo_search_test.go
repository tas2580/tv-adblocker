package main

import "testing"

func solidFrame(width, height int, value byte) Frame {
	f := Frame{width, height, make([]byte, width*height*3)}
	for i := range f.Pix {
		f.Pix[i] = value
	}
	return f
}

func paintTestLogo(frame *Frame, rect Rect, background byte) {
	for y := 0; y < rect.H; y++ {
		for x := 0; x < rect.W; x++ {
			value := background
			if (x >= 8 && x < 13 && y >= 5 && y < rect.H-5) ||
				(y >= rect.H-10 && y < rect.H-5 && x >= 8 && x < rect.W-7) ||
				(x >= rect.W-15 && x < rect.W-9 && y >= 6 && y < 12) {
				value = 245
			}
			i := ((rect.Y+y)*frame.W + rect.X + x) * 3
			frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2] = value, value, value
		}
	}
}

func TestFindLogoAcrossAspectRatioPositions(t *testing.T) {
	for _, tt := range []struct {
		name      string
		reference Rect
		shifted   Rect
	}{
		{"upper right", Rect{990, 50, 48, 36}, Rect{820, 130, 48, 36}},
		{"lower left", Rect{260, 620, 48, 36}, Rect{425, 545, 48, 36}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wide := solidFrame(frameWidth, frameHeight, 70)
			paintTestLogo(&wide, tt.reference, 70)
			ref := cropFrame(wide, tt.reference)
			if score, found, err := findLogo(wide, ref, tt.reference, .8, 1.5); err != nil || !found || score < .99 {
				t.Fatalf("original location: score=%f found=%t err=%v", score, found, err)
			}

			narrow := solidFrame(frameWidth, frameHeight, 90)
			for y := 0; y < frameHeight; y++ {
				for x := 0; x < 160; x++ {
					left := (y*frameWidth + x) * 3
					right := (y*frameWidth + frameWidth - 1 - x) * 3
					for color := 0; color < 3; color++ {
						narrow.Pix[left+color] = 0
						narrow.Pix[right+color] = 0
					}
				}
			}
			paintTestLogo(&narrow, tt.shifted, 90)
			if score, found, err := findLogo(narrow, ref, tt.reference, .8, 1.5); err != nil || !found || score < .85 {
				atLogo, _ := compareLogo(narrow, ref, tt.shifted, 1.5)
				t.Fatalf("4:3 position: score=%f found=%t exact_shifted=%f err=%v", score, found, atLogo, err)
			}

			for y := tt.shifted.Y; y < tt.shifted.Y+tt.shifted.H; y++ {
				for x := tt.shifted.X; x < tt.shifted.X+tt.shifted.W; x++ {
					i := (y*frameWidth + x) * 3
					narrow.Pix[i], narrow.Pix[i+1], narrow.Pix[i+2] = 90, 90, 90
				}
			}
			if score, found, err := findLogo(narrow, ref, tt.reference, .8, 1.5); err != nil || found {
				t.Fatalf("missing logo: score=%f found=%t err=%v", score, found, err)
			}
		})
	}
}

func BenchmarkLogoSearchFiveReferences(b *testing.B) {
	rects := []Rect{
		{990, 50, 48, 36},
		{970, 70, 48, 36},
		{1010, 65, 48, 36},
		{985, 45, 48, 36},
		{1000, 80, 48, 36},
	}
	refs := make([]Frame, len(rects))
	for i, rect := range rects {
		wide := solidFrame(frameWidth, frameHeight, 70)
		paintTestLogo(&wide, rect, 70)
		refs[i] = cropFrame(wide, rect)
	}
	missing := solidFrame(frameWidth, frameHeight, 90)
	for y := 0; y < missing.H; y++ {
		for x := 0; x < missing.W; x++ {
			value := byte((x*37 + y*19 + x*y*3) % 256)
			i := (y*missing.W + x) * 3
			missing.Pix[i], missing.Pix[i+1], missing.Pix[i+2] = value, value, value
		}
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for i, rect := range rects {
			if _, _, err := findLogo(missing, refs[i], rect, .8, 1.5); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func TestFindLogoDoesNotSearchOtherCorners(t *testing.T) {
	rect := Rect{990, 50, 48, 36}
	wide := solidFrame(frameWidth, frameHeight, 70)
	paintTestLogo(&wide, rect, 70)
	ref := cropFrame(wide, rect)
	otherCorner := solidFrame(frameWidth, frameHeight, 70)
	paintTestLogo(&otherCorner, Rect{240, 50, 48, 36}, 70)
	if score, found, err := findLogo(otherCorner, ref, rect, .8, 1.5); err != nil || found {
		t.Fatalf("other corner: score=%f found=%t err=%v", score, found, err)
	}
}

func TestFindLogoRejectsSimilarMark(t *testing.T) {
	rect := Rect{990, 50, 48, 36}
	wide := solidFrame(frameWidth, frameHeight, 70)
	paintTestLogo(&wide, rect, 70)
	ref := cropFrame(wide, rect)
	advert := solidFrame(frameWidth, frameHeight, 70)
	shifted := Rect{820, 130, 48, 36}
	paintTestLogo(&advert, shifted, 70)
	// Erase the foot and short upper stroke, leaving a similar vertical mark.
	for y := shifted.Y; y < shifted.Y+shifted.H; y++ {
		for x := shifted.X; x < shifted.X+shifted.W; x++ {
			if y >= shifted.Y+shifted.H-10 && x >= shifted.X+13 || x >= shifted.X+shifted.W-15 {
				i := (y*advert.W + x) * 3
				advert.Pix[i], advert.Pix[i+1], advert.Pix[i+2] = 70, 70, 70
			}
		}
	}
	if score, found, err := findLogo(advert, ref, rect, .8, 1.5); err != nil || found {
		t.Fatalf("similar mark: score=%f found=%t err=%v", score, found, err)
	}
}

func TestFindLogoKeepsFlatReferencesAtOriginalPosition(t *testing.T) {
	rect := Rect{990, 50, 32, 32}
	frame := solidFrame(frameWidth, frameHeight, 0)
	ref := solidFrame(rect.W, rect.H, 255)
	for y := rect.Y + 50; y < rect.Y+50+rect.H; y++ {
		for x := rect.X - 100; x < rect.X-100+rect.W; x++ {
			i := (y*frame.W + x) * 3
			frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2] = 255, 255, 255
		}
	}
	if score, found, err := findLogo(frame, ref, rect, .8, 1.5); err != nil || found {
		t.Fatalf("flat reference moved: score=%f found=%t err=%v", score, found, err)
	}
}

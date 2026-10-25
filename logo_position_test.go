package main

import "testing"

func findPositionTestLogo(template *logoTemplate, frame Frame) LogoResult {
	var plane logoPlane
	return template.find(frame, &plane, .8)
}

func TestLogoSearchFindsDistantPositionsInEachCorner(t *testing.T) {
	ref := compositeTestLogo(48, 36, 0, 1, true)
	for _, corner := range []string{"topLeft", "topRight", "bottomLeft", "bottomRight"} {
		t.Run(corner, func(t *testing.T) {
			mirror := func(r Rect) Rect {
				if corner == "topRight" || corner == "bottomRight" {
					r.X = 960 - r.X - r.W
				}
				if corner == "bottomLeft" || corner == "bottomRight" {
					r.Y = 720 - r.Y - r.H
				}
				return r
			}
			for _, test := range []struct {
				name   string
				target Rect
			}{
				{"horizontal", Rect{300, 20, 48, 36}},
				{"vertical", Rect{20, 180, 48, 36}},
				{"diagonal", Rect{300, 180, 48, 36}},
				{"smaller90", Rect{300, 180, 43, 32}},
				{"smaller95", Rect{300, 180, 46, 34}},
				{"larger105", Rect{300, 180, 50, 38}},
				{"larger110", Rect{300, 180, 53, 40}},
				{"frameEdge", Rect{0, 200, 48, 36}},
			} {
				t.Run(test.name, func(t *testing.T) {
					original, target := mirror(Rect{8, 8, 48, 36}), mirror(test.target)
					template := prepareLogo("position", ref, original, explicitTestMask(48, 36))
					frame := solidFrame(960, 720, 90)
					insertTestFrame(&frame, ref, target)
					for n := 1; n <= 3; n++ {
						result := findPositionTestLogo(template, frame)
						if absInt(result.Rect.X-target.X) > 2 || absInt(result.Rect.Y-target.Y) > 2 || result.Rect.W != target.W || result.Rect.H != target.H || result.Threshold < .85 {
							t.Fatalf("frame %d: target %+v, got %+v", n, target, result)
						}
						if n < 3 {
							if result.Decision != "uncertain" || result.UncertaintyReason != "logo_position_pending" || template.last != original || len(template.color.samples) != 0 {
								t.Fatalf("unconfirmed position used on frame %d: %+v", n, result)
							}
						} else if result.Decision != "present" || result.UncertaintyReason != "" || template.last != result.Rect || template.lastScale != result.Scale || len(template.color.samples) != 1 {
							t.Fatalf("position not confirmed on third frame: %+v", result)
						}
					}
					// Confirmed positions use the fast path on subsequent frames.
					var plane logoPlane
					if result := template.find(frame, &plane, .8); result.Decision != "present" || plane.w != 0 {
						t.Fatalf("confirmed position searched again: %+v", result)
					}
					frame = solidFrame(960, 720, 90)
					insertTestFrame(&frame, ref, original)
					if result := findPositionTestLogo(template, frame); result.Decision != "present" || template.last != original || template.lastScale != 1 {
						t.Fatalf("return to original position: %+v", result)
					}
				})
			}
		})
	}
}

func TestLogoPositionConfirmationResetsAndAllowsSmallJitter(t *testing.T) {
	ref := compositeTestLogo(48, 36, 0, 1, true)
	original := Rect{8, 8, 48, 36}
	target := Rect{300, 180, 48, 36}
	for _, interruption := range []string{"missing", "otherPosition", "otherScale", "original", "nearby", "uncertain"} {
		t.Run(interruption, func(t *testing.T) {
			template := prepareLogo("position", ref, original, explicitTestMask(48, 36))
			frameAt := func(r Rect, logo Frame) Frame {
				f := solidFrame(960, 720, 90)
				insertTestFrame(&f, logo, r)
				return f
			}
			frame := frameAt(target, ref)
			for n := 0; n < 2; n++ {
				if result := findPositionTestLogo(template, frame); result.UncertaintyReason != "logo_position_pending" {
					t.Fatalf("initial pending frame: %+v", result)
				}
			}
			var interrupted Frame
			switch interruption {
			case "missing":
				interrupted = solidFrame(960, 720, 90)
			case "otherPosition":
				interrupted = frameAt(Rect{330, 200, 48, 36}, ref)
			case "otherScale":
				interrupted = frameAt(Rect{300, 180, 53, 40}, ref)
			case "original":
				interrupted = frameAt(original, ref)
			case "nearby":
				interrupted = frameAt(Rect{38, 18, 48, 36}, ref)
			case "uncertain":
				// Shape scores in the uncertainty band cannot confirm a position.
				noisy := ref.clone()
				for i, v := range noisy.Pix {
					noisy.Pix[i] = byte(max(0, min(255, int(v)+(i*31+i*i)%11-5)))
				}
				interrupted = frameAt(target, noisy)
			}
			if interruption == "uncertain" {
				var plane logoPlane
				result := template.find(interrupted, &plane, 1)
				if result.Decision != "uncertain" || result.UncertaintyReason != "" || template.pendingFrames != 0 {
					t.Fatalf("uncertain match advanced confirmation: %+v", result)
				}
			} else {
				findPositionTestLogo(template, interrupted)
			}
			for n := 1; n <= 3; n++ {
				// A small variation stays within four pixels of the first hit.
				jittered := target
				jittered.X += n - 1
				result := findPositionTestLogo(template, frameAt(jittered, ref))
				if n < 3 && result.UncertaintyReason != "logo_position_pending" || n == 3 && result.Decision != "present" {
					t.Fatalf("frame %d after %s: %+v", n, interruption, result)
				}
			}
		})
	}
}

func TestLogoFullCornerSearchRejectsBackgroundAndOtherCorners(t *testing.T) {
	ref := compositeTestLogo(48, 36, 0, 1, true)
	original := Rect{8, 8, 48, 36}
	for _, kind := range []string{"flat", "line", "textured", "topRight", "bottomLeft", "bottomRight", "acrossCentre"} {
		t.Run(kind, func(t *testing.T) {
			template := prepareLogo("position", ref, original, explicitTestMask(48, 36))
			frame := solidFrame(960, 720, 90)
			switch kind {
			case "line":
				insertTestFrame(&frame, solidFrame(6, 60, 245), Rect{300, 180, 6, 60})
			case "textured":
				for i := 0; i < len(frame.Pix); i += 3 {
					v := byte((i*37 + i*i*3) % 251)
					frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2] = v, v, v
				}
			case "topRight":
				insertTestFrame(&frame, ref, Rect{700, 180, 48, 36})
			case "bottomLeft":
				insertTestFrame(&frame, ref, Rect{300, 500, 48, 36})
			case "bottomRight":
				insertTestFrame(&frame, ref, Rect{700, 500, 48, 36})
			case "acrossCentre":
				insertTestFrame(&frame, ref, Rect{456, 180, 48, 36})
			}
			for n := 0; n < 3; n++ {
				if result := findPositionTestLogo(template, frame); result.Decision != "missing" || result.UncertaintyReason != "" || template.last != original {
					t.Fatalf("%s frame %d accepted or pending: %+v", kind, n, result)
				}
			}
		})
	}
}

func TestPendingLogoPositionDoesNotSwitchOrSuppressAnotherReference(t *testing.T) {
	ref := compositeTestLogo(48, 36, 0, 1, true)
	original, target := Rect{8, 8, 48, 36}, Rect{300, 180, 48, 36}
	frame := solidFrame(960, 720, 90)
	insertTestFrame(&frame, ref, target)
	for _, placeholder := range []bool{false, true} {
		a, cfg, _, _ := debugTestApp(t, false, false)
		template := prepareLogo("position", ref, original, explicitTestMask(48, 36))
		cfg.ProgramCooldownSeconds, cfg.PlaceholderMinSeconds = 0, 0
		cfg.PresentFrames, cfg.MissingFrames = 1, 1
		a.cfg = cfg
		a.status.Mode = "sender"
		if placeholder {
			a.status.Mode = "placeholder"
		}
		mode := a.status.Mode
		a.status.Matches, a.status.Misses = 5, 5
		for n := 0; n < 2; n++ {
			result := findPositionTestLogo(template, frame)
			a.observeLogo(result, template, frame, cfg, a.active.URL)
			if a.status.Mode != mode || a.status.Matches != 0 || a.status.Misses != 0 || a.status.LogoPresent != nil {
				t.Fatalf("pending position triggered switch: %+v", a.status)
			}
			other := prepareLogo("other", ref, target, explicitTestMask(48, 36))
			confirmed := findPositionTestLogo(other, frame)
			if confirmed.Decision != "present" || !betterLogoResult(confirmed, result) {
				t.Fatalf("pending position suppressed confirmed reference: %+v vs %+v", result, confirmed)
			}
		}
		result := findPositionTestLogo(template, frame)
		a.observeLogo(result, template, frame, cfg, a.active.URL)
		if result.Decision != "present" || a.status.Mode != "sender" || a.status.Matches != 1 {
			t.Fatalf("confirmed position did not allow return: %+v / %+v", result, a.status)
		}
	}
}

func BenchmarkMaskedLogoTrackedFiveReferences(b *testing.B) {
	for _, moved := range []bool{false, true} {
		name := "original"
		if moved {
			name = "distant"
		}
		b.Run(name, func(b *testing.B) {
			ref := compositeTestLogo(48, 36, 0, 1, true)
			original, target := Rect{1200, 8, 48, 36}, Rect{1200, 8, 48, 36}
			if moved {
				target = Rect{750, 230, 48, 36}
			}
			frame := solidFrame(frameWidth, frameHeight, 90)
			insertTestFrame(&frame, ref, target)
			refs := make([]*logoTemplate, 5)
			for i := range refs {
				refs[i] = prepareLogo("position", ref, original, explicitTestMask(48, 36))
				for n := 0; n < 3; n++ {
					findPositionTestLogo(refs[i], frame)
				}
				if refs[i].last != target {
					b.Fatal("benchmark position not confirmed")
				}
			}
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				var plane logoPlane
				for _, ref := range refs {
					ref.find(frame, &plane, .8)
				}
			}
		})
	}
}

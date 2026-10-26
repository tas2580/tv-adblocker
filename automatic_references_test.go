package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func autoTestFrame(scene int, logo bool) Frame {
	f := compositeTestLogo(320, 180, scene, .8, false)
	if logo {
		for y := 0; y < 28; y++ {
			for x := 0; x < 40; x++ {
				if logoTestShape(x, y, 40, 28) {
					for c := 0; c < 3; c++ {
						i := ((y+6)*f.W+x+12)*3 + c
						f.Pix[i] = byte(.95*245 + .05*float64(f.Pix[i]))
					}
				}
			}
		}
	}
	return f
}

func TestAutomaticSearchRequiresIndependentValidation(t *testing.T) {
	s := automaticSearch{state: "searching", session: 1, last: -1}
	for stamp := 0.; stamp <= 30; stamp += 2 {
		if ready := s.observe(autoTestFrame(int(stamp/2), true), testObservation(stamp, "uncertain"), .8, 2, nil); len(ready) > 0 {
			t.Fatal("training frames activated a reference")
		}
	}
	if len(s.candidates) == 0 {
		t.Fatal("stable logo was not found")
	}
	candidates := append([]*autoCandidate(nil), s.candidates...)
	for stamp := 32.; stamp < 330; stamp += 2 {
		if ready := s.observe(autoTestFrame(int(stamp/2), true), testObservation(stamp, "uncertain"), .8, 2, nil); len(ready) > 0 {
			t.Fatal("reference activated before independent window")
		}
	}
	count := 0
	for stamp := 330.; stamp <= 332; stamp += 2 {
		count += len(s.observe(autoTestFrame(int(stamp/2), true), testObservation(stamp, "uncertain"), .8, 2, nil))
	}
	if count == 0 {
		for _, c := range candidates {
			t.Logf("candidate %+v tested=%d matched=%d", c.rect, c.tested, c.matched)
		}
		t.Fatal("validated stable logo was not activated")
	}
}
func TestAutomaticSearchRejectsStaticAndChangingBackground(t *testing.T) {
	for _, static := range []bool{true, false} {
		s := automaticSearch{state: "searching", session: 1, last: -1}
		for stamp := 0.; stamp <= 32; stamp += 2 {
			scene := int(stamp / 2)
			if static {
				scene = 0
			}
			s.observe(autoTestFrame(scene, false), testObservation(stamp, "uncertain"), .8, 2, nil)
		}
		if len(s.candidates) > 0 {
			t.Fatalf("background produced candidate (static=%t): %+v", static, s.candidates)
		}
	}
}
func TestAutomaticReferenceSaveDisableAndSuppress(t *testing.T) {
	cfg := detectionTestConfig()
	cfg.DetectionMode = "hybrid"
	active := Channel{Name: "Test", URL: "local://test"}
	a := &App{cfg: cfg, active: active, dataDir: t.TempDir(), status: Status{Running: true, Mode: "monitoring"}}
	var saved *logoTemplate
	for stamp := 0.; stamp <= 330; stamp += 2 {
		o := testObservation(stamp, "uncertain")
		if ref := a.processAutomaticFrame(context.Background(), autoTestFrame(int(stamp/2), true), cfg, active, nil, &o); ref != nil {
			saved = ref
		}
	}
	if saved == nil {
		t.Fatal("no automatic reference saved")
	}
	entries, err := a.referencesFor(active.URL)
	if err != nil || len(entries) != 1 || !entries[0].Automatic || entries[0].ValidationRate < .9 {
		t.Fatalf("entries: %+v %v", entries, err)
	}
	// No actual decoder is needed for reference metadata operations.
	a.status.Running = false
	w := httptest.NewRecorder()
	a.handleReferenceItem(w, httptest.NewRequest(http.MethodPatch, "/api/references/"+saved.id, strings.NewReader(`{"disabled":true}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	entry, _ := a.referenceByID(saved.id)
	if !entry.Disabled {
		t.Fatal("reference not disabled")
	}
	w = httptest.NewRecorder()
	a.handleReferenceItem(w, httptest.NewRequest(http.MethodDelete, "/api/references/"+saved.id, nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if len(a.profiles[active.URL].Blocked) != 1 {
		t.Fatal("deleted automatic region was not suppressed")
	}
	if _, err := os.Stat(a.referenceFile(saved.id)); !os.IsNotExist(err) {
		t.Fatal("reference image retained", err)
	}
	a.detection.automatic = nil
	for stamp := 0.; stamp <= 32; stamp += 2 {
		o := testObservation(stamp, "uncertain")
		a.processAutomaticFrame(context.Background(), autoTestFrame(int(stamp/2), true), cfg, active, nil, &o)
	}
	if len(a.detection.automatic.candidates) > 0 {
		t.Fatal("deleted region proposed again")
	}
}

func automaticValidationFixture(t *testing.T) (Frame, Frame, *autoCandidate) {
	t.Helper()
	present, absent := autoTestFrame(0, true), autoTestFrame(0, false)
	r := Rect{X: 12, Y: 6, W: 40, H: 28}
	frame := cropFrame(present, r)
	mask := automaticLogoMask(frame)
	c := &autoCandidate{rect: r, frame: frame, mask: mask, template: prepareLogo("", frame, r, mask)}
	if !automaticLogoShape(mask, frame.W, frame.H) {
		t.Fatal("validation fixture does not pass the shape gate")
	}
	if c.template.compare(present, r, 1, .8).Decision != "present" || c.template.compare(absent, r, 1, .8).Decision == "present" {
		t.Fatal("validation fixture cannot distinguish presence from absence")
	}
	return present, absent, c
}

func TestAutomaticValidationVisibilityOverFiveMinutes(t *testing.T) {
	for _, test := range []struct {
		name    string
		missing func(int) bool
		want    bool
	}{
		{"continuous", func(i int) bool { return false }, true},
		{"brief interruption", func(i int) bool { return i >= 50 && i <= 52 }, true},
		{"long interruption", func(i int) bool { return i >= 50 && i <= 55 }, false},
		{"loss in formerly unchecked minute", func(i int) bool { return i >= 5 && i <= 10 }, false},
		{"90 percent", func(i int) bool { return i%10 == 1 }, true},
		{"below 90 percent", func(i int) bool { return i%10 == 1 || i == 149 }, false},
		{"missing final frame", func(i int) bool { return i == 150 }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			present, absent, candidate := automaticValidationFixture(t)
			s := automaticSearch{state: "validating", session: 1, last: 0, candidates: []*autoCandidate{candidate}}
			activated := false
			for i := 1; i <= 150; i++ {
				frame := present
				if test.missing(i) {
					frame = absent
				}
				ready := s.observe(frame, testObservation(float64(i*2), "uncertain"), .8, 2, nil)
				if len(ready) > 0 {
					if i < 150 || ready[0] != candidate {
						t.Fatal("candidate activated before five independent minutes")
					}
					activated = true
				}
			}
			if activated != test.want {
				t.Fatalf("activated = %t, want %t; tested=%d matched=%d", activated, test.want, candidate.tested, candidate.matched)
			}
		})
	}
}

func TestAutomaticValidationNeedsThirtyChecks(t *testing.T) {
	present, _, candidate := automaticValidationFixture(t)
	s := automaticSearch{state: "validating", session: 1, last: 0, candidates: []*autoCandidate{candidate}}
	for i := 1; i <= 30; i++ {
		// Slow configured sampling must not activate after only ten observations.
		ready := s.observe(present, testObservation(float64(i*30), "uncertain"), .8, 30, nil)
		if i < 30 && len(ready) > 0 {
			t.Fatal("candidate activated with fewer than 30 checks")
		}
		if i == 30 && (len(ready) != 1 || ready[0] != candidate) {
			t.Fatal("candidate did not activate after enough confirmed observations")
		}
	}
}

func TestAutomaticValidationRetainsOtherReadyCandidates(t *testing.T) {
	present, _, first := automaticValidationFixture(t)
	first.tested, first.matched, first.lastMatch = 149, 149, 298
	second := *first
	s := automaticSearch{state: "validating", session: 1, last: 298, candidates: []*autoCandidate{first, &second}}
	ready := s.observe(present, testObservation(300, "uncertain"), .8, 2, nil)
	if len(ready) != 1 || ready[0] != first || len(s.candidates) != 1 || s.candidates[0] != &second {
		t.Fatal("saving the first validated candidate discarded another")
	}
	ready = s.observe(present, testObservation(302, "uncertain"), .8, 2, nil)
	if len(ready) != 1 || ready[0] != &second {
		t.Fatal("second candidate had to repeat validation")
	}
}

func TestAutomaticSearchRestartsAfterDiscontinuity(t *testing.T) {
	for _, test := range []struct {
		name     string
		stamp    float64
		session  uint64
		interval int
		reset    bool
	}{
		{"decoder change", 22, 2, 2, true},
		{"backwards time", 19, 1, 2, true},
		{"observation gap", 31, 1, 2, true},
		{"configured interval gap", 36, 1, 5, true},
		{"permitted configured interval", 35, 1, 5, false},
		{"duplicate time", 20, 1, 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			present, _, candidate := automaticValidationFixture(t)
			candidate.tested, candidate.matched, candidate.lastMatch = 10, 10, 20
			s := automaticSearch{state: "validating", session: 1, last: 20, candidates: []*autoCandidate{candidate}}
			o := testObservation(test.stamp, "uncertain")
			o.Session = test.session
			if ready := s.observe(present, o, .8, test.interval, nil); len(ready) != 0 {
				t.Fatal("discontinuity activated a reference")
			}
			if test.reset {
				if len(s.candidates) != 0 || s.start != test.stamp || len(s.frames[0]) != 1 {
					t.Fatal("discontinuity retained candidate or old training frames")
				}
			} else if len(s.candidates) != 1 || s.candidates[0] != candidate {
				t.Fatal("continuous sampling discarded a valid candidate")
			}
		})
	}
}

func TestAutomaticReferenceRechecksMaskBeforeSaving(t *testing.T) {
	present, _, candidate := automaticValidationFixture(t)
	// Keep the comparison template valid but make its proposed saved mask a line.
	candidate.mask = make([]byte, candidate.frame.W*candidate.frame.H)
	for y := 0; y < candidate.frame.H; y++ {
		for x := 0; x < 3; x++ {
			candidate.mask[y*candidate.frame.W+x] = 255
		}
	}
	candidate.tested, candidate.matched, candidate.lastMatch = 149, 149, 298
	cfg := detectionTestConfig()
	cfg.DetectionMode = "compare"
	a := &App{cfg: cfg, active: Channel{URL: "local://test"}, dataDir: t.TempDir()}
	a.detection.automatic = &automaticSearch{state: "validating", session: 1, last: 298, candidates: []*autoCandidate{candidate}}
	o := testObservation(300, "uncertain")
	if ref := a.processAutomaticFrame(context.Background(), present, cfg, a.active, nil, &o); ref != nil {
		t.Fatal("invalid saved mask bypassed the final shape gate")
	}
	entries, err := a.referencesFor(a.active.URL)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid automatic reference was persisted", entries, err)
	}
}

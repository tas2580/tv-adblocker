package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func detectionTestConfig() Config {
	c := defaultConfig()
	c.normalize()
	c.ProgramCooldownSeconds = 0
	c.PlaceholderMinSeconds = 0
	return c
}
func testObservation(stamp float64, decision string) DetectionObservation {
	return DetectionObservation{Time: stamp, Session: 1, Logo: LogoResult{Decision: decision, Threshold: .8, Score: .2}}
}
func TestDetectionConflictDeadlineAndFreshEvidence(t *testing.T) {
	cfg := detectionTestConfig()
	for _, resolved := range []bool{false, true} {
		var m decisionMachine
		m.step(testObservation(0, "uncertain"), cfg, true, SenderProfile{})
		for i := 1; i <= 3; i++ {
			o := testObservation(float64(i*2), "missing")
			o.Logo.Score = .7
			d := m.step(o, cfg, true, SenderProfile{})
			if d.Action != "" {
				t.Fatal("switched before fresh evidence", d)
			}
		}
		var d DetectionDecision
		for stamp := 8.; stamp <= 16; stamp += 2 {
			o := testObservation(stamp, "missing")
			if !resolved {
				o.Logo.Score = .7
			}
			d = m.step(o, cfg, true, SenderProfile{})
			if resolved && d.Action == "placeholder" {
				break
			}
			if !resolved && d.Action != "" {
				t.Fatal("unresolved evidence switched", d)
			}
		}
		if resolved && d.Action != "placeholder" {
			t.Fatalf("fresh strong evidence did not resolve suspicion: %+v", d)
		}
		if !resolved && (d.State != "program" || d.Reason != "unresolved_kept_program" || d.Remaining != 0) {
			t.Fatalf("deadline not enforced: %+v", d)
		}
	}
}
func TestDetectionSignalsNeverReplaceLogoAndSessionReset(t *testing.T) {
	cfg := detectionTestConfig()
	var m decisionMachine
	for i := 0; i < 20; i++ {
		o := testObservation(float64(i*2), "uncertain")
		o.Signals = SignalSnapshot{Available: SignalAvailability{Black: true, Silence: true}, Events: []SignalEvent{{Time: o.Time, Kind: "black"}, {Time: o.Time, Kind: "silence"}}}
		if d := m.step(o, cfg, true, SenderProfile{Active: true}); d.Action != "" {
			t.Fatalf("signals switched without logo: %+v", d)
		}
	}
	m = decisionMachine{}
	m.step(testObservation(0, "missing"), cfg, true, SenderProfile{})
	m.step(testObservation(2, "missing"), cfg, true, SenderProfile{})
	o := testObservation(4, "missing")
	o.Session = 2
	if d := m.step(o, cfg, true, SenderProfile{}); d.Action != "" || d.Misses != 1 {
		t.Fatalf("cross-session evidence: %+v", d)
	}
}
func TestDetectionLocksAndReturn(t *testing.T) {
	cfg := detectionTestConfig()
	cfg.PlaceholderMinSeconds = 10
	cfg.ProgramCooldownSeconds = 20
	var m decisionMachine
	for i := 0; i < 3; i++ {
		m.step(testObservation(float64(i*2), "missing"), cfg, true, SenderProfile{})
	}
	for stamp := 6.; stamp < 14; stamp += 2 {
		if d := m.step(testObservation(stamp, "present"), cfg, true, SenderProfile{}); d.Action != "" {
			t.Fatal("minimum ad time ignored", d)
		}
	}
	for stamp := 14.; stamp <= 18; stamp += 2 {
		d := m.step(testObservation(stamp, "present"), cfg, true, SenderProfile{})
		if stamp == 18 && d.Action != "sender" {
			t.Fatalf("did not return: %+v", d)
		}
	}
	for stamp := 20.; stamp < 38; stamp += 2 {
		if d := m.step(testObservation(stamp, "missing"), cfg, true, SenderProfile{}); d.Action != "" {
			t.Fatal("program cooldown ignored", d)
		}
	}
}

func TestDetectionKeepsPlaybackLocksAcrossDecoderSessions(t *testing.T) {
	cfg := detectionTestConfig()
	cfg.PlaceholderMinSeconds = 10
	var m decisionMachine
	for stamp := 0.; stamp <= 4; stamp += 2 {
		m.step(testObservation(stamp, "missing"), cfg, true, SenderProfile{})
	}
	for stamp := 6.; stamp <= 12; stamp += 2 {
		o := testObservation(stamp, "present")
		o.Session = 2
		if d := m.step(o, cfg, true, SenderProfile{}); d.Action != "" {
			t.Fatal("restart lost minimum placeholder lock", d)
		}
	}
}

func TestCorroboratingSignalsResolveConflict(t *testing.T) {
	cfg := detectionTestConfig()
	for _, kind := range []string{"none", "black", "pair", "learned"} {
		var m decisionMachine
		m.step(testObservation(0, "uncertain"), cfg, true, SenderProfile{})
		var d DetectionDecision
		for stamp := 2.; stamp <= 6; stamp += 2 {
			o := testObservation(stamp, "missing")
			if stamp == 6 {
				o.Signals.Available = SignalAvailability{Black: true, Silence: true, Scene: true}
				if kind != "none" {
					o.Signals.Events = []SignalEvent{{Time: stamp - 1, Kind: "black"}}
				}
				if kind == "pair" {
					o.Signals.Events = append(o.Signals.Events, SignalEvent{Time: stamp - 1, Kind: "silence"})
				}
				if kind == "learned" {
					o.Signals.Events = append(o.Signals.Events, SignalEvent{Time: stamp - 1, Kind: "scene"})
				}
			}
			d = m.step(o, cfg, true, SenderProfile{Active: kind == "learned", TransitionRate: 1})
		}
		want := kind == "pair" || kind == "learned"
		if (d.Action == "placeholder") != want {
			t.Fatalf("%s: %+v", kind, d)
		}
	}
}

func TestCompareAndHybridControlKodiSeparately(t *testing.T) {
	for _, mode := range []string{"compare", "hybrid"} {
		t.Run(mode, func(t *testing.T) {
			a, cfg, ref, frame := debugTestApp(t, false, false)
			cfg.DetectionMode = mode
			cfg.MissingFrames = 3
			cfg.PlaceholderMinSeconds = 0
			cfg.ProgramCooldownSeconds = 0
			a.cfg = cfg
			o := testObservation(0, "uncertain")
			a.observeLogoSample(o.Logo, ref, frame, cfg, a.active.URL, &o)
			for stamp := 2.; stamp <= 6; stamp += 2 {
				o = testObservation(stamp, "missing")
				o.Logo.Score = .7
				a.observeLogoSample(o.Logo, ref, frame, cfg, a.active.URL, &o)
			}
			if mode == "compare" {
				if a.status.Mode != "placeholder" {
					t.Fatalf("compare did not retain classic playback: %+v", a.status)
				}
			} else {
				if a.status.Mode != "monitoring" {
					t.Fatalf("hybrid ignored conflict: %+v", a.status)
				}
				for stamp := 8.; stamp <= 10; stamp += 2 {
					o = testObservation(stamp, "missing")
					a.observeLogoSample(o.Logo, ref, frame, cfg, a.active.URL, &o)
				}
				if a.status.Mode != "placeholder" {
					t.Fatalf("hybrid did not switch after fresh evidence: %+v", a.status)
				}
			}
		})
	}
}
func TestSenderProfileRequiresCompleteCleanCycles(t *testing.T) {
	cfg := detectionTestConfig()
	var tracker profileTracker
	var profile SenderProfile
	stamp := 0.
	for cycle := 0; cycle < 5; cycle++ {
		for _, decision := range []string{"present", "present", "present", "missing", "missing", "missing", "missing", "missing", "present", "present", "present"} {
			if duration, tr, ok := tracker.observe(testObservation(stamp, decision), cfg); ok {
				profile.add(duration, tr)
			}
			stamp += 2
		}
	}
	if profile.Breaks != 5 || !profile.Active || profile.TypicalDuration != 10 {
		t.Fatalf("profile: %+v", profile)
	}
	tracker = profileTracker{}
	for _, decision := range []string{"present", "present", "present", "missing", "missing", "missing", "uncertain", "missing", "present", "present", "present"} {
		if _, _, ok := tracker.observe(testObservation(stamp, decision), cfg); ok {
			t.Fatal("uncertain segment trained profile")
		}
		stamp += 2
	}
}
func TestReplayMetricsAndNoKodi(t *testing.T) {
	cfg := detectionTestConfig()
	rec := ObservationRecording{Settings: replaySettings(cfg), Labels: []ObservationLabel{{0, 10, "program"}, {10, 20, "ad"}, {20, 30, "program"}}}
	for stamp := 0.; stamp <= 30; stamp += 2 {
		decision := "present"
		if stamp >= 10 && stamp < 20 {
			decision = "missing"
		}
		rec.Observations = append(rec.Observations, RecordedObservation{DetectionObservation: testObservation(stamp, decision)})
	}
	report, err := replayObservations(rec)
	if err != nil {
		t.Fatal(err)
	}
	if report.Classic.MissedBreaks != 0 || report.Hybrid.FalseSwitches != 0 || len(report.Hybrid.AdDelays) != 1 || report.Hybrid.AdDelays[0] != 4 || report.Hybrid.ReturnDelays[0] != 4 {
		t.Fatalf("report: %+v", report)
	}
	a := &App{httpClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) { t.Fatal("replay contacted Kodi"); return nil, nil })}}
	data, _ := json.Marshal(rec)
	w := httptest.NewRecorder()
	a.handleDetectionReplay(w, httptest.NewRequest("POST", "/api/detection/replay", bytes.NewReader(data)))
	if w.Code != 200 {
		t.Fatalf("replay: %d %s", w.Code, w.Body)
	}
	rec.Labels = append(rec.Labels, ObservationLabel{25, 28, "ad"})
	if _, err := replayObservations(rec); err == nil {
		t.Fatal("overlapping labels accepted")
	}
}
func TestDetectionConfigCompatibility(t *testing.T) {
	a := &App{cfg: defaultConfig(), configPath: filepath.Join(t.TempDir(), "config.json")}
	a.cfg.DetectionMode = "compare"
	if err := os.WriteFile(a.configPath, []byte(`{"similarity_minimum":0.7}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.loadConfig(); err != nil {
		t.Fatal(err)
	}
	if a.cfg.DetectionMode != "classic" {
		t.Fatal("old config migrated to new mode")
	}
	a.cfg.DetectionMode = "invalid"
	if a.cfg.validateChannelSimilarity() == nil {
		t.Fatal("invalid mode accepted")
	}
}
func TestMediaSignalParsingAndAlignment(t *testing.T) {
	s := newMediaSignals(SignalAvailability{Black: true, Silence: true, Scene: true, Timing: true}, 4)
	ctx := context.Background()
	for _, line := range []string{"Stream #0:1: Audio: aac", "[blackdetect] black_start:2.0 black_end:2.2 black_duration:0.2", "[silencedetect] silence_start: 2.1", "[showinfo@sample] n: 1 pts: 2048 pts_time:4.0 s:1280x720"} {
		s.parse(ctx, line)
	}
	o := s.sample(ctx, 0, 2)
	if o.Time != 4 || o.Session != 4<<32 || !o.Signals.Available.Silence || len(o.Signals.Events) != 2 || !transitionSignal(o) {
		t.Fatalf("signals: %+v", o)
	}
	s.parse(ctx, "[showinfo@sample] n: 2 pts: 0 pts_time:0.0 s:1280x720")
	if o = s.sample(ctx, 1, 2); o.Session != (4<<32)+1 || len(o.Signals.Events) != 0 {
		t.Fatalf("stale signals survived discontinuity: %+v", o)
	}
}
func TestLocalVideoSignalsWithAndWithoutAudio(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	for _, audio := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-audio", true: "audio"}[audio], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "sample.mkv")
			args := []string{"-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=white:size=320x180:rate=25:duration=3,drawbox=color=black:t=fill:enable='between(t,0.8,1.0)'"}
			if audio {
				args = append(args, "-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo", "-t", "3", "-c:a", "pcm_s16le")
			}
			args = append(args, "-c:v", "ffv1", "-threads", "1", path)
			if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v %s", err, out)
			}
			ref := prepareLogo("test", compositeTestLogo(48, 36, 0, .8, true), Rect{W: 48, H: 36}, explicitTestMask(48, 36))
			rec, err := analyzeLocalVideo(ctx, path, detectionTestConfig(), []*logoTemplate{ref})
			if err != nil {
				t.Fatal(err)
			}
			if len(rec.Observations) < 1 {
				t.Fatal("no video observations")
			}
			last := rec.Observations[len(rec.Observations)-1]
			if last.Signals.Available.Silence != audio {
				t.Fatalf("audio availability: %+v", last.Signals)
			}
			if !last.Signals.Available.Timing {
				t.Fatal("sample timestamps unavailable")
			}
			black := false
			for _, e := range last.Signals.Events {
				if e.Kind == "black" {
					black = true
				}
			}
			if !black {
				t.Fatalf("short black transition between logo samples was lost: %+v", last.Signals)
			}
		})
	}
}
func TestDetectionUITranslationsAndSyntax(t *testing.T) {
	for _, lang := range []string{"de", "en"} {
		data, _ := localeFiles.ReadFile("locales/" + lang + ".json")
		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"detection.heading", "detection.reason.unresolved_kept_program", "detection.auto.validating", "detection.pending"} {
			if messages[key] == "" {
				t.Fatalf("%s lacks %s", lang, key)
			}
		}
	}
	if node, err := exec.LookPath("node"); err == nil {
		cmd := exec.Command(node, "--check")
		cmd.Stdin = strings.NewReader(detectionJS)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("UI syntax: %v %s", err, out)
		}
	}
}

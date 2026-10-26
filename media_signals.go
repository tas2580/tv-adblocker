package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

type mediaSignals struct {
	mu          sync.Mutex
	available   SignalAvailability
	events      []SignalEvent
	frames      chan float64
	format      string
	crop        string
	lastPTS     float64
	epoch       uint64
	tail        string
	audioFilter bool
}

var mediaNumber = regexp.MustCompile(`(?:pts_time:|black_start:|silence_start:|silence_end:|time:|\bt:)\s*(-?[0-9]+(?:\.[0-9]+)?)`)
var mediaSize = regexp.MustCompile(`\bs:([0-9]+x[0-9]+)\b`)
var mediaCrop = regexp.MustCompile(`\bcrop=([0-9]+:[0-9]+:[0-9]+:[0-9]+)`)

func signalCapabilities(ctx context.Context) SignalAvailability {
	out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-filters").Output()
	if err != nil {
		return SignalAvailability{}
	}
	s := string(out)
	has := func(name string) bool {
		for _, line := range strings.Split(s, "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && f[1] == name {
				return true
			}
		}
		return false
	}
	return SignalAvailability{Black: has("blackdetect"), Silence: has("silencedetect"), Scene: has("scdet"), Format: has("showinfo"), Timing: has("showinfo"), VisibleFormat: has("cropdetect")}
}

func newMediaSignals(available SignalAvailability, epoch uint64) *mediaSignals {
	if !available.Timing {
		available = SignalAvailability{}
	}
	// Silence becomes available only after an actual input audio stream is seen.
	// Reserve the low bits for timestamp discontinuities, so they cannot collide
	// with the identity allocated to the next decoder process.
	s := &mediaSignals{available: available, frames: make(chan float64, 16), lastPTS: math.Inf(-1), epoch: epoch << 32, audioFilter: available.Silence}
	s.available.Silence = false
	return s
}

// One input and one decoder feed both native-rate measurements and sampled RGB.
// The second null output also accepts video, so an absent optional audio stream
// never produces an empty output or a failed mapping.
func signalDecoderArgs(base []string, filter string, cap SignalAvailability) []string {
	if !cap.Timing {
		return append(base, "-an", "-vf", filter, "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1")
	}
	measurement := "scale=320:180"
	if cap.VisibleFormat {
		measurement += ",cropdetect=limit=24:round=2:reset=25"
	}
	if cap.Black {
		measurement += ",blackdetect=d=0.08:pix_th=0.10:pic_th=0.98"
	}
	if cap.Scene {
		measurement += ",scdet=threshold=12"
	}
	graph := "[0:v:0]showinfo@source=checksum=0,split=2[main][measure];[main]" + filter + ",showinfo@sample=checksum=0[rgb];[measure]" + measurement + "[signals]"
	args := append(base, "-filter_complex_threads", "1", "-filter_complex", graph, "-map", "[rgb]", "-an", "-pix_fmt", "rgb24", "-threads:v", "1", "-f", "rawvideo", "pipe:1", "-map", "[signals]")
	if cap.Silence {
		args = append(args, "-map", "0:a:0?", "-af", "silencedetect=n=-45dB:d=0.12")
	}
	return append(args, "-f", "null", "-")
}

func (s *mediaSignals) read(ctx context.Context, r io.Reader) {
	defer close(s.frames)
	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 4096), 64*1024)
	for scan.Scan() {
		if !s.parse(ctx, scan.Text()) {
			return
		}
	}
}
func (s *mediaSignals) parse(ctx context.Context, line string) bool {
	s.mu.Lock()
	// Keep only a bounded error tail. Input URLs/credentials are never exported.
	s.tail += line + "\n"
	if len(s.tail) > 4096 {
		s.tail = s.tail[len(s.tail)-4096:]
	}
	if s.audioFilter && strings.Contains(line, "Audio:") {
		s.available.Silence = true
	}
	n := mediaNumber.FindStringSubmatch(line)
	t := 0.0
	if len(n) > 1 {
		t, _ = strconv.ParseFloat(n[1], 64)
	}
	add := func(kind string, value float64) {
		s.events = append(s.events, SignalEvent{Time: t, Kind: kind, Value: value})
		if len(s.events) > 128 {
			s.events = append([]SignalEvent(nil), s.events[len(s.events)-128:]...)
		}
	}
	if len(n) > 1 {
		if strings.Contains(line, "black_start:") {
			add("black", 0)
		}
		if strings.Contains(line, "silence_start:") {
			add("silence", 0)
		}
		if strings.Contains(line, "silence_end:") {
			add("silence_end", 0)
		}
		if strings.Contains(line, "scdet") && strings.Contains(line, "time:") {
			add("scene", 0)
		}
	}
	if strings.Contains(line, "showinfo@source") {
		if size := mediaSize.FindStringSubmatch(line); len(size) > 1 {
			if s.format != "" && s.format != size[1] {
				add("format", 0)
			}
			s.format = size[1]
		}
	}
	if strings.Contains(line, "cropdetect") {
		if crop := mediaCrop.FindStringSubmatch(line); len(crop) > 1 {
			if s.crop != "" && s.crop != crop[1] {
				add("format", 0)
			}
			s.crop = crop[1]
		}
	}
	frame := strings.Contains(line, "showinfo@sample") && strings.Contains(line, " n:") && len(n) > 1
	s.mu.Unlock()
	if frame {
		select {
		case s.frames <- t:
		case <-ctx.Done():
			return false
		}
	}
	return true
}

func (s *mediaSignals) sample(ctx context.Context, index int, interval int) DetectionObservation {
	t := float64(index * interval)
	if s.available.Timing {
		select {
		case stamp, ok := <-s.frames:
			if ok {
				t = stamp
			}
		case <-ctx.Done():
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t < s.lastPTS || t-s.lastPTS > math.Max(30, float64(interval*3)) && !math.IsInf(s.lastPTS, -1) {
		s.epoch++
		s.events = nil
	}
	s.lastPTS = t
	events := make([]SignalEvent, 0, len(s.events))
	for _, e := range s.events {
		if e.Time <= t && t-e.Time <= 10 {
			events = append(events, e)
		}
	}
	return DetectionObservation{Time: t, Session: s.epoch, Signals: SignalSnapshot{Available: s.available, Events: events}}
}

func (s *mediaSignals) errorText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("%s", strings.TrimSpace(s.tail))
}

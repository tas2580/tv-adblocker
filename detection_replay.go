package main

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func readObservationFile(path string) (ObservationRecording, error) {
	var rec ObservationRecording
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		z, err := zip.OpenReader(path)
		if err != nil {
			return rec, err
		}
		defer z.Close()
		for _, f := range z.File {
			if f.Name == "observations.json" {
				r, err := f.Open()
				if err != nil {
					return rec, err
				}
				defer r.Close()
				err = json.NewDecoder(io.LimitReader(r, 16<<20)).Decode(&rec)
				return rec, err
			}
		}
		return rec, errors.New("archive contains no observations.json")
	}
	f, err := os.Open(path)
	if err != nil {
		return rec, err
	}
	defer f.Close()
	err = json.NewDecoder(io.LimitReader(f, 16<<20)).Decode(&rec)
	return rec, err
}

// Local video replay never starts the App automation loop, observes Kodi, or
// calls observeLogo. It decodes a file and feeds only the pure decision engines.
func analyzeLocalVideo(ctx context.Context, path string, cfg Config, refs []*logoTemplate) (ObservationRecording, error) {
	rec := ObservationRecording{State: "ready", Settings: replaySettings(cfg), Observations: []RecordedObservation{}, Labels: []ObservationLabel{}}
	if len(refs) == 0 {
		return rec, errors.New("video replay requires a reference")
	}
	info, err := os.Stat(path)
	if err != nil {
		return rec, err
	}
	if !info.Mode().IsRegular() {
		return rec, errors.New("replay input must be a regular local file")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cap := signalCapabilities(ctx)
	signals := newMediaSignals(cap, 1)
	filter := fmt.Sprintf("fps=1/%d,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", cfg.IntervalSeconds, frameWidth, frameHeight, frameWidth, frameHeight)
	args := signalDecoderArgs([]string{"-hide_banner", "-nostats", "-loglevel", "info", "-protocol_whitelist", "file,pipe", "-i", path}, filter, cap)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return rec, err
	}
	errorsOut, err := cmd.StderrPipe()
	if err != nil {
		return rec, err
	}
	if err = cmd.Start(); err != nil {
		return rec, err
	}
	done := make(chan struct{})
	go func() { defer close(done); signals.read(ctx, errorsOut) }()
	defer func() { cancel(); _ = cmd.Wait(); <-done }()
	rd := bufio.NewReaderSize(out, 3<<20)
	for index := 0; ; index++ {
		pix := make([]byte, frameWidth*frameHeight*3)
		_, err = io.ReadFull(rd, pix)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return rec, err
		}
		o := signals.sample(ctx, index, cfg.IntervalSeconds)
		f := Frame{frameWidth, frameHeight, pix}
		var plane logoPlane
		best := LogoResult{Decision: "uncertain"}
		for _, ref := range refs {
			result := ref.find(f, &plane, cfg.SimilarityMinimum)
			if best.Decision == "uncertain" && best.ReferenceID == "" || betterLogoResult(result, best) {
				best = result
			}
		}
		o.Logo = best
		o = rec.normalizeObservation(o)
		rec.Observations = append(rec.Observations, RecordedObservation{DetectionObservation: o})
		if len(rec.Observations) > 100000 {
			return rec, errors.New("video exceeds 100000 observations")
		}
	}
	<-done
	err = cmd.Wait()
	if err != nil {
		return rec, fmt.Errorf("ffmpeg: %w: %s", err, signals.errorText())
	}
	return rec, nil
}

func runDetectionReplay(path, labels, reference, configPath, dataDir string, w io.Writer) error {
	var rec ObservationRecording
	var err error
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".json" || ext == ".zip" {
		rec, err = readObservationFile(path)
	} else {
		if reference == "" {
			return errors.New("use -replay-reference with a saved reference ID for video input")
		}
		cfg := defaultConfig()
		if b, e := os.ReadFile(configPath); e == nil {
			if err = json.Unmarshal(b, &cfg); err != nil {
				return err
			}
		}
		cfg.normalize()
		a := &App{dataDir: dataDir}
		entry, e := a.referenceByID(reference)
		if e != nil {
			return e
		}
		frame, e := loadReference(a.referenceFile(entry.ID))
		if e != nil {
			return e
		}
		mask, e := a.loadLogoMask(entry.ID, frame)
		if e != nil {
			return e
		}
		ref := prepareLogo(entry.ID, frame, entry.Rect, mask)
		cfg.SimilarityMinimum = cfg.similarityMinimumFor(entry.StreamURL)
		rec, err = analyzeLocalVideo(context.Background(), path, cfg, []*logoTemplate{ref})
	}
	if err != nil {
		return err
	}
	if labels != "" {
		b, e := os.ReadFile(labels)
		if e != nil {
			return e
		}
		if e = json.Unmarshal(b, &rec.Labels); e != nil {
			return e
		}
	}
	report, err := replayObservations(rec)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

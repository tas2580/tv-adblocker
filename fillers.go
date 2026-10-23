package main

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	mathrand "math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type FillerSchedule struct {
	Mode  string `json:"mode"`  // always, annual, once
	Start string `json:"start"` // MM-DD for annual, YYYY-MM-DD for once
	End   string `json:"end"`
}

type Filler struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Source   string         `json:"source"`
	Image    string         `json:"image,omitempty"`
	Target   string         `json:"target,omitempty"`
	Enabled  bool           `json:"enabled"`
	Schedule FillerSchedule `json:"schedule"`
}

// A non-nil list (including an explicitly empty list) is authoritative. This
// prevents deleted entries from being resurrected by legacy config fields.
func (c *Config) migrateFillers() {
	if c.Fillers != nil {
		return
	}
	c.Fillers = []Filler{}
	add := func(f Filler, selected bool) {
		sum := sha256.Sum256([]byte(f.Source + "\x00" + f.Image + "\x00" + f.Target))
		f.ID = fmt.Sprintf("legacy-%x", sum[:12])
		f.Enabled = selected
		f.Schedule.Mode = "always"
		c.Fillers = append(c.Fillers, f)
		if selected {
			c.FallbackFillerID = f.ID
		}
	}
	if c.PlaceholderImage != "" || strings.TrimSpace(c.PlaceholderURL) != "" {
		f := Filler{Name: c.PlaceholderImage, Source: "uploaded-image", Image: c.PlaceholderImage, Target: strings.TrimSpace(c.PlaceholderURL)}
		if f.Image == "" {
			f.Source, f.Name = "kodi-local", f.Target
		}
		add(f, c.PlaceholderSource != "kodi-local")
	}
	for _, target := range normalizedKodiPlaceholders(append(append([]string{}, c.KodiPlaceholders...), c.KodiPlaceholder)) {
		// A legacy uploaded URL can also be present in the Kodi list.
		if len(c.Fillers) > 0 && c.Fillers[0].Source == "kodi-local" && c.Fillers[0].Target == target {
			if c.PlaceholderSource == "kodi-local" && target == c.KodiPlaceholder {
				c.Fillers[0].Enabled = true
				c.FallbackFillerID = c.Fillers[0].ID
			}
			continue
		}
		add(Filler{Name: target, Source: "kodi-local", Target: target}, c.PlaceholderSource == "kodi-local" && target == c.KodiPlaceholder)
	}
}

var fillerImageName = regexp.MustCompile(`^placeholder-image(?:-[a-f0-9]{32})?\.(?:jpg|png|gif|webp|bmp)$`)

func (c Config) validateFillers() error {
	ids, targets := map[string]bool{}, map[string]bool{}
	fallbackFound := c.FallbackFillerID == ""
	for _, f := range c.Fillers {
		if strings.TrimSpace(f.ID) == "" || ids[f.ID] {
			return errors.New("each filler must have a unique, non-empty ID")
		}
		ids[f.ID] = true
		if f.ID == c.FallbackFillerID {
			fallbackFound = true
		}
		var key string
		switch f.Source {
		case "uploaded-image":
			if !fillerImageName.MatchString(f.Image) {
				return fmt.Errorf("filler %q: invalid uploaded image", f.Name)
			}
			key = "image:" + f.Image
		case "kodi-local":
			if strings.TrimSpace(f.Target) == "" {
				return fmt.Errorf("filler %q: Kodi path or URL is required", f.Name)
			}
			key = "kodi:" + strings.TrimSpace(f.Target)
		default:
			return fmt.Errorf("filler %q: invalid source", f.Name)
		}
		if targets[key] {
			return fmt.Errorf("filler %q: duplicate image or Kodi path", f.Name)
		}
		targets[key] = true
		if err := f.Schedule.validate(); err != nil {
			return fmt.Errorf("filler %q: %w", f.Name, err)
		}
	}
	if !fallbackFound {
		return errors.New("standard filler does not exist")
	}
	return nil
}

func (s FillerSchedule) validate() error {
	switch s.Mode {
	case "always":
		return nil
	case "annual", "once":
		for _, date := range []string{s.Start, s.End} {
			value := date
			if s.Mode == "annual" {
				value = "2000-" + date // Leap year permits February 29.
			}
			parsed, err := time.Parse("2006-01-02", value)
			if err != nil || parsed.Format("2006-01-02") != value {
				return errors.New("a valid start and end date are required")
			}
		}
		if s.Mode == "once" && s.Start > s.End {
			return errors.New("end date must not precede start date")
		}
		return nil
	default:
		return errors.New("schedule must be always, annual, or once")
	}
}

func (s FillerSchedule) matches(now time.Time) bool {
	if s.Mode == "always" {
		return true
	}
	if s.validate() != nil {
		return false
	}
	date := now.Format("2006-01-02")
	if s.Mode == "annual" {
		date = now.Format("01-02")
		if s.Start > s.End {
			return date >= s.Start || date <= s.End
		}
	}
	return date >= s.Start && date <= s.End
}

func fillerTarget(cfg Config, f Filler) string {
	if f.Source == "uploaded-image" {
		if base := strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"); base != "" {
			return base + "/placeholder-image/" + f.Image
		}
	}
	return strings.TrimSpace(f.Target)
}

func chooseFiller(cfg Config, now time.Time, lastID string) (Filler, error) {
	if cfg.Fillers == nil {
		cfg.normalize()
	}
	var seasonal, always []Filler
	for _, f := range cfg.Fillers {
		if !f.Enabled || !f.Schedule.matches(now) {
			continue
		}
		if f.Schedule.Mode == "always" {
			always = append(always, f)
		} else {
			seasonal = append(seasonal, f)
		}
	}
	pool := seasonal
	if len(pool) == 0 {
		pool = always
	}
	if len(pool) == 0 {
		for _, f := range cfg.Fillers {
			if f.ID == cfg.FallbackFillerID {
				return f, nil
			}
		}
		return Filler{}, errors.New("no filler is available for today's date; enable a filler or select a standard filler")
	}
	if len(pool) > 1 {
		for i, f := range pool {
			if f.ID == lastID {
				pool = append(pool[:i], pool[i+1:]...)
				break
			}
		}
	}
	return pool[mathrand.IntN(len(pool))], nil
}

func (a *App) handlePlaceholderImageUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		http.Error(w, "image too large or invalid upload", 400)
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "missing image field", 400)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || len(data) == 0 {
		http.Error(w, "could not read image", 400)
		return
	}
	ext := imageExtension(http.DetectContentType(data))
	if ext == "" {
		http.Error(w, "only JPG, PNG, GIF, WebP, and BMP images are supported", 400)
		return
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	name := fmt.Sprintf("placeholder-image-%x%s", id, ext)
	if err := os.WriteFile(a.placeholderImagePath(name), data, 0600); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	f := Filler{ID: fmt.Sprintf("%x", id), Name: filepath.Base(header.Filename), Source: "uploaded-image", Image: name, Enabled: true, Schedule: FillerSchedule{Mode: "always"}}
	a.mu.Lock()
	a.cfg.migrateFillers()
	// Copy before appending: status and playback may be using a config snapshot.
	a.cfg.Fillers = append(append([]Filler{}, a.cfg.Fillers...), f)
	if a.cfg.FallbackFillerID == "" {
		a.cfg.FallbackFillerID = f.ID
	}
	if a.cfg.PublicBaseURL == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		a.cfg.PublicBaseURL = scheme + "://" + r.Host
	}
	a.mu.Unlock()
	if err := a.saveConfig(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"name": name, "url_path": "/placeholder-image/" + name, "filler": f})
}

func (a *App) handlePlaceholderImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "GET or HEAD required", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/placeholder-image/")
	if !fillerImageName.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	a.mu.RLock()
	configured := name == a.currentFillerImage || (a.cfg.Fillers == nil && name == a.cfg.PlaceholderImage)
	for _, f := range a.cfg.Fillers {
		configured = configured || (f.Source == "uploaded-image" && f.Image == name)
	}
	a.mu.RUnlock()
	if !configured {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, a.placeholderImagePath(name))
}

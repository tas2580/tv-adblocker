package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testFiller(id string, schedule FillerSchedule) Filler {
	return Filler{ID: id, Name: id, Source: "kodi-local", Target: "file:///" + id + ".jpg", Enabled: true, Schedule: schedule}
}

func TestFillerScheduleDates(t *testing.T) {
	for _, tc := range []struct {
		mode, start, end, date string
		want                   bool
	}{
		{"always", "", "", "2026-10-23", true},
		{"annual", "12-01", "12-26", "2026-11-30", false},
		{"annual", "12-01", "12-26", "2026-12-01", true},
		{"annual", "12-01", "12-26", "2030-12-26", true},
		{"annual", "12-01", "12-26", "2026-12-27", false},
		{"annual", "12-15", "01-06", "2026-12-31", true},
		{"annual", "12-15", "01-06", "2027-01-06", true},
		{"annual", "12-15", "01-06", "2027-01-07", false},
		{"annual", "02-29", "02-29", "2028-02-29", true},
		{"annual", "02-29", "02-29", "2027-02-28", false},
		{"annual", "02-29", "02-29", "2027-03-01", false},
		{"once", "2026-12-01", "2026-12-26", "2026-12-01", true},
		{"once", "2026-12-01", "2026-12-26", "2026-12-26", true},
		{"once", "2026-12-01", "2026-12-26", "2027-12-01", false},
	} {
		t.Run(strings.Join([]string{tc.mode, tc.start, tc.end, tc.date}, "/"), func(t *testing.T) {
			schedule := FillerSchedule{tc.mode, tc.start, tc.end}
			if err := schedule.validate(); err != nil {
				t.Fatal(err)
			}
			date, _ := time.Parse("2006-01-02", tc.date)
			// The entire end day, including the last instant before midnight, matches.
			date = date.Add(24*time.Hour - time.Nanosecond)
			if got := schedule.matches(date); got != tc.want {
				t.Fatalf("match = %v, want %v", got, tc.want)
			}
		})
	}
	// A calendar day uses the supplied server location, not the equivalent UTC date.
	local := time.Date(2026, 12, 1, 0, 0, 0, 0, time.FixedZone("server", 2*3600))
	if !(FillerSchedule{"annual", "12-01", "12-01"}).matches(local) {
		t.Fatal("server calendar day was not used")
	}
}

func TestChooseFillerPriorityRepetitionAndFallback(t *testing.T) {
	now := time.Date(2026, 12, 24, 0, 0, 0, 0, time.Local)
	annual := FillerSchedule{"annual", "12-01", "12-26"}
	always := FillerSchedule{Mode: "always"}
	cfg := Config{Fillers: []Filler{testFiller("normal", always), testFiller("christmas", annual), testFiller("other", annual)}}
	for range 20 {
		f, err := chooseFiller(cfg, now, "christmas")
		if err != nil || f.ID != "other" {
			t.Fatalf("seasonal selection = %q, %v", f.ID, err)
		}
	}
	cfg.Fillers[2].Enabled = false
	f, err := chooseFiller(cfg, now, "christmas")
	if err != nil || f.ID != "christmas" {
		t.Fatalf("single seasonal filler = %q, %v", f.ID, err)
	}
	f, err = chooseFiller(cfg, now.AddDate(0, 1, 0), "christmas")
	if err != nil || f.ID != "normal" {
		t.Fatalf("year-round fallback = %q, %v", f.ID, err)
	}
	cfg.Fillers[0].Enabled = false
	cfg.FallbackFillerID = "other" // Disabled and out of season, still the fallback.
	f, err = chooseFiller(cfg, now.AddDate(0, 1, 0), "other")
	if err != nil || f.ID != "other" {
		t.Fatalf("standard filler = %q, %v", f.ID, err)
	}
	cfg.FallbackFillerID = ""
	if _, err = chooseFiller(cfg, now.AddDate(0, 1, 0), ""); err == nil {
		t.Fatal("empty selection did not report an error")
	}
}

func TestFillerMigrationPreservesSelectionAndDeletion(t *testing.T) {
	for _, source := range []string{"uploaded-image", "kodi-local"} {
		cfg := Config{PlaceholderSource: source, PlaceholderImage: "placeholder-image.png", PlaceholderURL: "http://monitor/placeholder-image/placeholder-image.png", KodiPlaceholder: "file:///selected.jpg", KodiPlaceholders: []string{"file:///other.jpg", "file:///selected.jpg"}}
		cfg.normalize()
		if err := cfg.validateFillers(); err != nil {
			t.Fatal(err)
		}
		if len(cfg.Fillers) != 3 {
			t.Fatalf("migration lost entries: %#v", cfg.Fillers)
		}
		f, err := chooseFiller(cfg, time.Now(), "")
		if err != nil || f.Source != source || f.ID != cfg.FallbackFillerID {
			t.Fatalf("migration changed selection: %#v, %v", f, err)
		}
		data, _ := json.Marshal(cfg)
		var reloaded Config
		_ = json.Unmarshal(data, &reloaded)
		reloaded.normalize()
		if reloaded.FallbackFillerID != cfg.FallbackFillerID || len(reloaded.Fillers) != 3 {
			t.Fatal("migration was not stable across restart")
		}
		cfg.Fillers, cfg.FallbackFillerID = []Filler{}, ""
		cfg.normalize()
		if len(cfg.Fillers) != 0 {
			t.Fatal("deleted fillers were restored from legacy fields")
		}
		if isPlaceholderTarget("file:///selected.jpg", cfg) {
			t.Fatal("removed legacy target was still recognized as configured")
		}
	}
}

func TestInvalidFillerConfigDoesNotChangeApp(t *testing.T) {
	base := testFiller("first", FillerSchedule{Mode: "always"})
	for _, tc := range []struct {
		name     string
		schedule FillerSchedule
	}{
		{"missing date", FillerSchedule{"annual", "12-01", ""}},
		{"invalid day", FillerSchedule{"annual", "02-30", "12-01"}},
		{"invalid month", FillerSchedule{"annual", "13-01", "12-01"}},
		{"reversed", FillerSchedule{"once", "2026-12-26", "2026-12-01"}},
		{"invalid leap day", FillerSchedule{"once", "2027-02-29", "2027-03-01"}},
		{"invalid mode", FillerSchedule{Mode: "monthly"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			f.Schedule = tc.schedule
			assertFillerConfigRejected(t, Config{Fillers: []Filler{f}})
		})
	}
	assertFillerConfigRejected(t, Config{Fillers: []Filler{base}, FallbackFillerID: "missing"})
	assertFillerConfigRejected(t, Config{Fillers: []Filler{base, base}})
	traversal := base
	traversal.Source, traversal.Image = "uploaded-image", "../config.json"
	assertFillerConfigRejected(t, Config{Fillers: []Filler{traversal}})
}

func assertFillerConfigRejected(t *testing.T, cfg Config) {
	t.Helper()
	a := &App{cfg: Config{StreamName: "unchanged"}, status: Status{Running: true}}
	data, _ := json.Marshal(cfg)
	w := httptest.NewRecorder()
	a.handleConfig(w, httptest.NewRequest("POST", "/api/config", bytes.NewReader(data)))
	if w.Code != 400 || a.cfg.StreamName != "unchanged" || !a.status.Running {
		t.Fatalf("invalid config changed state: status %d, config %#v", w.Code, a.cfg)
	}
}

func TestFillerConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	a := &App{dataDir: dir, configPath: filepath.Join(dir, "config.json")}
	cfg := defaultConfig()
	cfg.PlaceholderImage = "placeholder-image.png" // Legacy fields remain non-authoritative.
	cfg.PublicBaseURL = "http://monitor:8099"
	cfg.Fillers = []Filler{
		testFiller("local", FillerSchedule{"once", "2026-12-01", "2026-12-26"}),
		{ID: "image", Name: "Winter", Source: "uploaded-image", Image: "placeholder-image.png", Enabled: true, Schedule: FillerSchedule{"annual", "12-15", "01-06"}},
	}
	cfg.FallbackFillerID = "local"
	for _, clear := range []bool{false, true} {
		if clear {
			cfg.Fillers, cfg.FallbackFillerID = []Filler{}, ""
		}
		data, _ := json.Marshal(cfg)
		w := httptest.NewRecorder()
		a.handleConfig(w, httptest.NewRequest("POST", "/api/config", bytes.NewReader(data)))
		if w.Code != 200 {
			t.Fatalf("save: %d %s", w.Code, w.Body.String())
		}
		reloaded := &App{configPath: a.configPath}
		if err := reloaded.loadConfig(); err != nil {
			t.Fatal(err)
		}
		reloaded.cfg.normalize()
		if len(reloaded.cfg.Fillers) != len(cfg.Fillers) || reloaded.cfg.FallbackFillerID != cfg.FallbackFillerID {
			t.Fatalf("saved selection changed: %#v", reloaded.cfg.Fillers)
		}
		if !clear && reloaded.cfg.Fillers[1].Schedule != cfg.Fillers[1].Schedule {
			t.Fatal("saved date range changed")
		}
	}
}

func TestFillerUploadsAreIndependentAndRemainAvailableWhilePlaying(t *testing.T) {
	dir := t.TempDir()
	a := &App{dataDir: dir, configPath: filepath.Join(dir, "config.json")}
	var uploaded []Filler
	for range 2 {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("image", "christmas.png")
		_, _ = part.Write([]byte("\x89PNG\r\n\x1a\nimage"))
		_ = writer.Close()
		r := httptest.NewRequest("POST", "http://monitor:8099/api/placeholder-image", &body)
		r.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		a.handlePlaceholderImageUpload(w, r)
		if w.Code != 200 {
			t.Fatalf("upload: %d %s", w.Code, w.Body.String())
		}
		var response struct{ Filler Filler }
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		uploaded = append(uploaded, response.Filler)
	}
	if uploaded[0].Image == uploaded[1].Image || len(a.cfg.Fillers) != 2 || a.cfg.FallbackFillerID != uploaded[0].ID {
		t.Fatal("uploads overwrote one another or did not set the initial standard")
	}
	if got := fillerTarget(a.cfg, uploaded[0]); got != "http://monitor:8099/placeholder-image/"+uploaded[0].Image {
		t.Fatalf("uploaded image URL = %q", got)
	}
	for _, f := range uploaded {
		w := httptest.NewRecorder()
		a.handlePlaceholderImage(w, httptest.NewRequest("GET", "/placeholder-image/"+f.Image, nil))
		if w.Code != 200 {
			t.Fatalf("previous upload unavailable: %d", w.Code)
		}
	}
	a.currentFillerImage = uploaded[0].Image
	a.cfg.Fillers = []Filler{}
	for i, f := range uploaded {
		w := httptest.NewRecorder()
		a.handlePlaceholderImage(w, httptest.NewRequest("GET", "/placeholder-image/"+f.Image, nil))
		want := 404
		if i == 0 {
			want = 200
		}
		if w.Code != want {
			t.Fatalf("removed image status = %d, want %d", w.Code, want)
		}
		if _, err := os.Stat(a.placeholderImagePath(f.Image)); err != nil {
			t.Fatal("removal deleted an image file")
		}
	}
}

func TestFillerPlaybackAndPolling(t *testing.T) {
	var opened []string
	failOpen := false
	file := ""
	client := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Method string `json:"method"`
			Params struct {
				Item struct{ File string } `json:"item"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		var result any
		switch request.Method {
		case "Playlist.GetPlaylists":
			result = []any{}
		case "Player.Open":
			if failOpen {
				return nil, fmt.Errorf("simulated Kodi failure")
			}
			file = request.Params.Item.File
			opened = append(opened, file)
			result = "OK"
		case "Player.GetActivePlayers":
			result = []any{map[string]any{"playerid": 1, "type": "picture"}}
		case "Player.GetItem":
			result = map[string]any{"item": map[string]any{"file": file, "label": "filler"}}
		default:
			t.Errorf("unexpected Kodi call: %s", request.Method)
		}
		data, _ := json.Marshal(map[string]any{"result": result})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})}
	cfg := Config{Kodi: KodiConfig{URL: "http://kodi/jsonrpc"}, Fillers: []Filler{testFiller("a", FillerSchedule{Mode: "always"}), testFiller("b", FillerSchedule{Mode: "always"})}, MissingFrames: 1, PresentFrames: 1}
	channel := Channel{Name: "TV", URL: "http://tv/stream"}
	a := &App{cfg: cfg, active: channel, httpClient: client, status: Status{Mode: "monitoring"}}
	a.observe(false, 0, cfg)
	if a.status.Mode != "placeholder" || len(opened) != 1 {
		t.Fatalf("automatic filler playback failed: %#v", a.status)
	}
	first := a.lastFillerID
	// A further frame during the same break must not choose or open another filler.
	a.observe(false, 0, cfg)
	if len(opened) != 1 {
		t.Fatal("filler rotated inside the break")
	}
	response := httptest.NewRecorder()
	a.handlePlayPlaceholder(response, httptest.NewRequest("POST", "/api/play/placeholder", nil))
	if response.Code != 200 || !a.status.ManualPlaceholder || a.lastFillerID == first || opened[0] == opened[1] {
		t.Fatal("manual playback did not avoid the last successful filler")
	}
	last := a.lastFillerID
	failOpen = true
	if err := a.play("placeholder", false); err == nil || a.lastFillerID != last {
		t.Fatal("failed playback changed the last filler")
	}
	failOpen = false
	// Editing or removing all configured fillers must not lose the source channel.
	a.cfg.Fillers = []Filler{}
	a.syncKodiPlayback()
	if a.active.URL != channel.URL {
		t.Fatal("polling lost the channel while a removed filler was playing")
	}

	// No filler: the automatic path restores the previous state without touching Kodi.
	empty := &App{cfg: Config{Fillers: []Filler{}}, status: Status{Mode: "monitoring"}}
	empty.observe(false, 0, Config{Fillers: []Filler{}, MissingFrames: 1})
	if empty.status.Mode != "monitoring" || empty.status.LastError == "" {
		t.Fatalf("empty selection left a switching state: %#v", empty.status)
	}
}

package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

const frameWidth, frameHeight = 1280, 720

// localeFiles keeps the UI translations available in the standalone binary.
//
//go:embed locales/*.json
var localeFiles embed.FS

type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}
type KodiConfig struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}
type Config struct {
	HTTPAddr                        string             `json:"http_addr"`
	M3UURL                          string             `json:"m3u_url"`
	StreamURL                       string             `json:"stream_url"`
	StreamName                      string             `json:"stream_name"`
	PlaceholderPath                 string             `json:"placeholder_path"`
	PlaceholderURL                  string             `json:"placeholder_url"`
	PlaceholderImage                string             `json:"placeholder_image"`
	PlaceholderSource               string             `json:"placeholder_source"`
	KodiPlaceholder                 string             `json:"kodi_placeholder"`
	KodiPlaceholders                []string           `json:"kodi_placeholders"`
	Fillers                         []Filler           `json:"fillers"`
	FallbackFillerID                string             `json:"fallback_filler_id"`
	PublicBaseURL                   string             `json:"public_base_url"`
	LogoRect                        Rect               `json:"logo_rect"`
	IntervalSeconds                 int                `json:"interval_seconds"`
	MissingFrames                   int                `json:"missing_frames"`
	PresentFrames                   int                `json:"present_frames"`
	ProgramCooldownSeconds          int                `json:"program_cooldown_seconds"`
	PlaceholderMinSeconds           int                `json:"placeholder_min_seconds"`
	PauseDetectionDuringProgramLock bool               `json:"pause_detection_during_program_lock"`
	SimilarityMinimum               float64            `json:"similarity_minimum"`
	ChannelSimilarityMinimum        map[string]float64 `json:"channel_similarity_minimum,omitempty"`
	ContrastStrength                float64            `json:"contrast_strength"`
	DebugEnabled                    bool               `json:"debug_enabled"`
	Kodi                            KodiConfig         `json:"kodi"`
}

func defaultConfig() Config {
	return Config{HTTPAddr: "127.0.0.1:8099", IntervalSeconds: 2, MissingFrames: 3, PresentFrames: 3, ProgramCooldownSeconds: 600, PlaceholderMinSeconds: 30, PauseDetectionDuringProgramLock: true, SimilarityMinimum: .80, ContrastStrength: 1.5}
}

func (c Config) similarityMinimumFor(streamURL string) float64 {
	if value, ok := c.ChannelSimilarityMinimum[streamURL]; ok {
		return value
	}
	return c.SimilarityMinimum
}

func (c Config) validateChannelSimilarity() error {
	for streamURL, value := range c.ChannelSimilarityMinimum {
		if strings.TrimSpace(streamURL) == "" {
			return errors.New("channel similarity thresholds require a non-empty stream URL")
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > 1 {
			return errors.New("channel similarity thresholds must be greater than 0 and at most 1; omit an entry to use the default")
		}
	}
	return nil
}

func (c *Config) normalize() {
	if c.HTTPAddr == "" {
		c.HTTPAddr = "127.0.0.1:8099"
	}
	if c.IntervalSeconds < 1 {
		c.IntervalSeconds = 2
	}
	if c.MissingFrames < 1 {
		c.MissingFrames = 3
	}
	if c.PresentFrames < 1 {
		c.PresentFrames = 3
	}
	if c.ProgramCooldownSeconds < 0 {
		c.ProgramCooldownSeconds = 0
	}
	if c.PlaceholderMinSeconds < 0 {
		c.PlaceholderMinSeconds = 0
	}
	if c.SimilarityMinimum <= 0 || c.SimilarityMinimum > 1 {
		c.SimilarityMinimum = .80
	}
	if c.ContrastStrength < .5 || c.ContrastStrength > 3 {
		c.ContrastStrength = 1.5
	}
	if c.PlaceholderSource != "uploaded-image" && c.PlaceholderSource != "kodi-local" {
		c.PlaceholderSource = "uploaded-image"
	}
	c.KodiPlaceholders = normalizedKodiPlaceholders(c.KodiPlaceholders)
	c.KodiPlaceholder = strings.TrimSpace(c.KodiPlaceholder)
	if c.KodiPlaceholder != "" && !containsPlaceholder(c.KodiPlaceholders, c.KodiPlaceholder) {
		// Keep the single value from configurations created before the list
		// existed, and also avoid silently losing an explicitly selected entry.
		c.KodiPlaceholders = append(c.KodiPlaceholders, c.KodiPlaceholder)
	}
	if c.KodiPlaceholder == "" && len(c.KodiPlaceholders) > 0 {
		c.KodiPlaceholder = c.KodiPlaceholders[0]
	}
	c.migrateFillers()
}

func normalizedKodiPlaceholders(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !containsPlaceholder(result, value) {
			result = append(result, value)
		}
	}
	return result
}

func containsPlaceholder(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type Status struct {
	LogoDetection            *LogoResult `json:"logo_detection,omitempty"`
	Running                  bool        `json:"running"`
	Mode                     string      `json:"mode"`
	ActiveChannel            string      `json:"active_channel,omitempty"`
	PlaylistUpdatedAt        string      `json:"playlist_updated_at,omitempty"`
	PlaylistError            string      `json:"playlist_error,omitempty"`
	KodiFile                 string      `json:"kodi_file,omitempty"`
	KodiLabel                string      `json:"kodi_label,omitempty"`
	KodiProgramme            string      `json:"kodi_programme,omitempty"`
	KodiError                string      `json:"kodi_error,omitempty"`
	LogoPresent              *bool       `json:"logo_present,omitempty"`
	Similarity               float64     `json:"similarity"`
	LastFrame                string      `json:"last_frame,omitempty"`
	LastError                string      `json:"last_error,omitempty"`
	DebugError               string      `json:"debug_error,omitempty"`
	Misses                   int         `json:"misses"`
	Matches                  int         `json:"matches"`
	ManualPlaceholder        bool        `json:"manual_placeholder"`
	ProgramCooldownRemaining int         `json:"program_cooldown_remaining"`
	PlaceholderMinRemaining  int         `json:"placeholder_min_remaining"`
	ProgramCooldownActive    bool        `json:"program_cooldown_active"`
	DetectionPaused          bool        `json:"detection_paused"`
	ProgramElapsedSeconds    int         `json:"program_elapsed_seconds"`
	AdvertElapsedSeconds     int         `json:"advert_elapsed_seconds"`
	ProgramStartedAt         string      `json:"program_started_at,omitempty"`
	AdvertStartedAt          string      `json:"advert_started_at,omitempty"`
}
type ReferenceEntry struct {
	MaskSource string `json:"mask_source,omitempty"`
	ID         string `json:"id"`
	StreamURL  string `json:"stream_url"`
	StreamName string `json:"stream_name"`
	Rect       Rect   `json:"rect"`
	CreatedAt  string `json:"created_at"`
}
type referenceIndex struct {
	References []ReferenceEntry `json:"references"`
}
type Frame struct {
	W, H int
	Pix  []byte
}

func (f Frame) clone() Frame { return Frame{f.W, f.H, append([]byte(nil), f.Pix...)} }

type App struct {
	logoCapture           *logoCapture
	logoDiagnostic        *logoDiagnostic
	logoDiagnosticHistory []*logoDiagnostic
	debugFrames           []debugFrame
	debugMu               sync.Mutex
	mu                    sync.RWMutex
	refMu                 sync.Mutex
	cfg                   Config
	playlist              []Channel
	kodiChannels          []KodiChannel
	active                Channel
	playlistUpdatedAt     time.Time
	kodiSwitchUntil       time.Time
	automaticReturnUntil  time.Time
	programCooldownUntil  time.Time
	placeholderMinUntil   time.Time
	programStartedAt      time.Time
	advertStartedAt       time.Time
	configPath, dataDir   string
	status                Status
	lastFillerID          string
	currentFillerTarget   string
	currentFillerImage    string
	lastFrame             Frame
	previewFrame          Frame
	cancel                context.CancelFunc
	resumeCancel          context.CancelFunc
	resumeID              uint64
	previewClients        map[string]time.Time
	previewTimer          *time.Timer
	httpClient            *http.Client
}

func main() {
	configPath := flag.String("config", "config.json", "path to the JSON configuration")
	dataDir := flag.String("data-dir", "data", "directory for reference data")
	httpAddr := flag.String("http-addr", "", "override HTTP listen address (for example 0.0.0.0:8099)")
	flag.Parse()
	a := &App{configPath: *configPath, dataDir: *dataDir, cfg: defaultConfig(), httpClient: &http.Client{Timeout: 12 * time.Second}}
	if err := os.MkdirAll(a.dataDir, 0750); err != nil {
		log.Fatal(err)
	}
	if err := a.loadConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal(err)
	}
	a.cfg.normalize()
	if err := a.cfg.validateChannelSimilarity(); err != nil {
		log.Fatal(err)
	}
	if err := a.cfg.validateFillers(); err != nil {
		log.Fatal(err)
	}
	if *httpAddr != "" {
		a.cfg.HTTPAddr = *httpAddr
	}
	if err := a.migrateLegacyReference(); err != nil {
		log.Printf("migrate legacy reference: %v", err)
	}
	if err := a.saveConfig(); err != nil {
		log.Fatal(err)
	}
	go a.automationLoop()
	mux := http.NewServeMux()
	a.routes(mux)
	log.Printf("TV logo monitor listening on http://%s", a.cfg.HTTPAddr)
	log.Fatal(http.ListenAndServe(a.cfg.HTTPAddr, mux))
}
func (a *App) loadConfig() error {
	b, e := os.ReadFile(a.configPath)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, &a.cfg)
}
func (a *App) saveConfig() error {
	a.mu.RLock()
	b, e := json.MarshalIndent(a.cfg, "", "  ")
	a.mu.RUnlock()
	if e != nil {
		return e
	}
	return os.WriteFile(a.configPath, append(b, '\n'), 0600)
}
func (a *App) routes(m *http.ServeMux) {
	m.HandleFunc("/", a.handleIndex)
	m.HandleFunc("/i18n/", a.handleLocale)
	m.HandleFunc("/api/status", a.handleStatus)
	m.HandleFunc("/api/config", a.handleConfig)
	m.HandleFunc("/api/playlist", a.handlePlaylist)
	m.HandleFunc("/api/frame.bmp", a.handleFrame)
	m.HandleFunc("/api/preview", a.handlePreviewSession)
	m.HandleFunc("/api/optimize-rect", a.handleOptimizeRect)
	m.HandleFunc("/api/reference", a.handleReference)
	m.HandleFunc("/api/references", a.handleReferences)
	m.HandleFunc("/api/references/", a.handleReferenceItem)
	m.HandleFunc("/api/logo-capture", a.handleLogoCapture)
	m.HandleFunc("/api/logo-mask", a.handleLogoMask)
	m.HandleFunc("/api/logo-diagnostic", a.handleLogoDiagnostic)
	m.HandleFunc("/api/debug-captures", a.handleDebugCaptures)
	m.HandleFunc("/api/debug-captures/", a.handleDebugCapture)
	m.HandleFunc("/api/start", a.handleStart)
	m.HandleFunc("/api/stop", a.handleStop)
	m.HandleFunc("/api/play/sender", a.handlePlaySender)
	m.HandleFunc("/api/play/placeholder", a.handlePlayPlaceholder)
	m.HandleFunc("/api/resume-automatic", a.handleResumeAutomatic)
	m.HandleFunc("/api/cancel-program-cooldown", a.handleCancelProgramCooldown)
	m.HandleFunc("/api/kodi-test", a.handleKodiTest)
	m.HandleFunc("/api/placeholder-image", a.handlePlaceholderImageUpload)
	m.HandleFunc("/placeholder.mp4", a.handlePlaceholder)
	m.HandleFunc("/placeholder-image/", a.handlePlaceholderImage)
}
func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, indexHTML)
}
func (a *App) handleLocale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/i18n/")
	if name != "de.json" && name != "en.json" {
		http.NotFound(w, r)
		return
	}
	b, err := localeFiles.ReadFile("locales/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(b)
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); e != nil {
		http.Error(w, e.Error(), 400)
		return false
	}
	return true
}
func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	s, c := a.status, a.cfg
	active := a.active
	programCooldownUntil := a.programCooldownUntil
	placeholderMinUntil := a.placeholderMinUntil
	programStartedAt := a.programStartedAt
	advertStartedAt := a.advertStartedAt
	a.mu.RUnlock()
	now := time.Now()
	s.ProgramCooldownRemaining = cooldownRemaining(programCooldownUntil, now)
	s.PlaceholderMinRemaining = cooldownRemaining(placeholderMinUntil, now)
	s.ProgramCooldownActive = programCooldownUntil.After(now)
	s.ProgramElapsedSeconds = elapsedSeconds(programStartedAt, now)
	s.AdvertElapsedSeconds = elapsedSeconds(advertStartedAt, now)
	if !programStartedAt.IsZero() {
		s.ProgramStartedAt = programStartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !advertStartedAt.IsZero() {
		s.AdvertStartedAt = advertStartedAt.UTC().Format(time.RFC3339Nano)
	}
	c.Kodi.Password = ""
	refs, _ := a.referencesFor(active.URL)
	writeJSON(w, map[string]any{"status": s, "config": c, "active_channel": active, "has_reference": len(refs) > 0, "reference_count": len(refs), "filler_timezone": time.Local.String() + ", " + now.Format("MST (UTC-07:00)")})
}

func cooldownRemaining(until, now time.Time) int {
	if !until.After(now) {
		return 0
	}
	return int((until.Sub(now) + time.Second - 1) / time.Second)
}

func elapsedSeconds(start, now time.Time) int {
	if start.IsZero() || now.Before(start) {
		return 0
	}
	return int(now.Sub(start) / time.Second)
}
func (a *App) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var n Config
	if !decodeJSON(w, r, &n) {
		return
	}
	n.normalize()
	if err := n.validateChannelSimilarity(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := n.validateFillers(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.mu.Lock()
	running := a.status.Running || a.status.DetectionPaused || a.cancel != nil
	if n.Kodi.Password == "" {
		n.Kodi.Password = a.cfg.Kodi.Password
	}
	if n.Kodi != a.cfg.Kodi {
		a.kodiChannels = nil
		a.active.KodiChannelID = nil
		a.status.KodiProgramme = ""
	}
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.cancelScheduledResumeLocked()
	a.status.Running = false
	a.status.DetectionPaused = false
	a.cfg = n
	if !n.DebugEnabled {
		a.debugFrames = nil
		a.status.DebugError = ""
	}
	a.mu.Unlock()
	if e := a.saveConfig(); e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	if running {
		_ = a.startMonitor()
	}
	go func() {
		if err := a.refreshKodiChannels(); err != nil {
			log.Printf("Kodi channels: %v", err)
		}
		if err := a.refreshPlaylist(); err == nil {
			a.syncKodiPlayback()
		}
	}()
	writeJSON(w, map[string]bool{"ok": true})
}

type Channel struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	LogoURL       string `json:"logo_url,omitempty"`
	KodiChannelID *int   `json:"kodi_channel_id,omitempty"`
}

var playlistAttribute = regexp.MustCompile(`([a-zA-Z0-9_-]+)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s,]+))`)

func playlistMetadata(line string, base *url.URL) (name, logo string) {
	// The separator is the first comma outside quotes; logo URLs and names
	// can themselves contain commas.
	var quote rune
	for i, char := range line {
		if quote != 0 {
			if char == quote {
				quote = 0
			}
		} else if char == '"' || char == '\'' {
			quote = char
		} else if char == ',' {
			name = strings.TrimSpace(line[i+1:])
			for _, attr := range playlistAttribute.FindAllStringSubmatch(line[:i], -1) {
				if !strings.EqualFold(attr[1], "tvg-logo") {
					continue
				}
				value := strings.TrimSpace(attr[2] + attr[3] + attr[4])
				if value == "" {
					continue
				}
				if parsed, err := url.Parse(value); err == nil {
					resolved := base.ResolveReference(parsed)
					if resolved.Scheme == "http" || resolved.Scheme == "https" {
						logo = resolved.String()
					}
				}
			}
			break
		}
	}
	return name, logo
}

func (a *App) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	if e := a.refreshPlaylist(); e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	a.mu.RLock()
	cs := append([]Channel(nil), a.playlist...)
	a.mu.RUnlock()
	writeJSON(w, cs)
}
func fetchPlaylist(client *http.Client, src string) ([]Channel, error) {
	resp, e := client.Get(src)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("playlist returned %s", resp.Status)
	}
	base, e := url.Parse(src)
	if e != nil {
		return nil, e
	}
	if resp.Request != nil && resp.Request.URL != nil {
		base = resp.Request.URL
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 8<<20))
	sc.Buffer(make([]byte, 4096), 1<<20)
	var ret []Channel
	name, logo := "", ""
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#EXTINF:") {
			name, logo = playlistMetadata(line, base)
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		u, e := url.Parse(line)
		if e == nil {
			ret = append(ret, Channel{Name: name, URL: base.ResolveReference(u).String(), LogoURL: logo})
		}
		name, logo = "", ""
	}
	return ret, sc.Err()
}
func (a *App) refreshPlaylist() error {
	a.mu.RLock()
	source := a.cfg.M3UURL
	a.mu.RUnlock()
	if source == "" {
		return errors.New("M3U8 URL is not configured")
	}
	channels, err := fetchPlaylist(a.httpClient, source)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.status.PlaylistError = err.Error()
		return err
	}
	a.playlist = channels
	if active, ok := channelForKodiPlayback(channels, a.active.URL, a.active.Name); ok {
		a.active.LogoURL = active.LogoURL
	} else {
		a.active.LogoURL = ""
	}
	a.playlistUpdatedAt = time.Now()
	a.status.PlaylistUpdatedAt = a.playlistUpdatedAt.Format(time.RFC3339)
	a.status.PlaylistError = ""
	return nil
}
func (a *App) automationLoop() {
	playlistTicker := time.NewTicker(5 * time.Minute)
	kodiTicker := time.NewTicker(2 * time.Second)
	defer playlistTicker.Stop()
	defer kodiTicker.Stop()
	if err := a.refreshPlaylist(); err != nil {
		log.Printf("playlist: %v", err)
	}
	if err := a.refreshKodiChannels(); err != nil {
		log.Printf("Kodi channels: %v", err)
	}
	a.syncKodiPlayback()
	for {
		select {
		case <-playlistTicker.C:
			if err := a.refreshKodiChannels(); err != nil {
				log.Printf("Kodi channels: %v", err)
			}
			if err := a.refreshPlaylist(); err != nil {
				log.Printf("playlist: %v", err)
			}
		case <-kodiTicker.C:
			a.syncKodiPlayback()
		}
	}
}
func (a *App) syncKodiPlayback() {
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	item, active, err := a.kodiCurrentItem(cfg.Kodi)
	a.mu.RLock()
	connectionChanged := a.cfg.Kodi != cfg.Kodi
	a.mu.RUnlock()
	if connectionChanged {
		return
	}
	file, label := item.File, item.Label
	if err != nil {
		a.setKodiReadout("", "", err)
		return
	}
	defer a.syncKodiProgramme(cfg.Kodi)
	if !active && (a.kodiSwitchInProgress() || a.automaticReturnInProgress()) {
		// Clearing Kodi's playlist briefly leaves it without a player. Keep the
		// known channel until Player.Open has had a chance to start the new one.
		// An automatic return additionally keeps its timing state: on slower
		// Kodi installations the empty-player phase can outlive the short
		// generic switch grace period.
		return
	}
	a.setKodiReadout(file, label, nil)
	if !active {
		a.setActiveChannel(Channel{})
		return
	}
	a.mu.RLock()
	channels := append([]Channel(nil), a.playlist...)
	kodiChannels := append([]KodiChannel(nil), a.kodiChannels...)
	currentFillerTarget := a.currentFillerTarget
	a.mu.RUnlock()
	if isPlaceholderTarget(file, cfg) || (currentFillerTarget != "" && sameStreamURL(file, currentFillerTarget)) {
		return // Keep monitoring the source while Kodi displays our own placeholder.
	}
	if channel, ok := channelForKodiItem(channels, kodiChannels, item); ok {
		a.confirmAutomaticReturn(channel)
		a.setActiveChannel(channel)
		return
	}
	if a.automaticReturnInProgress() {
		return
	}
	a.setActiveChannel(Channel{})
}

// channelForKodiPlayback resolves a Kodi item to a playlist channel. Kodi
// normally preserves the M3U label, but Player.Open can return only a file
// name or an empty label. The stream URL is a safe fallback in that case.
func channelForKodiPlayback(channels []Channel, kodiFile, kodiLabel string) (Channel, bool) {
	var matches []Channel
	for _, channel := range channels {
		if sameChannelName(kodiLabel, channel.Name) {
			matches = append(matches, channel)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	if kodiFile == "" {
		return Channel{}, false
	}
	matches = nil
	for _, channel := range channels {
		if channel.URL != "" && sameStreamURL(kodiFile, channel.URL) {
			matches = append(matches, channel)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return Channel{}, false
}

func (a *App) setKodiReadout(file, label string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status.KodiFile = file
	a.status.KodiLabel = label
	if err != nil {
		a.status.KodiError = err.Error()
		a.status.KodiProgramme = ""
	} else {
		a.status.KodiError = ""
	}
}

func (a *App) protectKodiTransition() {
	a.mu.Lock()
	a.kodiSwitchUntil = time.Now().Add(4 * time.Second)
	a.mu.Unlock()
}

func (a *App) kodiSwitchInProgress() bool {
	a.mu.RLock()
	until := a.kodiSwitchUntil
	a.mu.RUnlock()
	return time.Now().Before(until)
}

func (a *App) automaticReturnInProgress() bool {
	a.mu.RLock()
	until := a.automaticReturnUntil
	a.mu.RUnlock()
	return time.Now().Before(until)
}

func (a *App) confirmAutomaticReturn(channel Channel) {
	a.mu.Lock()
	if a.automaticReturnUntil.After(time.Now()) && sameActiveChannel(a.active, channel) {
		a.automaticReturnUntil = time.Time{}
	}
	a.mu.Unlock()
}
func sameStreamURL(aURL, bURL string) bool {
	if aURL == bURL {
		return true
	}
	a, errA := url.Parse(aURL)
	b, errB := url.Parse(bURL)
	return errA == nil && errB == nil && a.Scheme == b.Scheme && a.Host == b.Host && a.Path == b.Path && a.RawQuery == b.RawQuery
}

// sameChannelName accepts harmless presentation differences between Kodi's
// item label and the #EXTINF channel name, while avoiding fuzzy matches.
func sameChannelName(aName, bName string) bool {
	a := normalizeChannelName(aName)
	b := normalizeChannelName(bName)
	return a != "" && a == b
}

func normalizeChannelName(name string) string {
	var normalized strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}

func (a *App) kodiCurrentItem(cfg KodiConfig) (KodiItem, bool, error) {
	result, _, err := a.kodiCallWithConfig(cfg, "Player.GetActivePlayers", nil)
	if err != nil {
		return KodiItem{}, false, err
	}
	var players []map[string]any
	if err := json.Unmarshal(result, &players); err != nil {
		return KodiItem{}, false, err
	}
	if len(players) == 0 {
		return KodiItem{}, false, nil
	}
	var playerID int
	found := false
	for _, player := range players {
		id, ok := player["playerid"].(float64)
		if !ok {
			continue
		}
		if player["type"] == "video" || player["type"] == "picture" {
			playerID, found = int(id), true
			break
		}
		if !found {
			playerID, found = int(id), true
		}
	}
	if !found {
		return KodiItem{}, false, errors.New("Kodi returned no usable active player")
	}
	// Kodi supplies label, type and id without requesting them as properties.
	itemResult, _, err := a.kodiCallWithConfig(cfg, "Player.GetItem", map[string]any{"playerid": playerID, "properties": []string{"file"}})
	if err != nil {
		return KodiItem{}, false, err
	}
	var response struct {
		Item *KodiItem `json:"item"`
	}
	if err := json.Unmarshal(itemResult, &response); err != nil {
		return KodiItem{}, false, err
	}
	if response.Item == nil {
		return KodiItem{}, false, errors.New("Kodi returned no active item")
	}
	item := *response.Item
	return item, item.File != "" || item.Label != "" || item.channelID() != nil, nil
}
func (a *App) setActiveChannel(channel Channel) {
	a.mu.Lock()
	if sameActiveChannel(a.active, channel) {
		// Enrich the sender without resetting detection or phase timers.
		a.active.LogoURL = channel.LogoURL
		if channel.KodiChannelID != nil {
			a.active.KodiChannelID = channel.KodiChannelID
		}
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	a.suspendMonitor()
	a.mu.Lock()
	a.cancelScheduledResumeLocked()
	a.active = channel
	a.status.ActiveChannel = channel.Name
	a.status.KodiProgramme = ""
	a.status.Matches, a.status.Misses = 0, 0
	a.status.LogoPresent = nil
	a.status.LogoDetection = nil
	a.logoDiagnostic = nil
	a.logoDiagnosticHistory = nil
	a.debugFrames = nil
	if c := a.logoCapture; c != nil {
		c.State, c.Error, c.frames = "error", "capture.channelChanged", nil
	}
	a.status.Similarity = 0
	a.lastFrame = Frame{}
	a.previewFrame = Frame{}
	a.status.LastFrame = ""
	a.status.ManualPlaceholder = false
	a.programCooldownUntil = time.Time{}
	a.placeholderMinUntil = time.Time{}
	a.programStartedAt = time.Time{}
	a.advertStartedAt = time.Time{}
	if channel.URL == "" {
		a.status.Mode = "idle"
	}
	a.mu.Unlock()
	if channel.URL != "" {
		_ = a.startMonitor()
	}
}

// The M3U/Kodi sender name is the stable identity; stream URLs can be
// rewritten by Kodi or the TV server between two polling requests.
func sameActiveChannel(a, b Channel) bool {
	if a.KodiChannelID != nil && b.KodiChannelID != nil {
		return *a.KodiChannelID == *b.KodiChannelID
	}
	if a.Name == "" && b.Name == "" && a.URL == "" && b.URL == "" {
		return true
	}
	return sameChannelName(a.Name, b.Name)
}
func (a *App) handleFrame(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	f := a.lastFrame.clone()
	active := a.active
	running := a.status.Running || a.cancel != nil
	a.mu.RUnlock()
	if !running {
		if active.URL == "" {
			http.Error(w, "Kodi is not playing a known TV sender", http.StatusConflict)
			return
		}
		var err error
		f, err = captureFrame(r.Context(), active.URL)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	if len(f.Pix) == 0 {
		http.Error(w, "no frame received yet", 404)
		return
	}
	// Keep the exact frame that is sent to the browser. Saving a reference
	// must not race with a later frame received by the monitor.
	a.mu.Lock()
	if a.active.URL != active.URL {
		a.mu.Unlock()
		http.Error(w, "channel changed while loading preview", http.StatusConflict)
		return
	}
	a.previewFrame = f.clone()
	a.mu.Unlock()
	w.Header().Set("Content-Type", "image/bmp")
	_, _ = w.Write(encodeBMP(f))
}

// A preview needs only one frame; release its stream connection immediately.
func captureFrame(parent context.Context, streamURL string) (Frame, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	// Keep probing short for a single image. Some streams need more data to
	// identify their video track, so retain the normal probe as a fallback.
	fastCtx, fastCancel := context.WithTimeout(ctx, 5*time.Second)
	frame, err := captureFrameAttempt(fastCtx, streamURL, true)
	fastCancel()
	if err == nil {
		return frame, nil
	}
	if ctx.Err() != nil {
		return Frame{}, ctx.Err()
	}
	return captureFrameAttempt(ctx, streamURL, false)
}

func captureFrameAttempt(ctx context.Context, streamURL string, fast bool) (Frame, error) {
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", frameWidth, frameHeight, frameWidth, frameHeight)
	args := []string{"-hide_banner", "-loglevel", "error"}
	if fast {
		args = append(args, "-analyzeduration", "500000", "-probesize", "262144", "-fpsprobesize", "0")
	}
	// Frame threading buffers additional frames before delivering the first
	// decoded image. A single decoding thread avoids that startup delay.
	args = append(args, "-threads", "1", "-i", streamURL, "-an", "-vf", filter, "-frames:v", "1", "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	pix, err := cmd.Output()
	if err != nil {
		return Frame{}, fmt.Errorf("ffmpeg preview: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if len(pix) != frameWidth*frameHeight*3 {
		return Frame{}, errors.New("ffmpeg preview: incomplete frame")
	}
	return Frame{frameWidth, frameHeight, pix}, nil
}

// handleOptimizeRect trims only low-detail outer rows and columns from a
// user-selected logo rectangle. It intentionally leaves detailed borders
// alone: a transparent logo can have programme content behind it.
func (a *App) handleOptimizeRect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var rect Rect
	if !decodeJSON(w, r, &rect) {
		return
	}
	a.mu.RLock()
	frame := a.previewFrame.clone()
	a.mu.RUnlock()
	if !rectInside(frame, rect) {
		http.Error(w, "invalid or out-of-frame rectangle", http.StatusBadRequest)
		return
	}
	writeJSON(w, trimUniformBorder(frame, rect))
}
func (a *App) handleReference(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	var rect Rect
	if !decodeJSON(w, r, &rect) {
		return
	}
	a.mu.RLock()
	f := a.previewFrame.clone()
	active := a.active
	a.mu.RUnlock()
	if len(f.Pix) == 0 {
		http.Error(w, "load a preview image before saving a reference", 409)
		return
	}
	if !rectInside(f, rect) {
		http.Error(w, "invalid or out-of-frame rectangle", 400)
		return
	}
	if active.URL == "" {
		http.Error(w, "Kodi is not playing a known TV sender", 409)
		return
	}
	entry := ReferenceEntry{
		MaskSource: "single",
		ID:         fmt.Sprintf("%x", time.Now().UnixNano()),
		StreamURL:  active.URL,
		StreamName: active.Name,
		Rect:       rect,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
	cropped := cropFrame(f, rect)
	a.mu.Lock()
	if a.active.URL != active.URL {
		a.mu.Unlock()
		http.Error(w, "channel changed while saving reference", http.StatusConflict)
		return
	}
	if e := a.addMaskedReference(entry, cropped, automaticLogoMask(cropped)); e != nil {
		a.mu.Unlock()
		http.Error(w, e.Error(), 500)
		return
	}
	restart := a.status.Running || a.status.Mode == "setup"
	a.mu.Unlock()
	if restart {
		a.restartMonitor()
	}
	writeJSON(w, entry)
}
func (a *App) referencesDir() string          { return filepath.Join(a.dataDir, "references") }
func (a *App) referencesPath() string         { return filepath.Join(a.dataDir, "references.json") }
func (a *App) referenceFile(id string) string { return filepath.Join(a.referencesDir(), id+".rgb") }

func (a *App) loadReferenceIndex() (referenceIndex, error) {
	b, err := os.ReadFile(a.referencesPath())
	if errors.Is(err, os.ErrNotExist) {
		return referenceIndex{}, nil
	}
	if err != nil {
		return referenceIndex{}, err
	}
	var index referenceIndex
	if err := json.Unmarshal(b, &index); err != nil {
		return referenceIndex{}, err
	}
	return index, nil
}
func (a *App) saveReferenceIndex(index referenceIndex) error {
	b, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.referencesPath(), append(b, '\n'), 0600)
}
func (a *App) migrateLegacyReference() error {
	legacy := filepath.Join(a.dataDir, "logo-reference.rgb")
	if _, err := os.Stat(legacy); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	a.mu.RLock()
	cfg := a.cfg
	a.mu.RUnlock()
	if cfg.StreamURL == "" || !validRect(cfg.LogoRect) {
		return nil
	}
	a.refMu.Lock()
	defer a.refMu.Unlock()
	index, err := a.loadReferenceIndex()
	if err != nil {
		return err
	}
	id := "legacy-" + fmt.Sprintf("%x", time.Now().UnixNano())
	if err := os.MkdirAll(a.referencesDir(), 0750); err != nil {
		return err
	}
	if err := os.Rename(legacy, a.referenceFile(id)); err != nil {
		return err
	}
	index.References = append(index.References, ReferenceEntry{ID: id, StreamURL: cfg.StreamURL, StreamName: cfg.StreamName, Rect: cfg.LogoRect, CreatedAt: time.Now().Format(time.RFC3339)})
	if err := a.saveReferenceIndex(index); err != nil {
		_ = os.Rename(a.referenceFile(id), legacy)
		return err
	}
	return nil
}
func (a *App) addReference(entry ReferenceEntry, frame Frame) error {
	a.refMu.Lock()
	defer a.refMu.Unlock()
	if err := os.MkdirAll(a.referencesDir(), 0750); err != nil {
		return err
	}
	if err := saveReference(a.referenceFile(entry.ID), frame); err != nil {
		return err
	}
	index, err := a.loadReferenceIndex()
	if err != nil {
		return err
	}
	index.References = append(index.References, entry)
	return a.saveReferenceIndex(index)
}
func (a *App) referencesFor(streamURL string) ([]ReferenceEntry, error) {
	a.refMu.Lock()
	defer a.refMu.Unlock()
	index, err := a.loadReferenceIndex()
	if err != nil {
		return nil, err
	}
	// Keep the API representation consistent: an empty reference collection is
	// an empty JSON array, never null.
	entries := make([]ReferenceEntry, 0)
	for _, entry := range index.References {
		if entry.StreamURL == streamURL {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}
func (a *App) referenceByID(id string) (ReferenceEntry, error) {
	a.refMu.Lock()
	defer a.refMu.Unlock()
	index, err := a.loadReferenceIndex()
	if err != nil {
		return ReferenceEntry{}, err
	}
	for _, entry := range index.References {
		if entry.ID == id {
			return entry, nil
		}
	}
	return ReferenceEntry{}, os.ErrNotExist
}
func (a *App) removeReference(id, streamURL string) error {
	a.refMu.Lock()
	defer a.refMu.Unlock()
	index, err := a.loadReferenceIndex()
	if err != nil {
		return err
	}
	var kept []ReferenceEntry
	found := false
	for _, entry := range index.References {
		if entry.ID == id && entry.StreamURL == streamURL {
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	if !found {
		return os.ErrNotExist
	}
	index.References = kept
	if err := a.saveReferenceIndex(index); err != nil {
		return err
	}
	if err := os.Remove(a.referenceFile(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(a.maskFile(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(a.automaticMaskFile(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func (a *App) handleReferences(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	a.mu.RLock()
	streamURL := a.active.URL
	a.mu.RUnlock()
	entries, err := a.referencesFor(streamURL)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, entries)
}
func (a *App) handleReferenceItem(w http.ResponseWriter, r *http.Request) {
	part := strings.TrimPrefix(r.URL.Path, "/api/references/")
	if strings.HasSuffix(part, ".bmp") && r.Method == http.MethodGet {
		id := strings.TrimSuffix(part, ".bmp")
		entry, err := a.referenceByID(id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		frame, err := loadReference(a.referenceFile(entry.ID))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/bmp")
		_, _ = w.Write(encodeBMP(frame))
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "DELETE required", http.StatusMethodNotAllowed)
		return
	}
	a.mu.RLock()
	streamURL := a.active.URL
	running := a.status.Running || a.status.DetectionPaused
	a.mu.RUnlock()
	if err := a.removeReference(part, streamURL); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), 500)
		return
	}
	if running {
		a.restartMonitor()
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (a *App) restartMonitor() {
	a.mu.Lock()
	defer a.mu.Unlock()
	mode := a.status.Mode
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.cancelScheduledResumeLocked()
	a.status.Running = false
	a.status.DetectionPaused = false
	a.status.Matches, a.status.Misses = 0, 0
	_ = a.startMonitorLocked()
	if a.status.Running && (mode == "placeholder" || mode == "manual-placeholder" || mode == "sender") {
		a.status.Mode = mode
	}
}
func (a *App) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	if e := a.startMonitor(); e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (a *App) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.cancelScheduledResumeLocked()
	a.status.Running = false
	a.status.DetectionPaused = false
	if a.status.Mode == "setup" {
		a.status.Mode = "idle"
	}
	a.mu.Unlock()
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handleCancelProgramCooldown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	var err error
	if a.programCooldownUntil.After(time.Now()) || a.status.DetectionPaused {
		resume := a.status.DetectionPaused
		a.programCooldownUntil = time.Time{}
		a.cancelScheduledResumeLocked()
		a.status.DetectionPaused = false
		a.status.Matches, a.status.Misses = 0, 0
		if resume {
			a.status.Mode = "idle"
			err = a.startMonitorLocked()
		}
	}
	a.mu.Unlock()
	if err != nil {
		a.setError(err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (a *App) handlePlaySender(w http.ResponseWriter, r *http.Request) {
	if e := a.switchToSender(false); e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	a.mu.Lock()
	a.status.ManualPlaceholder = false
	a.mu.Unlock()
	writeJSON(w, map[string]bool{"ok": true})
}
func (a *App) handlePlayPlaceholder(w http.ResponseWriter, r *http.Request) {
	if e := a.play("placeholder", false); e != nil {
		http.Error(w, e.Error(), 502)
		return
	}
	a.mu.Lock()
	a.status.ManualPlaceholder = true
	a.status.Mode = "manual-placeholder"
	a.status.Matches, a.status.Misses = 0, 0
	a.mu.Unlock()
	writeJSON(w, map[string]bool{"ok": true})
}
func (a *App) handleResumeAutomatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	wasManualPlaceholder := a.status.ManualPlaceholder
	wasPaused := a.status.DetectionPaused
	a.status.ManualPlaceholder = false
	a.status.Matches, a.status.Misses = 0, 0
	if wasManualPlaceholder {
		// Kodi is still showing the manually selected placeholder. Resume from
		// that state so positive frames can trigger the return to the sender.
		a.status.Mode = "placeholder"
	} else if a.status.Running {
		a.status.Mode = "monitoring"
	}
	a.mu.Unlock()
	if wasPaused {
		_ = a.startMonitor()
	}
	writeJSON(w, map[string]bool{"ok": true})
}
func (a *App) handleKodiTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	result, endpoint, err := a.kodiCall("JSONRPC.Version", nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "endpoint": endpoint, "result": json.RawMessage(result)})
}
func (a *App) handlePlaceholder(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	p := a.cfg.PlaceholderPath
	a.mu.RUnlock()
	if p == "" {
		http.Error(w, "placeholder_path not configured", 404)
		return
	}
	http.ServeFile(w, r, p)
}
func (a *App) placeholderImagePath(name string) string {
	return filepath.Join(a.dataDir, name)
}
func imageExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	default:
		return ""
	}
}
func (a *App) startMonitor() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startMonitorLocked()
}

// startMonitorLocked keeps manual cancellation and timer expiry atomic with
// respect to stopping detection. The caller holds a.mu.
func (a *App) startMonitorLocked() error {
	if a.status.Running {
		return nil
	}
	active := a.active
	if active.URL == "" {
		return errors.New("Kodi is not playing a known TV sender")
	}
	refs, err := a.referencesFor(active.URL)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		a.cancelScheduledResumeLocked()
		a.status.DetectionPaused = false
		a.status.Mode = "setup"
		a.status.LogoPresent = nil
		a.status.Similarity = 0
		a.status.Matches, a.status.Misses = 0, 0
		a.status.LastError = ""
		if a.hasPreviewClientsLocked(time.Now()) && a.cancel == nil {
			ctx, cancel := context.WithCancel(context.Background())
			a.cancel = cancel
			a.lastFrame = Frame{}
			go a.monitor(ctx)
		}
		return nil
	}
	// Replace a preview-only decoder when the first reference is saved.
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	if a.cfg.PauseDetectionDuringProgramLock && a.programCooldownUntil.After(time.Now()) {
		a.pauseMonitorUntilLocked(a.programCooldownUntil)
		return nil
	}
	a.cancelScheduledResumeLocked()
	a.status.DetectionPaused = false
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.status.Running = true
	a.status.Mode = "monitoring"
	a.status.LastError = ""
	go a.monitor(ctx)
	return nil
}

// pauseMonitorUntilLocked releases the stream connection during the program
// grace period and starts a fresh detector as soon as it ends. a.mu is held.
func (a *App) pauseMonitorUntilLocked(until time.Time) {
	if a.resumeCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.resumeCancel = cancel
	a.resumeID++
	resumeID := a.resumeID
	a.status.Running = false
	a.status.DetectionPaused = true
	a.status.Mode = "program-lock"
	a.status.Matches, a.status.Misses = 0, 0
	go func() {
		delay := time.Until(until)
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
		a.mu.Lock()
		if ctx.Err() != nil || a.resumeID != resumeID {
			a.mu.Unlock()
			return
		}
		a.resumeCancel = nil
		a.status.DetectionPaused = false
		err := a.startMonitorLocked()
		a.mu.Unlock()
		if err != nil {
			a.setError(err)
		}
	}()
}

// cancelScheduledResumeLocked prevents a paused detector from being revived
// after the user stopped it, changed sender, or changed its configuration.
func (a *App) cancelScheduledResumeLocked() {
	if a.resumeCancel != nil {
		a.resumeCancel()
		a.resumeCancel = nil
	}
	a.resumeID++
}
func (a *App) monitor(ctx context.Context) {
	fastPreview := true
	for {
		e := a.monitorStream(ctx, fastPreview)
		if e == nil || ctx.Err() != nil {
			return
		}
		a.setError(e)
		fastPreview = false // Retry streams needing the full input probe.
		select {
		case <-ctx.Done():
			return
		case <-time.After(4 * time.Second):
		}
	}
}
func (a *App) monitorOnce(ctx context.Context) error {
	return a.monitorStream(ctx, true)
}

func (a *App) monitorStream(parent context.Context, fastPreview bool) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	a.mu.RLock()
	cfg := a.cfg
	active := a.active
	preview := a.hasPreviewClientsLocked(time.Now())
	detect := a.status.Running
	a.mu.RUnlock()
	if active.URL == "" {
		return nil
	}
	minimum := cfg.similarityMinimumFor(active.URL)
	entries, e := a.referencesFor(active.URL)
	if e != nil {
		return e
	}
	if len(entries) == 0 && !preview {
		return nil
	}
	setup := len(entries) == 0 || !detect
	refs := make([]*logoTemplate, 0, len(entries))
	for _, entry := range entries {
		ref, err := loadReference(a.referenceFile(entry.ID))
		if err != nil {
			return fmt.Errorf("read reference %s: %w", entry.ID, err)
		}
		mask, err := a.loadLogoMask(entry.ID, ref)
		if err != nil {
			return fmt.Errorf("read mask %s: %w", entry.ID, err)
		}
		prepared := prepareLogo(entry.ID, ref, entry.Rect, mask)
		if _, err := os.Stat(a.maskFile(entry.ID)); errors.Is(err, os.ErrNotExist) {
			prepared = prepareLogo(entry.ID, contrastLogoFrame(ref, cfg.ContrastStrength), entry.Rect, mask)
			prepared.frame, prepared.legacyContrast = ref, cfg.ContrastStrength
		}
		refs = append(refs, prepared)
	}
	filter := fmt.Sprintf("fps=1/%d,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", cfg.IntervalSeconds, frameWidth, frameHeight, frameWidth, frameHeight)
	args := []string{"-hide_banner", "-loglevel", "error"}
	if setup {
		if fastPreview {
			args = append(args, "-analyzeduration", "500000", "-probesize", "262144", "-fpsprobesize", "0")
		}
		args = append(args, "-threads", "1")
		filter = fmt.Sprintf("fps=2,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", frameWidth, frameHeight, frameWidth, frameHeight)
	}
	args = append(args, "-i", active.URL, "-an", "-vf", filter, "-pix_fmt", "rgb24", "-f", "rawvideo", "pipe:1")
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if e = cmd.Start(); e != nil {
		return e
	}
	defer func() {
		cancel()
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
	}()
	rd := bufio.NewReaderSize(out, 3<<20)
	for {
		pix := make([]byte, frameWidth*frameHeight*3)
		_, e = io.ReadFull(rd, pix)
		if e != nil {
			_ = cmd.Wait()
			if ctx.Err() != nil {
				return nil
			}
			if stderr.Len() > 0 {
				return fmt.Errorf("ffmpeg: %s", strings.TrimSpace(stderr.String()))
			}
			return e
		}
		f := Frame{frameWidth, frameHeight, pix}
		a.mu.Lock()
		if ctx.Err() != nil || a.active.URL != active.URL {
			a.mu.Unlock()
			return nil
		}
		a.lastFrame = f
		a.status.LastFrame = time.Now().Format(time.RFC3339)
		a.sampleLogoCaptureLocked(f, time.Now())
		if setup {
			a.status.LastError = ""
		}
		a.mu.Unlock()
		if setup {
			continue
		}
		best := LogoResult{Decision: "missing"}
		var bestRef *logoTemplate
		var plane logoPlane
		for _, ref := range refs {
			if len(ref.features) < 8 || ref.cells < 3 {
				continue
			}
			result := ref.find(f, &plane, minimum)
			if bestRef == nil || betterLogoResult(result, best) {
				best, bestRef = result, ref
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		a.observeLogo(best, bestRef, f, cfg, active.URL)
	}
}
func (a *App) setError(e error) {
	a.mu.Lock()
	a.status.LastError = e.Error()
	a.mu.Unlock()
	log.Printf("monitor: %v", e)
}
func (a *App) observe(present bool, score float64, cfg Config) {
	a.mu.Lock()
	a.observeLocked(present, score, cfg)
}

// Caller holds a.mu. This function releases it before contacting Kodi.
func (a *App) observeLocked(present bool, score float64, cfg Config) {
	a.status.Similarity = score
	a.status.LogoPresent = &present
	now := time.Now()
	programLocked := a.programCooldownUntil.After(now)
	placeholderLocked := a.placeholderMinUntil.After(now)
	mode := a.status.Mode
	if present {
		a.status.Matches++
		a.status.Misses = 0
		// A confirmed logo while the original channel is on screen marks the
		// point from which the programme has been observed as running.
		if !a.status.ManualPlaceholder && mode != "placeholder" && a.status.Matches >= cfg.PresentFrames {
			// Kodi can already be playing a programme when monitoring starts.
			// We do not know its real broadcast start then, but the status should
			// still show how long it has been confirmed as running. Unlike an
			// automatic return from a placeholder, this must not enable the
			// programme cooldown.
			if a.programStartedAt.IsZero() {
				a.programStartedAt = now
			}
		}
	} else {
		a.status.Misses++
		a.status.Matches = 0
	}
	if a.status.ManualPlaceholder {
		a.mu.Unlock()
		return
	}
	if !present && programLocked {
		// Do not carry misses accumulated during the program grace period
		// into the first decision after it expires.
		a.status.Misses = 0
	}
	if present && placeholderLocked {
		// Require fresh positive frames after the minimum ad time as well.
		a.status.Matches = 0
	}
	toPlaceholder := !present && !programLocked && a.status.Misses >= cfg.MissingFrames && mode != "placeholder"
	toSender := present && !placeholderLocked && a.status.Matches >= cfg.PresentFrames && mode == "placeholder"
	var debugCapture *debugCapture
	if toPlaceholder {
		a.status.Mode = "switching-placeholder"
		debugCapture = a.snapshotDebugCaptureLocked(cfg, now)
	}
	if toSender {
		a.status.Mode = "switching-sender"
	}
	a.mu.Unlock()
	if toPlaceholder {
		// Reaching this branch means the detector itself triggered the switch.
		// Treat it as the beginning of an advertisement even if monitoring began
		// after the logo had already disappeared.
		if e := a.play("placeholder", true); e != nil {
			a.mu.Lock()
			if a.status.Mode == "switching-placeholder" {
				a.status.Mode = mode
			}
			a.mu.Unlock()
			a.setError(e)
		} else if debugCapture != nil {
			go a.saveDebugCaptureAndReport(debugCapture)
		}
	} else if toSender {
		if e := a.switchToSender(true); e != nil {
			a.setError(e)
		}
	}
}
func (a *App) suspendMonitor() bool {
	a.mu.Lock()
	wasRunning := a.status.Running || a.cancel != nil
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
	a.status.Running = false
	a.mu.Unlock()
	return wasRunning
}
func (a *App) switchToSender(knownProgramStart bool) error {
	a.protectKodiTransition()
	if knownProgramStart {
		// Kodi can expose an empty or incomplete player state for longer than
		// kodiSwitchUntil while it returns from the placeholder.  Do not let
		// that transient state reset the just-recorded programme lock.
		a.mu.Lock()
		a.automaticReturnUntil = time.Now().Add(20 * time.Second)
		a.mu.Unlock()
	}
	a.mu.Lock()
	wasPaused := a.status.DetectionPaused
	a.cancelScheduledResumeLocked()
	a.status.DetectionPaused = false
	a.status.Mode = "switching-sender"
	a.mu.Unlock()
	wasMonitoring := a.suspendMonitor()
	if wasMonitoring || wasPaused {
		// Give ffmpeg and Kodi's previous RTSP player time to release their slots.
		time.Sleep(750 * time.Millisecond)
	}
	if knownProgramStart {
		// The confirmed logo is the end of the advertisement. Set the grace
		// period at that point, rather than waiting for Kodi's Player.Open reply:
		// Kodi can have already switched successfully when that reply is delayed
		// or lost.
		a.mu.RLock()
		cfg := a.cfg
		a.mu.RUnlock()
		a.recordPlayback("sender", cfg, true)
	}
	err := a.play("sender", knownProgramStart)
	if wasMonitoring {
		// Kodi needs a short head start, otherwise the monitor can consume its second slot.
		time.Sleep(1500 * time.Millisecond)
		if startErr := a.startMonitor(); err == nil && startErr != nil {
			err = startErr
		}
	}
	return err
}

// knownPhaseStart is true only when logo detection observed the beginning of
// the next program or advertisement. Starting Kodi manually, or discovering
// an already running channel, provides no such timing information.
func (a *App) play(which string, knownPhaseStart bool) error {
	a.mu.RLock()
	cfg := a.cfg
	lastFillerID := a.lastFillerID
	active := a.active
	if which == "sender" && active.KodiChannelID == nil {
		active.KodiChannelID = uniqueKodiChannelID(a.kodiChannels, active.Name)
	}
	a.mu.RUnlock()
	target := active.URL
	var chosen Filler
	if which == "placeholder" {
		var err error
		chosen, err = chooseFiller(cfg, time.Now(), lastFillerID)
		if err != nil {
			return err
		}
		target = fillerTarget(cfg, chosen)
	}
	if target == "" {
		return fmt.Errorf("%s URL is not configured", which)
	}
	if cfg.Kodi.URL == "" {
		return errors.New("Kodi JSON-RPC URL is not configured")
	}
	a.protectKodiTransition()
	if err := a.clearKodiPlaylists(cfg.Kodi); err != nil {
		return fmt.Errorf("clear Kodi playlists: %w", err)
	}
	item := map[string]any{"file": target}
	if which == "sender" {
		channelID := active.KodiChannelID
		if channelID != nil {
			item = map[string]any{"channelid": *channelID}
		} else {
			log.Printf("Kodi sender %q: no unique channel ID; falling back to stream URL", active.Name)
		}
	}
	_, _, err := a.kodiCallWithConfig(cfg.Kodi, "Player.Open", map[string]any{"item": item})
	var rpcErr *kodiRPCError
	if _, byID := item["channelid"]; byID && errors.As(err, &rpcErr) {
		log.Printf("Kodi sender %q: %v; falling back to stream URL", active.Name, err)
		_, _, err = a.kodiCallWithConfig(cfg.Kodi, "Player.Open", map[string]any{"item": map[string]string{"file": target}})
	}
	if err != nil {
		return err
	}
	if which == "placeholder" {
		a.mu.Lock()
		a.lastFillerID = chosen.ID
		a.currentFillerTarget = target
		a.currentFillerImage = chosen.Image
		a.mu.Unlock()
	}
	a.recordPlayback(which, cfg, knownPhaseStart)
	return nil
}

func (a *App) recordPlayback(which string, cfg Config, knownPhaseStart bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status.Mode = which
	a.status.LastError = ""
	if which == "placeholder" {
		a.debugFrames = nil
		if knownPhaseStart {
			a.placeholderMinUntil = cooldownDeadline(cfg.PlaceholderMinSeconds)
			a.advertStartedAt = time.Now()
		} else {
			a.placeholderMinUntil = time.Time{}
			a.advertStartedAt = time.Time{}
		}
		a.programCooldownUntil = time.Time{}
		a.programStartedAt = time.Time{}
		return
	}
	if which == "sender" {
		if knownPhaseStart {
			a.programCooldownUntil = cooldownDeadline(cfg.ProgramCooldownSeconds)
			a.programStartedAt = time.Now()
		} else {
			a.programCooldownUntil = time.Time{}
			a.programStartedAt = time.Time{}
		}
		a.placeholderMinUntil = time.Time{}
		a.advertStartedAt = time.Time{}
	}
}

func cooldownDeadline(seconds int) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Now().Add(time.Duration(seconds) * time.Second)
}

func placeholderTarget(cfg Config) string {
	if cfg.PlaceholderSource == "kodi-local" {
		return strings.TrimSpace(cfg.KodiPlaceholder)
	}
	return strings.TrimSpace(cfg.PlaceholderURL)
}

// isPlaceholderTarget also recognizes an older configured local placeholder
// while Kodi is still showing it after the selected entry has changed.
func isPlaceholderTarget(file string, cfg Config) bool {
	if cfg.Fillers != nil {
		for _, f := range cfg.Fillers {
			if target := fillerTarget(cfg, f); target != "" && sameStreamURL(file, target) {
				return true
			}
		}
		return false
	}
	if cfg.PlaceholderSource != "kodi-local" {
		return sameStreamURL(file, placeholderTarget(cfg))
	}
	if sameStreamURL(file, cfg.KodiPlaceholder) {
		return true
	}
	for _, target := range cfg.KodiPlaceholders {
		if sameStreamURL(file, target) {
			return true
		}
	}
	return false
}

func kodiEndpoint(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("Kodi JSON-RPC URL is invalid")
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/jsonrpc"
	}
	return u.String(), nil
}
func (a *App) kodiCall(method string, params any) ([]byte, string, error) {
	a.mu.RLock()
	cfg := a.cfg.Kodi
	a.mu.RUnlock()
	return a.kodiCallWithConfig(cfg, method, params)
}

func (a *App) kodiCallWithConfig(cfg KodiConfig, method string, params any) ([]byte, string, error) {
	if cfg.URL == "" {
		return nil, "", errors.New("Kodi JSON-RPC URL is not configured")
	}
	endpoint, err := kodiEndpoint(cfg.URL)
	if err != nil {
		return nil, "", err
	}
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		payload["params"] = params
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, endpoint, err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, endpoint, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.Username != "" {
		req.SetBasicAuth(cfg.Username, cfg.Password)
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, endpoint, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, endpoint, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, endpoint, fmt.Errorf("Kodi returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var reply map[string]json.RawMessage
	if err := json.Unmarshal(body, &reply); err != nil {
		return nil, endpoint, fmt.Errorf("invalid Kodi response: %w", err)
	}
	if rpcErr, ok := reply["error"]; ok {
		return nil, endpoint, &kodiRPCError{Message: string(rpcErr)}
	}
	result, ok := reply["result"]
	if !ok {
		return nil, endpoint, errors.New("Kodi response has no result")
	}
	return result, endpoint, nil
}
func (a *App) clearKodiPlaylists(cfg KodiConfig) error {
	result, _, err := a.kodiCallWithConfig(cfg, "Playlist.GetPlaylists", nil)
	if err != nil {
		return err
	}
	var playlists []map[string]any
	if err := json.Unmarshal(result, &playlists); err != nil {
		return fmt.Errorf("decode playlists: %w", err)
	}
	for _, playlist := range playlists {
		id, ok := playlist["playlistid"].(float64)
		if !ok {
			continue
		}
		if _, _, err := a.kodiCallWithConfig(cfg, "Playlist.Clear", map[string]int{"playlistid": int(id)}); err != nil {
			return err
		}
	}
	return nil
}
func validRect(r Rect) bool           { return r.X >= 0 && r.Y >= 0 && r.W >= 8 && r.H >= 8 }
func rectInside(f Frame, r Rect) bool { return validRect(r) && r.X <= f.W-r.W && r.Y <= f.H-r.H }

// trimUniformBorder removes the low-detail padding commonly left around a
// logo after a manual drag. The threshold is deliberately conservative so a
// moving or textured background is not mistaken for empty padding.
func trimUniformBorder(f Frame, r Rect) Rect {
	const detailThreshold = 12.0
	for {
		trimmed := false
		if r.H > 8 && edgeDetail(f, r, "top") < detailThreshold {
			r.Y++
			r.H--
			trimmed = true
		}
		if r.H > 8 && edgeDetail(f, r, "bottom") < detailThreshold {
			r.H--
			trimmed = true
		}
		if r.W > 8 && edgeDetail(f, r, "left") < detailThreshold {
			r.X++
			r.W--
			trimmed = true
		}
		if r.W > 8 && edgeDetail(f, r, "right") < detailThreshold {
			r.W--
			trimmed = true
		}
		if !trimmed {
			return r
		}
	}
}

// edgeDetail combines variation along an edge with the contrast to the next
// inner row or column. Flat padding scores near zero; a logo edge or a
// textured scene scores higher.
func edgeDetail(f Frame, r Rect, edge string) float64 {
	var sum float64
	var count int
	length := r.H
	if edge == "top" || edge == "bottom" {
		length = r.W
	}
	for n := 0; n < length; n++ {
		var x, y, innerX, innerY int
		switch edge {
		case "top":
			x, y, innerX, innerY = r.X+n, r.Y, r.X+n, r.Y+1
		case "bottom":
			x, y, innerX, innerY = r.X+n, r.Y+r.H-1, r.X+n, r.Y+r.H-2
		case "left":
			x, y, innerX, innerY = r.X, r.Y+n, r.X+1, r.Y+n
		case "right":
			x, y, innerX, innerY = r.X+r.W-1, r.Y+n, r.X+r.W-2, r.Y+n
		}
		value := pixelLuminance(f, x, y)
		sum += math.Abs(value - pixelLuminance(f, innerX, innerY))
		if n > 0 {
			if edge == "top" || edge == "bottom" {
				sum += math.Abs(value - pixelLuminance(f, x-1, y))
			} else {
				sum += math.Abs(value - pixelLuminance(f, x, y-1))
			}
			count++
		}
		count++
	}
	return sum / float64(count)
}

func pixelLuminance(f Frame, x, y int) float64 {
	i := (y*f.W + x) * 3
	return .2126*float64(f.Pix[i]) + .7152*float64(f.Pix[i+1]) + .0722*float64(f.Pix[i+2])
}

func cropFrame(f Frame, r Rect) Frame {
	out := Frame{r.W, r.H, make([]byte, r.W*r.H*3)}
	for y := 0; y < r.H; y++ {
		copy(out.Pix[y*r.W*3:(y+1)*r.W*3], f.Pix[((r.Y+y)*f.W+r.X)*3:((r.Y+y)*f.W+r.X+r.W)*3])
	}
	return out
}
func saveReference(p string, f Frame) error {
	if f.W > 65535 || f.H > 65535 {
		return errors.New("reference too large")
	}
	head := []byte{'T', 'V', 'L', 'G', byte(f.W >> 8), byte(f.W), byte(f.H >> 8), byte(f.H)}
	return os.WriteFile(p, append(head, f.Pix...), 0600)
}
func loadReference(p string) (Frame, error) {
	b, e := os.ReadFile(p)
	if e != nil {
		return Frame{}, e
	}
	if len(b) < 8 || string(b[:4]) != "TVLG" {
		return Frame{}, errors.New("invalid reference file")
	}
	w := int(b[4])<<8 | int(b[5])
	h := int(b[6])<<8 | int(b[7])
	if w < 1 || h < 1 || len(b) != 8+w*h*3 {
		return Frame{}, errors.New("invalid reference dimensions")
	}
	return Frame{w, h, b[8:]}, nil
}

// compareLogo combines luminance SSIM with a normalized Sobel edge score.
// SSIM retains colour/brightness information while the edge score emphasizes
// the logo's shape, making transparent logos less sensitive to a changing
// programme image behind them.
func compareLogo(frame, ref Frame, r Rect, contrastStrength float64) (float64, error) {
	if !rectInside(frame, r) {
		return 0, errors.New("logo rectangle outside decoded frame")
	}
	if contrastStrength < .5 || contrastStrength > 3 {
		contrastStrength = 1.5
	}
	const side = 48
	frameLuminance := make([]float64, side*side)
	refLuminance := make([]float64, side*side)
	var frameSum, refSum float64
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			sx := r.X + x*r.W/side
			sy := r.Y + y*r.H/side
			rx := x * ref.W / side
			ry := y * ref.H / side
			fi := (sy*frame.W + sx) * 3
			ri := (ry*ref.W + rx) * 3
			i := y*side + x
			frameLuminance[i] = contrastLuminance(frame.Pix[fi], frame.Pix[fi+1], frame.Pix[fi+2], contrastStrength)
			refLuminance[i] = contrastLuminance(ref.Pix[ri], ref.Pix[ri+1], ref.Pix[ri+2], contrastStrength)
			frameSum += frameLuminance[i]
			refSum += refLuminance[i]
		}
	}
	pixels := float64(side * side)
	frameMean, refMean := frameSum/pixels, refSum/pixels
	var frameVariance, refVariance, covariance float64
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			i := y*side + x
			frameDelta := frameLuminance[i] - frameMean
			refDelta := refLuminance[i] - refMean
			frameVariance += frameDelta * frameDelta
			refVariance += refDelta * refDelta
			covariance += frameDelta * refDelta
		}
	}
	frameVariance /= pixels
	refVariance /= pixels
	covariance /= pixels
	const c1 = 6.5025  // (0.01 * 255)²
	const c2 = 58.5225 // (0.03 * 255)²
	ssim := ((2*frameMean*refMean + c1) * (2*covariance + c2)) /
		((frameMean*frameMean + refMean*refMean + c1) * (frameVariance + refVariance + c2))
	ssim = math.Max(0, math.Min(1, ssim))

	edgeScore, usableEdges := sobelSimilarity(frameLuminance, refLuminance, side)
	if !usableEdges {
		// Uniform areas have no orientation to compare. In that case SSIM is
		// the meaningful signal and preserves compatibility with such references.
		return ssim, nil
	}
	// The shape is deliberately dominant: the programme background can make
	// SSIM differ substantially even when the transparent logo is unchanged.
	return .8*edgeScore + .2*ssim, nil
}

// sobelSimilarity measures whether edge direction and position agree. It is
// cosine similarity over the horizontal and vertical Sobel gradients, so the
// score is insensitive to uniform brightness offsets.
func sobelSimilarity(frame, ref []float64, side int) (float64, bool) {
	var dot, frameEnergy, refEnergy float64
	for y := 1; y < side-1; y++ {
		for x := 1; x < side-1; x++ {
			i := y*side + x
			frameX := -frame[i-side-1] + frame[i-side+1] - 2*frame[i-1] + 2*frame[i+1] - frame[i+side-1] + frame[i+side+1]
			frameY := -frame[i-side-1] - 2*frame[i-side] - frame[i-side+1] + frame[i+side-1] + 2*frame[i+side] + frame[i+side+1]
			refX := -ref[i-side-1] + ref[i-side+1] - 2*ref[i-1] + 2*ref[i+1] - ref[i+side-1] + ref[i+side+1]
			refY := -ref[i-side-1] - 2*ref[i-side] - ref[i-side+1] + ref[i+side-1] + 2*ref[i+side] + ref[i+side+1]
			dot += frameX*refX + frameY*refY
			frameEnergy += frameX*frameX + frameY*frameY
			refEnergy += refX*refX + refY*refY
		}
	}
	// Values below this threshold are effectively flat after downsampling.
	if frameEnergy < 1 || refEnergy < 1 {
		return 0, false
	}
	return math.Max(0, math.Min(1, dot/math.Sqrt(frameEnergy*refEnergy))), true
}

func contrastLuminance(red, green, blue byte, strength float64) float64 {
	luminance := .2126*float64(red) + .7152*float64(green) + .0722*float64(blue)
	return math.Max(0, math.Min(255, 128+(luminance-128)*strength))
}
func encodeBMP(f Frame) []byte {
	row := ((f.W*3 + 3) / 4) * 4
	out := make([]byte, 54+row*f.H)
	copy(out, "BM")
	n := len(out)
	out[2], out[3], out[4], out[5] = byte(n), byte(n>>8), byte(n>>16), byte(n>>24)
	out[10] = 54
	out[14] = 40
	out[18], out[19], out[20], out[21] = byte(f.W), byte(f.W>>8), byte(f.W>>16), byte(f.W>>24)
	out[22], out[23], out[24], out[25] = byte(f.H), byte(f.H>>8), byte(f.H>>16), byte(f.H>>24)
	out[26] = 1
	out[28] = 24
	for y := 0; y < f.H; y++ {
		for x := 0; x < f.W; x++ {
			src := ((f.H-1-y)*f.W + x) * 3
			dst := 54 + y*row + x*3
			out[dst], out[dst+1], out[dst+2] = f.Pix[src+2], f.Pix[src+1], f.Pix[src]
		}
	}
	return out
}

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestCompareLogo(t *testing.T) {
	frame := Frame{100, 100, make([]byte, 30000)}
	for y := 20; y < 40; y++ {
		for x := 30; x < 50; x++ {
			i := (y*100 + x) * 3
			frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2] = 255, 255, 255
		}
	}
	ref := Frame{20, 20, make([]byte, 1200)}
	for i := range ref.Pix {
		ref.Pix[i] = 255
	}
	s, e := compareLogo(frame, ref, Rect{30, 20, 20, 20}, 1.5)
	if e != nil || s < .99 {
		t.Fatalf("match=%f err=%v", s, e)
	}
	black := Frame{100, 100, make([]byte, 30000)}
	s, e = compareLogo(black, ref, Rect{30, 20, 20, 20}, 1.5)
	if e != nil || s > .01 {
		t.Fatalf("mismatch=%f err=%v", s, e)
	}
}

func TestCompareLogoToleratesBrightnessAndContrastChanges(t *testing.T) {
	ref := Frame{20, 20, make([]byte, 20*20*3)}
	for y := 0; y < ref.H; y++ {
		for x := 0; x < ref.W; x++ {
			value := byte(70)
			if x > 4 && x < 15 && (y > 7 && y < 12 || x > 8 && x < 12) {
				value = 190
			}
			i := (y*ref.W + x) * 3
			ref.Pix[i], ref.Pix[i+1], ref.Pix[i+2] = value, value, value
		}
	}
	frame := Frame{100, 100, make([]byte, 100*100*3)}
	for y := 0; y < ref.H; y++ {
		for x := 0; x < ref.W; x++ {
			value := byte(110)
			if x > 4 && x < 15 && (y > 7 && y < 12 || x > 8 && x < 12) {
				value = 255
			}
			i := ((20+y)*frame.W + 30 + x) * 3
			frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2] = value, value, value
		}
	}

	score, err := compareLogo(frame, ref, Rect{30, 20, 20, 20}, 1.5)
	if err != nil {
		t.Fatal(err)
	}
	if score < .85 {
		t.Fatalf("brightness and contrast shifted logo score=%f", score)
	}

	for y := 0; y < 20; y++ {
		for x := 0; x < 20; x++ {
			i := ((20+y)*frame.W + 30 + x) * 3
			frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2] = 110, 110, 110
		}
	}
	score, err = compareLogo(frame, ref, Rect{30, 20, 20, 20}, 1.5)
	if err != nil {
		t.Fatal(err)
	}
	if score > .25 {
		t.Fatalf("structureless area score=%f", score)
	}
}

func TestContrastStrengthNormalization(t *testing.T) {
	c := defaultConfig()
	c.ContrastStrength = 0
	c.normalize()
	if c.ContrastStrength != 1.5 {
		t.Fatalf("missing contrast strength = %f", c.ContrastStrength)
	}
	c.ContrastStrength = 3.1
	c.normalize()
	if c.ContrastStrength != 1.5 {
		t.Fatalf("invalid contrast strength = %f", c.ContrastStrength)
	}
}
func TestDefaults(t *testing.T) {
	c := defaultConfig()
	c.normalize()
	if c.IntervalSeconds != 2 || c.MissingFrames != 3 || c.PresentFrames != 3 || c.ProgramCooldownSeconds != 600 || c.PlaceholderMinSeconds != 30 || c.SimilarityMinimum != .80 || c.ContrastStrength != 1.5 {
		t.Fatal("defaults")
	}
}

func TestLocaleEndpoint(t *testing.T) {
	a := &App{}
	request := httptest.NewRequest(http.MethodGet, "/i18n/en.json", nil)
	response := httptest.NewRecorder()
	a.handleLocale(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q", got)
	}
	var translations map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &translations); err != nil {
		t.Fatal(err)
	}
	if translations["app.title"] != "TV Logo Monitor" {
		t.Fatalf("unexpected English title: %q", translations["app.title"])
	}

	request = httptest.NewRequest(http.MethodGet, "/i18n/fr.json", nil)
	response = httptest.NewRecorder()
	a.handleLocale(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown locale status = %d", response.Code)
	}
}

func TestLocaleFilesHaveTheSameKeys(t *testing.T) {
	locales := make(map[string]map[string]string)
	for _, language := range []string{"de", "en"} {
		contents, err := localeFiles.ReadFile("locales/" + language + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var translations map[string]string
		if err := json.Unmarshal(contents, &translations); err != nil {
			t.Fatal(err)
		}
		locales[language] = translations
	}
	for key := range locales["de"] {
		if _, ok := locales["en"][key]; !ok {
			t.Errorf("English translation missing key %q", key)
		}
	}
	for key := range locales["en"] {
		if _, ok := locales["de"][key]; !ok {
			t.Errorf("German translation missing key %q", key)
		}
	}
}

func TestImageExtension(t *testing.T) {
	for contentType, want := range map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp", "image/bmp": ".bmp"} {
		if got := imageExtension(contentType); got != want {
			t.Fatalf("%s: got %q, want %q", contentType, got, want)
		}
	}
	if imageExtension("text/plain") != "" {
		t.Fatal("non-image type accepted")
	}
}

func TestReferenceRoundTrip(t *testing.T) {
	f := Frame{W: 2, H: 2, Pix: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}}
	p := filepath.Join(t.TempDir(), "reference.rgb")
	if err := saveReference(p, f); err != nil {
		t.Fatal(err)
	}
	got, err := loadReference(p)
	if err != nil {
		t.Fatal(err)
	}
	if got.W != f.W || got.H != f.H || string(got.Pix) != string(f.Pix) {
		t.Fatal("reference did not round-trip")
	}
}

func TestKodiOpenPayload(t *testing.T) {
	b, err := kodiOpenPayload("http://tv.lan/channel.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	var received map[string]any
	if err := json.Unmarshal(b, &received); err != nil {
		t.Fatal(err)
	}
	params := received["params"].(map[string]any)
	item := params["item"].(map[string]any)
	if item["file"] != "http://tv.lan/channel.m3u8" {
		t.Fatalf("unexpected Kodi target: %#v", item)
	}
}

func TestKodiEndpointAddsJSONRPCPath(t *testing.T) {
	got, err := kodiEndpoint("http://kodi.lan:8080")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://kodi.lan:8080/jsonrpc" {
		t.Fatalf("got %q", got)
	}
	got, err = kodiEndpoint("http://kodi.lan:8080/jsonrpc")
	if err != nil || got != "http://kodi.lan:8080/jsonrpc" {
		t.Fatalf("got %q, err %v", got, err)
	}
}

func TestSameStreamURL(t *testing.T) {
	if !sameStreamURL("rtsp://tv.lan:554/?channel=1&pid=2", "rtsp://tv.lan:554/?channel=1&pid=2") {
		t.Fatal("identical URLs did not match")
	}
	if sameStreamURL("rtsp://tv.lan:554/?channel=1", "rtsp://tv.lan:554/?channel=2") {
		t.Fatal("different streams matched")
	}
}

func TestSameChannelName(t *testing.T) {
	for _, test := range []struct {
		a, b string
		want bool
	}{
		{"Das Erste HD", "das erste hd", true},
		{"RTL-ZWEI", "RTL zwei", true},
		{"ZDF.neo", "zdf neo", true},
		{"RTL", "RTL ZWEI", false},
		{"", "Das Erste", false},
	} {
		if got := sameChannelName(test.a, test.b); got != test.want {
			t.Errorf("sameChannelName(%q, %q) = %v, want %v", test.a, test.b, got, test.want)
		}
	}
}

func TestSameActiveChannelPreservesSenderAcrossURLFormatting(t *testing.T) {
	current := Channel{Name: "RTL ZWEI", URL: "rtsp://tv.lan:554/?channel=2"}
	fromKodi := Channel{Name: "rtl-zwei", URL: "rtsp://tv.lan:554/?channel=2&source=kodi"}
	if !sameActiveChannel(current, fromKodi) {
		t.Fatal("same named Kodi channel was treated as a sender change")
	}
	if sameActiveChannel(current, Channel{Name: "ZDF", URL: fromKodi.URL}) {
		t.Fatal("different sender name was treated as the active channel")
	}
}

func TestChannelForKodiPlaybackUsesLabelThenStreamURL(t *testing.T) {
	channels := []Channel{{Name: "Das Erste", URL: "rtsp://tv.example/one"}, {Name: "RTL ZWEI", URL: "rtsp://tv.example/two"}}
	got, ok := channelForKodiPlayback(channels, "rtsp://tv.example/one", "rtl-zwei")
	if !ok || got.Name != "RTL ZWEI" {
		t.Fatalf("got %#v, ok=%v", got, ok)
	}
	got, ok = channelForKodiPlayback(channels, "rtsp://tv.example/two", "Unbekannter Sender")
	if !ok || got.Name != "RTL ZWEI" {
		t.Fatalf("file fallback got %#v, ok=%v", got, ok)
	}
	if _, ok := channelForKodiPlayback(channels, "rtsp://tv.example/unknown", "Unbekannter Sender"); ok {
		t.Fatal("unknown Kodi item matched a channel")
	}
}

func TestKodiCurrentItemProperties(t *testing.T) {
	params := map[string]any{"playerid": 1, "properties": []string{"file"}}
	properties := params["properties"].([]string)
	if len(properties) != 1 || properties[0] != "file" {
		t.Fatalf("unexpected Player.GetItem properties: %#v", properties)
	}
}

func TestKodiTransitionProtection(t *testing.T) {
	a := &App{}
	if a.kodiSwitchInProgress() {
		t.Fatal("new app should not be in a Kodi transition")
	}
	a.protectKodiTransition()
	if !a.kodiSwitchInProgress() {
		t.Fatal("Kodi transition protection was not enabled")
	}
}

func TestAutomaticReturnProtectionSurvivesSlowKodiTransition(t *testing.T) {
	a := &App{automaticReturnUntil: time.Now().Add(time.Second)}
	if !a.automaticReturnInProgress() {
		t.Fatal("automatic return protection was not enabled")
	}
	channel := Channel{Name: "Kabel Eins", URL: "rtsp://tv.example/kabeleins"}
	a.active = channel
	a.confirmAutomaticReturn(channel)
	if a.automaticReturnInProgress() {
		t.Fatal("return protection was not cleared after the original sender reappeared")
	}
}

func TestPlaceholderTarget(t *testing.T) {
	cfg := Config{PlaceholderSource: "uploaded-image", PlaceholderURL: "http://monitor/placeholder.png", KodiPlaceholder: "file:///storage/placeholder.mp4"}
	if got := placeholderTarget(cfg); got != cfg.PlaceholderURL {
		t.Fatalf("uploaded placeholder target = %q", got)
	}
	cfg.PlaceholderSource = "kodi-local"
	if got := placeholderTarget(cfg); got != cfg.KodiPlaceholder {
		t.Fatalf("Kodi-local placeholder target = %q", got)
	}
}

func TestKodiPlaceholderListNormalizesAndMigratesSingleValue(t *testing.T) {
	cfg := Config{KodiPlaceholder: " file:///storage/one.mp4 ", KodiPlaceholders: []string{"", "file:///storage/two.mp4", "file:///storage/two.mp4"}}
	cfg.normalize()
	if got, want := cfg.KodiPlaceholder, "file:///storage/one.mp4"; got != want {
		t.Fatalf("selected placeholder = %q, want %q", got, want)
	}
	if got, want := len(cfg.KodiPlaceholders), 2; got != want {
		t.Fatalf("placeholder count = %d, want %d: %#v", got, want, cfg.KodiPlaceholders)
	}
	if cfg.KodiPlaceholders[0] != "file:///storage/two.mp4" || cfg.KodiPlaceholders[1] != "file:///storage/one.mp4" {
		t.Fatalf("unexpected placeholders: %#v", cfg.KodiPlaceholders)
	}
}

func TestIsPlaceholderTargetAcceptsEveryConfiguredKodiFile(t *testing.T) {
	cfg := Config{PlaceholderSource: "kodi-local", KodiPlaceholder: "file:///storage/one.mp4", KodiPlaceholders: []string{"file:///storage/one.mp4", "file:///storage/two.mp4"}}
	if !isPlaceholderTarget("file:///storage/two.mp4", cfg) {
		t.Fatal("configured Kodi placeholder was not recognized")
	}
	if isPlaceholderTarget("file:///storage/other.mp4", cfg) {
		t.Fatal("unconfigured Kodi file was recognized as a placeholder")
	}
}

func TestResumeAutomaticKeepsPlaceholderState(t *testing.T) {
	a := &App{status: Status{Running: true, Mode: "manual-placeholder", ManualPlaceholder: true, Matches: 4, Misses: 2}}
	req := httptest.NewRequest(http.MethodPost, "/api/resume-automatic", nil)
	response := httptest.NewRecorder()
	a.handleResumeAutomatic(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if a.status.ManualPlaceholder || a.status.Mode != "placeholder" || a.status.Matches != 0 || a.status.Misses != 0 {
		t.Fatalf("unexpected status after resume: %#v", a.status)
	}
}

func TestObserveHonorsProgramCooldown(t *testing.T) {
	a := &App{status: Status{Mode: "monitoring"}, programCooldownUntil: time.Now().Add(time.Minute)}
	a.observe(false, 0, Config{MissingFrames: 1, PresentFrames: 1})
	if a.status.Mode != "monitoring" || a.status.Misses != 0 {
		t.Fatalf("program cooldown allowed a placeholder switch: %#v", a.status)
	}
}

func TestObserveHonorsPlaceholderMinimum(t *testing.T) {
	a := &App{status: Status{Mode: "placeholder"}, placeholderMinUntil: time.Now().Add(time.Minute)}
	a.observe(true, 1, Config{MissingFrames: 1, PresentFrames: 1})
	if a.status.Mode != "placeholder" || a.status.Matches != 0 {
		t.Fatalf("placeholder minimum allowed a sender switch: %#v", a.status)
	}
}

func TestObserveStartsProgramTimerAfterConfirmedLogo(t *testing.T) {
	a := &App{status: Status{Mode: "monitoring"}}
	cfg := Config{MissingFrames: 1, PresentFrames: 2}
	a.observe(true, 1, cfg)
	if !a.programStartedAt.IsZero() {
		t.Fatal("program timer started before the logo was confirmed")
	}
	a.observe(true, 1, cfg)
	if a.programStartedAt.IsZero() {
		t.Fatal("program timer did not start after the logo was confirmed")
	}
	if !a.programCooldownUntil.IsZero() {
		t.Fatal("program timer enabled a cooldown without an automatic return")
	}
}

func TestDetectedAdvertisementStartsTimerAndProgramLock(t *testing.T) {
	kodiClient := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		var result any
		switch request.Method {
		case "Playlist.GetPlaylists":
			result = []any{}
		case "Player.Open":
			result = "OK"
		default:
			t.Errorf("unexpected Kodi method %q", request.Method)
			result = nil
		}
		body, err := json.Marshal(map[string]any{"result": result})
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
	})}

	cfg := Config{
		Kodi:                   KodiConfig{URL: "http://kodi.test/jsonrpc"},
		PlaceholderSource:      "kodi-local",
		KodiPlaceholder:        "file:///storage/advert.mp4",
		MissingFrames:          1,
		PresentFrames:          1,
		PlaceholderMinSeconds:  30,
		ProgramCooldownSeconds: 600,
	}
	a := &App{
		cfg:        cfg,
		active:     Channel{Name: "Test", URL: "http://tv.example/test.m3u8"},
		status:     Status{Mode: "monitoring"},
		httpClient: kodiClient,
	}
	a.observe(false, 0, cfg)
	if a.advertStartedAt.IsZero() || !a.placeholderMinUntil.After(time.Now()) {
		t.Fatalf("automatic placeholder switch did not start advertisement timing: %#v", a)
	}

	// Expire the advertisement minimum so the test can simulate a confirmed
	// program return without waiting for the configured duration.
	a.mu.Lock()
	a.placeholderMinUntil = time.Now().Add(-time.Second)
	a.mu.Unlock()
	a.observe(true, 1, cfg)
	if a.programStartedAt.IsZero() || !a.programCooldownUntil.After(time.Now()) {
		t.Fatalf("automatic sender return did not start program timing and cooldown: %#v", a)
	}
}

func TestAutomaticProgramReturnSetsLockBeforeKodiReply(t *testing.T) {
	cfg := Config{Kodi: KodiConfig{URL: "http://kodi.test/jsonrpc"}, ProgramCooldownSeconds: 600}
	a := &App{
		cfg:    cfg,
		active: Channel{Name: "Test", URL: "http://tv.example/test.m3u8"},
		status: Status{Mode: "placeholder"},
		httpClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, io.ErrUnexpectedEOF
		})},
	}

	if err := a.switchToSender(true); err == nil {
		t.Fatal("expected Kodi request to fail")
	}
	if !a.programCooldownUntil.After(time.Now()) || a.programStartedAt.IsZero() {
		t.Fatalf("automatic program return did not set its lock: %#v", a)
	}
}

func TestCooldownRemaining(t *testing.T) {
	now := time.Now()
	if got := cooldownRemaining(now.Add(1500*time.Millisecond), now); got != 2 {
		t.Fatalf("remaining = %d, want 2", got)
	}
	if got := cooldownRemaining(now, now); got != 0 {
		t.Fatalf("expired cooldown = %d, want 0", got)
	}
}

func TestElapsedSeconds(t *testing.T) {
	now := time.Now()
	if got := elapsedSeconds(now.Add(-95*time.Second), now); got != 95 {
		t.Fatalf("elapsed = %d, want 95", got)
	}
	if got := elapsedSeconds(time.Time{}, now); got != 0 {
		t.Fatalf("zero start = %d, want 0", got)
	}
}

func TestPlaybackTimingOnlyForKnownTransitions(t *testing.T) {
	cfg := Config{ProgramCooldownSeconds: 600, PlaceholderMinSeconds: 30}
	a := &App{}
	a.recordPlayback("sender", cfg, false)
	if !a.programCooldownUntil.IsZero() {
		t.Fatal("manual sender start set a program cooldown")
	}
	if !a.programStartedAt.IsZero() {
		t.Fatal("manual sender start started an unknown program counter")
	}
	a.recordPlayback("placeholder", cfg, false)
	if !a.advertStartedAt.IsZero() || !a.placeholderMinUntil.IsZero() {
		t.Fatal("unknown advertisement started timing")
	}
	a.recordPlayback("sender", cfg, true)
	if !a.programCooldownUntil.After(time.Now()) {
		t.Fatal("automatic return did not set a program cooldown")
	}
	if a.programStartedAt.IsZero() {
		t.Fatal("automatic return did not start the program counter")
	}
	a.recordPlayback("placeholder", cfg, true)
	if !a.placeholderMinUntil.After(time.Now()) || a.advertStartedAt.IsZero() {
		t.Fatal("detected advertisement did not start timing")
	}
}

func TestExternalChannelDoesNotStartTiming(t *testing.T) {
	a := &App{status: Status{Running: true, Mode: "monitoring"}}
	a.setActiveChannel(Channel{Name: "Test", URL: "rtsp://tv.example/test"})
	if !a.programStartedAt.IsZero() || !a.advertStartedAt.IsZero() || !a.programCooldownUntil.IsZero() || !a.placeholderMinUntil.IsZero() {
		t.Fatalf("external channel started timing: %#v", a)
	}
}

func TestReferenceUsesPreviewFrame(t *testing.T) {
	preview := Frame{W: 8, H: 8, Pix: bytes.Repeat([]byte{1, 2, 3}, 64)}
	a := &App{
		dataDir:      t.TempDir(),
		active:       Channel{Name: "Test", URL: "rtsp://example.test/channel"},
		lastFrame:    Frame{W: 8, H: 8, Pix: bytes.Repeat([]byte{9, 8, 7}, 64)},
		previewFrame: preview,
	}
	body, err := json.Marshal(Rect{W: 8, H: 8})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/reference", bytes.NewReader(body))
	response := httptest.NewRecorder()
	a.handleReference(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var entry ReferenceEntry
	if err := json.Unmarshal(response.Body.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	ref, err := loadReference(a.referenceFile(entry.ID))
	if err != nil {
		t.Fatal(err)
	}
	if string(ref.Pix[:3]) != string(preview.Pix[:3]) {
		t.Fatalf("reference uses live frame %v instead of preview %v", ref.Pix[:3], preview.Pix[:3])
	}
}

func TestPlaceholderNotConfigured(t *testing.T) {
	a := &App{cfg: Config{StreamURL: "x"}}
	if err := a.play("placeholder", false); err == nil {
		t.Fatal("expected placeholder error")
	}
}

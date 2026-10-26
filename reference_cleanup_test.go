package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func cleanupTestApp(t *testing.T, entries ...ReferenceEntry) (*App, *referenceLearningSession, []*logoTemplate) {
	t.Helper()
	cfg := detectionTestConfig()
	cfg.DetectionMode = "compare"
	cfg.Kodi.URL = "http://kodi.test"
	a := &App{cfg: cfg, dataDir: t.TempDir(),
		active: Channel{Name: "Test", URL: "local://test", KodiChannelID: kodiTestID(1)},
		status: Status{Running: true, Mode: "sender", Matches: cfg.PresentFrames}}
	var refs []*logoTemplate
	for _, entry := range entries {
		ref := addCleanupTestReference(t, a, entry)
		if !entry.Disabled && (entry.StreamURL == "" || entry.StreamURL == a.active.URL) {
			refs = append(refs, ref)
		}
	}
	session := a.beginReferenceLearning(context.Background(), a.active.URL, refs)
	return a, session, refs
}

func addCleanupTestReference(t *testing.T, a *App, entry ReferenceEntry) *logoTemplate {
	t.Helper()
	if entry.StreamURL == "" {
		entry.StreamURL = a.active.URL
	}
	if entry.Rect.W == 0 {
		entry.Rect = Rect{X: 12, Y: 6, W: 40, H: 28}
	}
	f := cropFrame(autoTestFrame(0, true), Rect{X: 12, Y: 6, W: entry.Rect.W, H: entry.Rect.H})
	mask := automaticLogoMask(f)
	if err := a.addMaskedReference(entry, f, mask); err != nil {
		t.Fatal(err)
	}
	return prepareLogo(entry.ID, f, entry.Rect, mask)
}

func cleanupTestResults(refs []*logoTemplate, missing string) []LogoResult {
	results := make([]LogoResult, 0, len(refs))
	for _, ref := range refs {
		decision := "present"
		if ref.id == missing {
			decision = "missing"
		}
		results = append(results, LogoResult{ReferenceID: ref.id, Decision: decision})
	}
	return results
}

func pollCleanupProgramme(t *testing.T, a *App, title string) {
	t.Helper()
	a.httpClient = kodiTestClient(t, func(string, map[string]json.RawMessage) (string, error) {
		encoded, err := json.Marshal(map[string]any{"result": map[string]any{"channeldetails": map[string]any{
			"broadcastnow": map[string]string{"title": title},
		}}})
		return string(encoded), err
	})
	a.syncKodiProgramme(a.cfg.Kodi)
}

func TestReferenceCleanupAfterProgrammeChange(t *testing.T) {
	a, session, refs := cleanupTestApp(t,
		ReferenceEntry{ID: "overlay", Automatic: true},
		ReferenceEntry{ID: "sender", Automatic: true},
		ReferenceEntry{ID: "disabled", Automatic: true, Disabled: true},
		ReferenceEntry{ID: "manual"},
		ReferenceEntry{ID: "other", Automatic: true, StreamURL: "local://other"})
	pollCleanupProgramme(t, a, "Old programme")
	if len(a.referenceCleanup.review) != 0 {
		t.Fatal("first title started cleanup")
	}
	pollCleanupProgramme(t, a, "New programme")
	if len(a.referenceCleanup.review) != 0 {
		t.Fatal("unconfirmed title started cleanup")
	}
	pollCleanupProgramme(t, a, "New programme")
	if len(a.referenceCleanup.review) != 2 {
		t.Fatalf("unexpected review: %v", a.referenceCleanup.review)
	}
	// References learned after the boundary do not join its five-minute review.
	refs = append(refs, addCleanupTestReference(t, a, ReferenceEntry{ID: "new", Automatic: true}))
	results := cleanupTestResults(refs, "overlay")
	results[len(results)-1].Decision = "missing"
	// The automatic sender reference is present but is not the best match.
	results[1].Score, results[2].Score = .85, 1
	now := time.Unix(1000, 0)
	for elapsed := time.Duration(0); elapsed < referenceProgrammeGrace; elapsed += 2 * time.Second {
		refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now.Add(elapsed))
	}
	if _, err := a.referenceByID("overlay"); err != nil {
		t.Fatal("overlay removed before five observed minutes", err)
	}
	refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now.Add(referenceProgrammeGrace))
	if len(refs) != 3 {
		t.Fatalf("live templates = %d, want 3", len(refs))
	}
	if _, err := a.referenceByID("overlay"); !os.IsNotExist(err) {
		t.Fatal("expired overlay retained in index", err)
	}
	for _, path := range []string{a.referenceFile("overlay"), a.maskFile("overlay"), a.automaticMaskFile("overlay")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expired file retained: %s: %v", path, err)
		}
	}
	for _, id := range []string{"sender", "manual", "disabled", "other", "new"} {
		if _, err := a.referenceByID(id); err != nil {
			t.Fatalf("protected reference %s removed: %v", id, err)
		}
	}
	if len(a.profiles[a.active.URL].Blocked) != 0 || a.detection.automaticState != "searching" {
		t.Fatal("cleanup suppressed rediscovery or failed to resume it")
	}
}

func TestReferenceCleanupWithoutEPGAndMatchResetsAge(t *testing.T) {
	a, session, refs := cleanupTestApp(t, ReferenceEntry{ID: "overlay", Automatic: true}, ReferenceEntry{ID: "manual"})
	now := time.Unix(1000, 0)
	results := cleanupTestResults(refs, "overlay")
	for elapsed := time.Duration(0); elapsed <= 20*time.Minute; elapsed += 2 * time.Second {
		refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now.Add(elapsed))
	}
	now = now.Add(20*time.Minute + 2*time.Second)
	refs = a.cleanupAutomaticReferences(session, refs, cleanupTestResults(refs, ""), a.cfg, nil, now)
	now = now.Add(2 * time.Second)
	for elapsed := time.Duration(0); elapsed < referenceUnusedLimit; elapsed += 2 * time.Second {
		refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now.Add(elapsed))
	}
	if _, err := a.referenceByID("overlay"); err != nil {
		t.Fatal("reference expired before 30 minutes since its last match", err)
	}
	refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now.Add(referenceUnusedLimit))
	if len(refs) != 1 || refs[0].id != "manual" {
		t.Fatal("fallback did not expire only the automatic reference")
	}
}

func TestReferenceCleanupPausesAndDiscontinuities(t *testing.T) {
	for _, condition := range []string{"advert", "manual", "stopped", "paused", "protection", "validation", "uncertain programme", "uncertain reference", "decoder gap", "decoder restart", "timeline reset", "unconfirmed programme"} {
		t.Run(condition, func(t *testing.T) {
			a, session, refs := cleanupTestApp(t, ReferenceEntry{ID: "overlay", Automatic: true}, ReferenceEntry{ID: "manual"})
			now := time.Unix(1000, 0)
			results := cleanupTestResults(refs, "overlay")
			sample := testObservation(0, "present")
			refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, &sample, now)
			sample.Time = 2
			refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, &sample, now.Add(2*time.Second))
			before := a.referenceCleanup.unused["overlay"]
			if before != 2*time.Second {
				t.Fatal("test did not establish observed absence", before)
			}
			sample.Time = 4
			now = now.Add(4 * time.Second)
			switch condition {
			case "advert":
				a.status.Mode = "placeholder"
			case "manual":
				a.status.ManualPlaceholder = true
			case "stopped":
				a.status.Running = false
			case "paused":
				a.status.DetectionPaused = true
			case "protection":
				a.programCooldownUntil = now.Add(time.Hour)
			case "validation":
				a.recording = &ObservationRecording{State: "recording", Purpose: "validation"}
			case "uncertain programme":
				results[1].Decision = "uncertain"
			case "uncertain reference":
				results[0].Decision, results[0].UncertaintyReason = "uncertain", "logo_color_low_contrast"
			case "decoder gap":
				now = now.Add(time.Hour)
			case "decoder restart":
				session = a.beginReferenceLearning(context.Background(), a.active.URL, refs)
			case "timeline reset":
				sample.Session++
				sample.Time = 0
			case "unconfirmed programme":
				a.status.Matches = 0
			}
			refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, &sample, now)
			if got := a.referenceCleanup.unused["overlay"]; got != before {
				t.Fatalf("unobserved time advanced cleanup: %v -> %v", before, got)
			}
			// Restore a running programme: the first frame must not bridge the gap.
			results = cleanupTestResults(refs, "overlay")
			a.status.Running, a.status.DetectionPaused, a.status.ManualPlaceholder = true, false, false
			a.status.Mode, a.status.Matches = "sender", a.cfg.PresentFrames
			a.programCooldownUntil, a.recording = time.Time{}, nil
			sample.Time += 2
			a.cleanupAutomaticReferences(session, refs, results, a.cfg, &sample, now.Add(2*time.Second))
			// A restarted decoder's first frame was already eligible; its next one counts.
			want := before
			if condition == "decoder restart" || condition == "decoder gap" || condition == "timeline reset" {
				want += 2 * time.Second
			}
			if got := a.referenceCleanup.unused["overlay"]; got != want {
				t.Fatalf("resumed timer = %v, want %v", got, want)
			}
		})
	}
}

func TestReferenceCleanupRejectsCancelledOrOldDecoder(t *testing.T) {
	a, old, refs := cleanupTestApp(t, ReferenceEntry{ID: "overlay", Automatic: true}, ReferenceEntry{ID: "manual"})
	ctx, cancel := context.WithCancel(context.Background())
	session := a.beginReferenceLearning(ctx, a.active.URL, refs)
	results := cleanupTestResults(refs, "overlay")
	now := time.Unix(1000, 0)
	a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now)
	a.cleanupAutomaticReferences(old, refs, results, a.cfg, nil, now.Add(time.Second))
	cancel()
	a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now.Add(2*time.Second))
	if !a.referenceCleanup.last.Equal(now) || a.referenceCleanup.unused["overlay"] != 0 {
		t.Fatal("obsolete decoder mutated cleanup state")
	}
}

func TestReferenceCleanupEPGConfirmation(t *testing.T) {
	a, _, _ := cleanupTestApp(t, ReferenceEntry{ID: "overlay", Automatic: true})
	for _, title := range []string{"Old", "New", "", "New", "Old", "New"} {
		pollCleanupProgramme(t, a, title)
		if len(a.referenceCleanup.review) != 0 {
			t.Fatal("intermittent EPG started a review")
		}
	}
	a.httpClient = kodiTestClient(t, func(string, map[string]json.RawMessage) (string, error) { return "", io.ErrUnexpectedEOF })
	a.syncKodiProgramme(a.cfg.Kodi)
	pollCleanupProgramme(t, a, "New")
	if len(a.referenceCleanup.review) != 0 {
		t.Fatal("failed EPG did not interrupt confirmation")
	}
	pollCleanupProgramme(t, a, "New")
	if len(a.referenceCleanup.review) != 1 {
		t.Fatal("confirmed EPG did not start a review")
	}
	// A repeated title must not renew or rebuild an existing review.
	delete(a.referenceCleanup.review, "overlay")
	pollCleanupProgramme(t, a, "New")
	if len(a.referenceCleanup.review) != 0 {
		t.Fatal("unchanged EPG restarted review")
	}
}

func TestReferenceCleanupStorageFailureKeepsLiveReference(t *testing.T) {
	a, session, refs := cleanupTestApp(t, ReferenceEntry{ID: "overlay", Automatic: true}, ReferenceEntry{ID: "manual"})
	now := time.Unix(1000, 0)
	results := cleanupTestResults(refs, "overlay")
	a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now)
	a.referenceCleanup.unused["overlay"] = referenceUnusedLimit
	if err := os.Chmod(a.dataDir, 0500); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = os.Chmod(a.dataDir, 0700) })
	if probe, err := os.CreateTemp(a.dataDir, "permission-probe-"); err == nil {
		probe.Close()
		os.Remove(probe.Name())
		t.Skip("directory permissions do not prevent writes")
	}
	live := a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now.Add(2*time.Second))
	if len(live) != 2 || !strings.Contains(a.status.LastError, "automatic reference cleanup:") {
		t.Fatal("index failure was not reported or removed live template", a.status.LastError)
	}
	if _, err := a.referenceByID("overlay"); err != nil {
		t.Fatal("failed index update removed reference", err)
	}
	if err := os.Chmod(a.dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	live = a.cleanupAutomaticReferences(session, live, results, a.cfg, nil, now.Add(4*time.Second))
	if len(live) != 1 {
		t.Fatal("cleanup did not retry after storage recovered")
	}
}

func TestReferenceCleanupFileFailureStillRemovesLiveReference(t *testing.T) {
	a, session, refs := cleanupTestApp(t, ReferenceEntry{ID: "overlay", Automatic: true}, ReferenceEntry{ID: "manual"})
	// A nonempty directory at the image path forces removal to fail on all OSes.
	if err := os.Remove(a.referenceFile("overlay")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(a.referenceFile("overlay"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.referenceFile("overlay"), "block"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	results := cleanupTestResults(refs, "overlay")
	a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now)
	a.referenceCleanup.unused["overlay"] = referenceUnusedLimit
	live := a.cleanupAutomaticReferences(session, refs, results, a.cfg, nil, now.Add(2*time.Second))
	if len(live) != 1 || !strings.Contains(a.status.LastError, "automatic reference cleanup:") {
		t.Fatal("file failure lost index/live consistency or was not reported", a.status.LastError)
	}
	if _, err := a.referenceByID("overlay"); !os.IsNotExist(err) {
		t.Fatal("reference retained after successful index update", err)
	}
	for _, path := range []string{a.maskFile("overlay"), a.automaticMaskFile("overlay")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("one failed deletion prevented remaining file cleanup", err)
		}
	}
}

func TestReferenceCleanupFreesDiscoverySlot(t *testing.T) {
	a, session, refs := cleanupTestApp(t,
		ReferenceEntry{ID: "overlay", Automatic: true},
		ReferenceEntry{ID: "sender", Automatic: true, Rect: Rect{240, 6, 40, 28}},
		ReferenceEntry{ID: "variant1", Automatic: true, Rect: Rect{12, 140, 40, 28}},
		ReferenceEntry{ID: "variant2", Automatic: true, Rect: Rect{240, 140, 40, 28}})
	f := autoTestFrame(0, true)
	o := testObservation(122, "present")
	if added := a.processAutomaticFrame(context.Background(), f, a.cfg, a.active, refs, &o); added != nil || a.detection.automaticState != "complete" {
		t.Fatal("test did not fill all discovery slots")
	}
	now := time.Unix(1000, 0)
	results := cleanupTestResults(refs, "overlay")
	refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, &o, now)
	a.referenceCleanup.unused["overlay"] = referenceUnusedLimit
	o.Time += 2
	refs = a.cleanupAutomaticReferences(session, refs, results, a.cfg, &o, now.Add(2*time.Second))
	if len(refs) != 3 || a.referenceLearning != session {
		t.Fatal("cleanup did not free a live slot in the existing decoder")
	}
	// Supply an independently validated candidate in the expired logo's region.
	r := Rect{X: 12, Y: 6, W: 40, H: 28}
	frame := cropFrame(f, r)
	mask := automaticLogoMask(frame)
	a.detection.automatic = &automaticSearch{state: "validating", session: o.Session, last: -1,
		candidates: []*autoCandidate{{rect: r, frame: frame, mask: mask, template: prepareLogo("", frame, r, mask),
			validationStart: o.Time - automaticValidationSeconds, lastMatch: o.Time - 2, tested: 150, matched: 150}}}
	added := a.processAutomaticFrame(context.Background(), f, a.cfg, a.active, refs, &o)
	if added == nil {
		t.Fatal("expired region could not be learned again")
	}
	entries, err := a.referencesFor(a.active.URL)
	if err != nil || len(entries) != 4 {
		t.Fatalf("discovery did not refill the slot: %v %v", entries, err)
	}
}

func TestReferenceCleanupDiscardsOutOfOrderEPG(t *testing.T) {
	a, _, _ := cleanupTestApp(t, ReferenceEntry{ID: "overlay", Automatic: true})
	pollCleanupProgramme(t, a, "Old")
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	a.httpClient = kodiTestClient(t, func(string, map[string]json.RawMessage) (string, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return `{"result":{"channeldetails":{"broadcastnow":{"title":"Stale"}}}}`, nil
		}
		return `{"result":{"channeldetails":{"broadcastnow":{"title":"New"}}}}`, nil
	})
	go func() {
		defer close(done)
		a.syncKodiProgramme(a.cfg.Kodi)
	}()
	<-started
	a.syncKodiProgramme(a.cfg.Kodi)
	close(release)
	<-done
	if a.status.KodiProgramme != "New" || a.referenceCleanup.pendingTitle != "New" || len(a.referenceCleanup.review) != 0 {
		t.Fatal("old response changed programme or cleanup confirmation")
	}
	a.syncKodiProgramme(a.cfg.Kodi)
	if a.referenceCleanup.title != "New" || len(a.referenceCleanup.review) != 1 {
		t.Fatal("fresh confirmation did not start cleanup")
	}
}

func TestReferenceCleanupResetsWithSenderOrKodiConnection(t *testing.T) {
	for _, connection := range []bool{false, true} {
		t.Run(map[bool]string{false: "sender", true: "connection"}[connection], func(t *testing.T) {
			a, _, _ := cleanupTestApp(t, ReferenceEntry{ID: "overlay", Automatic: true})
			pollCleanupProgramme(t, a, "Old")
			pollCleanupProgramme(t, a, "New")
			pollCleanupProgramme(t, a, "New")
			a.referenceCleanup.unused = map[string]time.Duration{"overlay": time.Minute}
			request := a.kodiProgrammeRequest
			// Stop detection so this state-reset test never starts a real decoder.
			a.status.Running = false
			if connection {
				a.configPath = filepath.Join(a.dataDir, "config.json")
				cfg := a.cfg
				cfg.Kodi.URL = "http://other-kodi.test"
				body, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				refreshed := make(chan struct{})
				a.httpClient = kodiTestClient(t, func(string, map[string]json.RawMessage) (string, error) {
					defer close(refreshed)
					return "", io.ErrUnexpectedEOF
				})
				w := httptest.NewRecorder()
				a.handleConfig(w, httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(string(body))))
				if w.Code != http.StatusOK {
					t.Fatalf("configuration update failed: %s", w.Body.String())
				}
				<-refreshed
			} else {
				a.setActiveChannel(Channel{})
			}
			if a.referenceCleanup.title != "" || len(a.referenceCleanup.review) != 0 || len(a.referenceCleanup.unused) != 0 || a.kodiProgrammeRequest <= request {
				t.Fatal("sender/connection change retained cleanup or accepted old EPG requests")
			}
		})
	}
}

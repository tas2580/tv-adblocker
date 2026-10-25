package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func referenceDetailsApp(t *testing.T) (*App, Frame, []byte) {
	t.Helper()
	a := &App{dataDir: t.TempDir(), cfg: defaultConfig(), active: Channel{URL: "test://one"}}
	f := compositeTestLogo(48, 36, 0, .85, true)
	return a, f, explicitTestMask(f.W, f.H)
}

func getReferenceDetails(t *testing.T, a *App) []referenceDetails {
	t.Helper()
	w := httptest.NewRecorder()
	a.handleReferences(w, httptest.NewRequest(http.MethodGet, "/api/references", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET references: %d %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("live metadata can be cached")
	}
	var entries []referenceDetails
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestReferenceDetailsInspectStoredAndGeneratedData(t *testing.T) {
	a, f, mask := referenceDetailsApp(t)
	for _, source := range []string{"single", "series", "legacy"} {
		entry := ReferenceEntry{ID: source, StreamURL: a.active.URL, Rect: Rect{W: f.W, H: f.H}}
		var err error
		if source == "legacy" {
			err = a.addReference(entry, f)
		} else {
			entry.MaskSource = source
			err = a.addMaskedReference(entry, f, mask)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := a.addReference(ReferenceEntry{ID: "other", StreamURL: "test://other", Rect: Rect{W: f.W, H: f.H}}, f); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(a.referencesPath())
	if err != nil {
		t.Fatal(err)
	}
	entries := getReferenceDetails(t, a)
	if len(entries) != 3 {
		t.Fatalf("returned other channel or missing references: %+v", entries)
	}
	for _, entry := range entries {
		d := entry.Data
		if d.Image != "available" || !d.ShapeUsable || d.Color.State != "inactive" {
			t.Fatalf("invalid data for %s: %+v", entry.ID, d)
		}
		if entry.ID == "legacy" {
			if d.Source != "unknown" || d.Mask != "generated" || d.AutomaticMask != "missing" {
				t.Fatalf("legacy treated as stored: %+v", d)
			}
		} else if d.Source != entry.ID || d.Mask != "available" || d.AutomaticMask != "available" {
			t.Fatalf("saved data missing: %+v", d)
		}
	}
	after, err := os.ReadFile(a.referencesPath())
	if err != nil || string(before) != string(after) {
		t.Fatal("reading availability changed the reference index")
	}
	if _, err = os.Stat(a.maskFile("legacy")); !os.IsNotExist(err) {
		t.Fatal("inspecting legacy reference saved a mask")
	}
}

func TestReferenceDetailsReportMissingCorruptAndUnusableFiles(t *testing.T) {
	for _, fault := range []string{"missingImage", "corruptImage", "corruptMask", "wrongMaskSize", "emptyMask", "missingProposal", "corruptProposal"} {
		t.Run(fault, func(t *testing.T) {
			a, f, mask := referenceDetailsApp(t)
			entry := ReferenceEntry{ID: "ref", StreamURL: a.active.URL, MaskSource: "series", Rect: Rect{W: f.W, H: f.H}}
			if err := a.addMaskedReference(entry, f, mask); err != nil {
				t.Fatal(err)
			}
			var err error
			switch fault {
			case "missingImage":
				err = os.Remove(a.referenceFile(entry.ID))
			case "corruptImage":
				err = os.WriteFile(a.referenceFile(entry.ID), []byte("bad image"), 0600)
			case "corruptMask":
				err = os.WriteFile(a.maskFile(entry.ID), []byte("bad mask"), 0600)
			case "wrongMaskSize":
				err = saveLogoMaskFile(a.maskFile(entry.ID), solidFrame(8, 8, 90), make([]byte, 64))
			case "emptyMask":
				err = a.saveLogoMask(entry.ID, f, make([]byte, f.W*f.H))
			case "missingProposal":
				err = os.Remove(a.automaticMaskFile(entry.ID))
			case "corruptProposal":
				err = os.WriteFile(a.automaticMaskFile(entry.ID), []byte("bad proposal"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			d := getReferenceDetails(t, a)[0].Data
			switch fault {
			case "missingImage":
				if d.Image != "missing" || d.ShapeUsable {
					t.Fatalf("missing image: %+v", d)
				}
			case "corruptImage":
				if d.Image != "invalid" || d.ShapeUsable {
					t.Fatalf("corrupt image: %+v", d)
				}
			case "corruptMask", "wrongMaskSize":
				if d.Mask != "invalid" || d.ShapeUsable {
					t.Fatalf("invalid mask: %+v", d)
				}
			case "emptyMask":
				if d.Mask != "available" || d.ShapeUsable {
					t.Fatalf("empty mask: %+v", d)
				}
			case "missingProposal":
				if d.AutomaticMask != "missing" || !d.ShapeUsable {
					t.Fatalf("optional proposal: %+v", d)
				}
			case "corruptProposal":
				if d.AutomaticMask != "invalid" || !d.ShapeUsable {
					t.Fatalf("optional proposal: %+v", d)
				}
			}
		})
	}
}

func TestReferenceDetailsExposeEveryTemplatesLearningAndResetWithSession(t *testing.T) {
	a, f, mask := referenceDetailsApp(t)
	refs := make([]*logoTemplate, 0, 3)
	for _, id := range []string{"waiting", "learning", "ready"} {
		entry := ReferenceEntry{ID: id, StreamURL: a.active.URL, Rect: Rect{W: f.W, H: f.H}}
		if err := a.addMaskedReference(entry, f, mask); err != nil {
			t.Fatal(err)
		}
		ref := prepareLogo(id, f, entry.Rect, mask)
		if id == "learning" {
			for scene := 0; scene < 3; scene++ {
				var plane logoPlane
				ref.find(compositeTestLogo(f.W, f.H, scene, .85, true), &plane, .7)
			}
		}
		if id == "ready" {
			ref = calibratedColorTemplate(t, [3]byte{245, 245, 245}, .85)
			ref.id = id
		}
		refs = append(refs, ref)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := a.beginReferenceLearning(ctx, a.active.URL, refs)
	entries := getReferenceDetails(t, a)
	for i, state := range []string{"waiting", "learning", "ready"} {
		if entries[i].Data.Color.State != state {
			t.Fatalf("%s: %+v", state, entries[i].Data.Color)
		}
	}
	if entries[1].Data.Color.Samples != 3 || entries[2].Data.Color.Estimate == nil {
		t.Fatalf("missing progress or estimate: %+v", entries)
	}
	// Updates publish immutable copies, independent of mutable detector state.
	old := entries[2].Data.Color.Estimate.RGB
	refs[2].color.estimate.RGB[0] = 0
	if getReferenceDetails(t, a)[2].Data.Color.Estimate.RGB != old {
		t.Fatal("HTTP reads mutable template state")
	}
	// Exercise publication concurrently with HTTP reads under the race detector.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 40; i++ {
			a.publishReferenceLearning(session, refs)
		}
	}()
	for i := 0; i < 8; i++ {
		getReferenceDetails(t, a)
	}
	wg.Wait()
	cancel()
	for _, entry := range getReferenceDetails(t, a) {
		if entry.Data.Color.State != "inactive" || entry.Data.Color.Estimate != nil {
			t.Fatal("cancelled session still reports learned colour")
		}
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	newSession := a.beginReferenceLearning(ctx2, a.active.URL, refs[:1])
	a.endReferenceLearning(session)
	if getReferenceDetails(t, a)[0].Data.Color.State != "waiting" {
		t.Fatal("old decoder cleared the new decoder's learning state")
	}
	if a.referenceColorSnapshot("test://other") != nil {
		t.Fatal("learning leaked to another channel")
	}
	a.endReferenceLearning(newSession)
	if getReferenceDetails(t, a)[0].Data.Color.State != "inactive" {
		t.Fatal("ended session still available")
	}
	// The reference index contains only its original persisted fields.
	data, err := os.ReadFile(filepath.Join(a.dataDir, "references.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index map[string]json.RawMessage
	if err = json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	var saved []map[string]json.RawMessage
	if err = json.Unmarshal(index["references"], &saved); err != nil {
		t.Fatal(err)
	}
	if _, ok := saved[0]["data"]; ok {
		t.Fatal("runtime metadata persisted")
	}
}

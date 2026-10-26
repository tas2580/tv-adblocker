package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func automaticShapeFixture(t *testing.T, path string) (Frame, []byte) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	config, err := png.DecodeConfig(file)
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	frame := solidFrame(config.Width, config.Height, 0)
	mask, err := loadLogoMaskFile(path, frame)
	if err != nil {
		t.Fatal(err)
	}
	return frame, mask
}

func TestAutomaticLogoShapeBroadcastMasks(t *testing.T) {
	for _, test := range []struct {
		path string
		want bool
	}{
		{"automatic-reference-shapes/vertical-strip-27.png", false},
		{"automatic-reference-shapes/vertical-strip-26.png", false},
		{"automatic-reference-shapes/rtl.png", true},
		{"automatic-reference-shapes/vox.png", true},
		{"prosieben-white-sky/mask.png", true},
		{"dmax-white-sky/mask.png", true},
		{"sat1-series/old-mask.png", true},
	} {
		t.Run(test.path, func(t *testing.T) {
			frame, mask := automaticShapeFixture(t, filepath.Join("testdata", test.path))
			if got := automaticLogoShape(mask, frame.W, frame.H); got != test.want {
				t.Fatalf("shape accepted = %t, want %t", got, test.want)
			}
		})
	}
}

func TestAutomaticLogoShapeSizeSupportAndLines(t *testing.T) {
	for _, test := range []struct {
		name string
		w, h int
		keep func(int, int) bool
		want bool
	}{
		{"minimum dimensions", 20, 20, func(x, y int) bool { return true }, true},
		{"narrow", 19, 40, func(x, y int) bool { return true }, false},
		{"short", 40, 19, func(x, y int) bool { return true }, false},
		{"padding around narrow shape", 60, 60, func(x, y int) bool { return x >= 20 && x < 39 && y >= 10 && y < 40 }, false},
		{"200 supported pixels", 20, 20, func(x, y int) bool { return (x+y)%2 == 0 }, true},
		{"199 supported pixels", 20, 20, func(x, y int) bool { return (x+y)%2 == 0 && (x != 0 || y != 0) }, false},
		{"vertical", 60, 60, func(x, y int) bool { return x >= 25 && x < 30 }, false},
		{"horizontal", 60, 60, func(x, y int) bool { return y >= 25 && y < 30 }, false},
		{"diagonal", 60, 60, func(x, y int) bool { return x-y >= -2 && x-y <= 2 }, false},
		{"reverse diagonal", 60, 60, func(x, y int) bool { return x+y >= 57 && x+y <= 61 }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			mask := make([]byte, test.w*test.h)
			for y := 0; y < test.h; y++ {
				for x := 0; x < test.w; x++ {
					// Weight 100 must not enlarge a shape's supported footprint.
					mask[y*test.w+x] = 100
					if test.keep(x, y) {
						mask[y*test.w+x] = 101
					}
				}
			}
			if got := automaticLogoShape(mask, test.w, test.h); got != test.want {
				t.Fatalf("shape accepted = %t, want %t", got, test.want)
			}
		})
	}
	if automaticLogoShape(nil, 20, 20) || automaticLogoShape(make([]byte, 400), 0, 20) {
		t.Fatal("invalid mask dimensions accepted")
	}
}

func TestAutomaticRegionsSkipStripsBeforeCandidateLimit(t *testing.T) {
	const w, h = 320, 180
	mask := make([]byte, w*h)
	for _, r := range []Rect{{20, 10, 5, 40}, {60, 10, 5, 40}, {100, 10, 5, 40}, {140, 10, 5, 40}, {240, 100, 20, 20}} {
		for y := r.Y; y < r.Y+r.H; y++ {
			for x := r.X; x < r.X+r.W; x++ {
				mask[y*w+x] = 255
			}
		}
	}
	regions := stableLogoRegions(mask, w, h)
	if len(regions) != 1 || !overlapsLogo(regions[0], Rect{240, 100, 20, 20}) {
		t.Fatalf("strips occupied the candidate slots: %v", regions)
	}
}

func addAutomaticShapeFixture(t *testing.T, a *App, entry ReferenceEntry, fixture string) {
	t.Helper()
	frame, mask := automaticShapeFixture(t, filepath.Join("testdata", fixture))
	entry.Rect = Rect{X: 20, Y: 20, W: frame.W, H: frame.H}
	if err := a.addMaskedReference(entry, frame, mask); err != nil {
		t.Fatal(err)
	}
}

func TestPruneInvalidAutomaticReferencesAcrossSenders(t *testing.T) {
	a := &App{dataDir: t.TempDir()}
	for _, test := range []struct {
		id, stream, fixture string
		automatic, disabled bool
	}{
		{"strip1", "one", "automatic-reference-shapes/vertical-strip-27.png", true, false},
		{"strip2", "two", "automatic-reference-shapes/vertical-strip-26.png", true, false},
		{"manual", "one", "automatic-reference-shapes/vertical-strip-27.png", false, false},
		{"disabled", "two", "automatic-reference-shapes/vertical-strip-26.png", true, true},
		{"small-logo", "three", "prosieben-white-sky/mask.png", true, false},
		{"wide-logo", "four", "automatic-reference-shapes/rtl.png", true, false},
	} {
		addAutomaticShapeFixture(t, a, ReferenceEntry{ID: test.id, StreamURL: test.stream, Automatic: test.automatic, Disabled: test.disabled}, test.fixture)
	}
	if err := a.pruneInvalidAutomaticReferences(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"strip1", "strip2"} {
		if _, err := a.referenceByID(id); !os.IsNotExist(err) {
			t.Fatalf("invalid automatic reference retained: %s: %v", id, err)
		}
		for _, path := range []string{a.referenceFile(id), a.maskFile(id), a.automaticMaskFile(id)} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("expired asset retained: %s: %v", path, err)
			}
		}
	}
	for _, id := range []string{"manual", "disabled", "small-logo", "wide-logo"} {
		if _, err := a.referenceByID(id); err != nil {
			t.Fatalf("protected reference removed: %s: %v", id, err)
		}
		for _, path := range []string{a.referenceFile(id), a.maskFile(id), a.automaticMaskFile(id)} {
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("protected asset removed: %s: %v", path, err)
			}
		}
	}
	before, err := os.ReadFile(a.referencesPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.pruneInvalidAutomaticReferences(); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(a.referencesPath())
	if !bytes.Equal(before, after) {
		t.Fatal("repeated startup changed suitable references")
	}
	if _, err := os.Stat(filepath.Join(a.dataDir, "sender-profiles.json")); !os.IsNotExist(err) {
		t.Fatal("startup pruning wrote blocked regions", err)
	}
}

func TestPruneInvalidAutomaticReferencesRetainsUnreadableData(t *testing.T) {
	a := &App{dataDir: t.TempDir()}
	for _, id := range []string{"unreadable-image", "unreadable-mask", "strip"} {
		addAutomaticShapeFixture(t, a, ReferenceEntry{ID: id, Automatic: true}, "automatic-reference-shapes/vertical-strip-27.png")
	}
	if err := os.Remove(a.referenceFile("unreadable-image")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.maskFile("unreadable-mask"), []byte("invalid PNG"), 0600); err != nil {
		t.Fatal(err)
	}
	err := a.pruneInvalidAutomaticReferences()
	if err == nil || !strings.Contains(err.Error(), "unreadable-image") || !strings.Contains(err.Error(), "unreadable-mask") {
		t.Fatal("unreadable references were not reported", err)
	}
	for _, id := range []string{"unreadable-image", "unreadable-mask"} {
		if _, err := a.referenceByID(id); err != nil {
			t.Fatal("unreadable reference removed", id, err)
		}
	}
	if _, err := a.referenceByID("strip"); !os.IsNotExist(err) {
		t.Fatal("readable invalid reference was not removed", err)
	}
}

func TestPruneInvalidAutomaticReferencesIndexFailure(t *testing.T) {
	a := &App{dataDir: t.TempDir()}
	addAutomaticShapeFixture(t, a, ReferenceEntry{ID: "strip", Automatic: true}, "automatic-reference-shapes/vertical-strip-27.png")
	if err := os.Chmod(a.dataDir, 0500); err != nil {
		t.Skip(err)
	}
	t.Cleanup(func() { _ = os.Chmod(a.dataDir, 0700) })
	if probe, err := os.CreateTemp(a.dataDir, "permission-probe-"); err == nil {
		probe.Close()
		os.Remove(probe.Name())
		t.Skip("directory permissions do not prevent writes")
	}
	if err := a.pruneInvalidAutomaticReferences(); err == nil {
		t.Fatal("index failure was not reported")
	}
	if _, err := a.referenceByID("strip"); err != nil {
		t.Fatal("reference removed despite failed index update", err)
	}
	for _, path := range []string{a.referenceFile("strip"), a.maskFile("strip"), a.automaticMaskFile("strip")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("asset removed despite failed index update", err)
		}
	}
	if err := os.Chmod(a.dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.pruneInvalidAutomaticReferences(); err != nil {
		t.Fatal(err)
	}
	index, err := a.loadReferenceIndex()
	if err != nil || index.References == nil || len(index.References) != 0 {
		t.Fatalf("retry did not save an empty reference array: %+v %v", index, err)
	}
}

func TestPruneInvalidAutomaticReferencesFileFailure(t *testing.T) {
	a := &App{dataDir: t.TempDir()}
	addAutomaticShapeFixture(t, a, ReferenceEntry{ID: "strip", Automatic: true}, "automatic-reference-shapes/vertical-strip-27.png")
	// Keep the actual image/mask readable while forcing a sidecar deletion error.
	path := a.automaticMaskFile("strip")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "block"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.pruneInvalidAutomaticReferences(); err == nil {
		t.Fatal("file removal failure was not reported")
	}
	if _, err := a.referenceByID("strip"); !os.IsNotExist(err) {
		t.Fatal("reference retained after successful index update", err)
	}
	for _, path := range []string{a.referenceFile("strip"), a.maskFile("strip")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("remaining files were not removed", err)
		}
	}
}

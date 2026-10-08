// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTempPresetDirs(t *testing.T) (lib string, dir string) {
	t.Helper()
	oldLib, oldDir := presetLibraryPath, userPresetDir
	dir = t.TempDir()
	lib = filepath.Join(dir, "library.json")
	presetLibraryPath, userPresetDir = lib, filepath.Join(dir, "user")
	t.Cleanup(func() { presetLibraryPath, userPresetDir = oldLib, oldDir })
	return lib, dir
}

func TestPresetFileNameIsSafe(t *testing.T) {
	cases := map[string]string{
		"Clear Vocals":   "Clear Vocals.json",
		"../etc/passwd":  "_etc_passwd.json",
		"a/b\\c":         "a_b_c.json",
		"  ":             "preset.json",
		"trailing.dots.": "trailing.dots.json",
	}
	for in, want := range cases {
		if got := presetFileName(in); got != want {
			t.Errorf("presetFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidatePresetRejectsBadInput(t *testing.T) {
	good := func() Preset {
		return Preset{Name: "x", Chain: []ChainItem{{Type: "peq", Enabled: true, Freq: 1000, GainDB: 1, Q: 1}}}
	}
	cases := []struct {
		name string
		mut  func(*Preset)
		want string
	}{
		{"bad format", func(p *Preset) { p.Format = "something-else" }, "not a native preset format"},
		{"version too new", func(p *Preset) { p.Version = 99 }, "not supported"},
		{"missing name", func(p *Preset) { p.Name = " " }, "missing name"},
		{"typo in type", func(p *Preset) { p.Chain[0].Type = "peakng" }, "unknown"},
		{"missing type", func(p *Preset) { p.Chain[0].Type = "" }, "missing type"},
		{"requires missing entry", func(p *Preset) { p.Requires = []string{"crossfeed"} }, "requires is missing"},
	}
	for _, c := range cases {
		p := good()
		c.mut(&p)
		err := validatePreset(&p)
		if err == nil {
			t.Errorf("%s: should fail", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error should contain %q, got %v", c.name, c.want, err)
		}
	}

	p := good()
	p.Requires = []string{"peq", "crossfeed"}
	p.Chain = append(p.Chain, ChainItem{Type: "crossfeed", Enabled: true})
	if err := validatePreset(&p); err != nil {
		t.Errorf("Planned types should validate (UI is responsible for greying out): %v", err)
	}
	if why := presetUnsupported(p); len(why) == 0 {
		t.Error("Presets containing crossfeed should be flagged as temporarily unsupported")
	}
}

func TestBuiltinLibraryLoadsAndIsUsableAware(t *testing.T) {
	withTempPresetDirs(t)

	presetLibraryPath = filepath.Join("..", "presets", "library.json")
	if _, err := os.Stat(presetLibraryPath); err != nil {
		t.Skipf("no preset library in this repo (release ships no presets): %v", err)
	}
	lib, err := loadPresetLibrary()
	if err != nil {
		t.Fatalf("Built-in library should load: %v", err)
	}
	if len(lib) < 10 {
		t.Fatalf("Built-in library has only %d entries, too few (want >=10)", len(lib))
	}
	byName := map[string]Preset{}
	for _, p := range lib {
		byName[p.Name] = p
		if !p.Builtin {
			t.Errorf("%s should be a built-in preset", p.Name)
		}
		if p.Stage == "" {
			t.Errorf("%s is missing stage (UI relies on it for the stage hint)", p.Name)
		}
	}

	for _, name := range []string{"Heavy Bass", "Clear Vocals", "Classical"} {
		p, ok := byName[name]
		if !ok {
			t.Fatalf("Built-in library is missing %q", name)
		}
		if why := presetUnsupported(p); len(why) != 0 {
			t.Errorf("%s is a P0 preset and should work now, but was judged: %v", name, why)
		}

		if _, _, err := chainToSlots(p.Chain); err != nil {
			t.Errorf("%s chain does not fit current hardware: %v", name, err)
		}
	}

	for _, name := range []string{"Live Venue", "Vinyl", "Grand Stage (Wide Surround)"} {
		p, ok := byName[name]
		if !ok {
			t.Fatalf("Built-in library is missing %q", name)
		}
		why := presetUnsupported(p)
		if len(why) == 0 {
			t.Errorf("%s is a %s planned item and should be flagged as temporarily unsupported", name, p.Stage)
			continue
		}
		if !strings.Contains(strings.Join(why, " "), p.Stage) {
			t.Errorf("%s unavailability reason should mention stage %s: %v", name, p.Stage, why)
		}
	}
}

func TestLibraryRejectsBadFile(t *testing.T) {
	lib, _ := withTempPresetDirs(t)

	os.WriteFile(lib, []byte("{ not json"), 0o644)
	if _, err := loadPresetLibrary(); err == nil {
		t.Error("Bad JSON should fail")
	}

	os.WriteFile(lib, []byte(`{"format":"zybo-audio-preset-library","version":99,"presets":[]}`), 0o644)
	if _, err := loadPresetLibrary(); err == nil {
		t.Error("Unsupported version should fail")
	}

	os.WriteFile(lib, []byte(`{"format":"zybo-audio-preset-library","version":1,"presets":[{"name":"","chain":[]}]}`), 0o644)
	if _, err := loadPresetLibrary(); err == nil {
		t.Error("Library with an illegal preset should fail (naming which entry)")
	}
}

func TestUserPresetRoundTrip(t *testing.T) {
	withTempPresetDirs(t)
	p := Preset{
		Name: "My Bass",
		Note: "My own tuning",
		Chain: []ChainItem{
			{Type: "peq", Enabled: true, Freq: 60, GainDB: 4, Q: 0.9},
			{Type: "peq", Enabled: true, Freq: 150, GainDB: 2, Q: 1.0},
		},
	}
	if err := saveUserPreset(p); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	entries, _ := os.ReadDir(userPresetDir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("Leftover temp file %s (atomic write leaked)", e.Name())
		}
	}
	list, err := loadUserPresets()
	if err != nil || len(list) != 1 {
		t.Fatalf("Read back %d entries (err=%v), want 1", len(list), err)
	}
	if list[0].Name != p.Name || len(list[0].Chain) != 2 {
		t.Errorf("Read-back content mismatch: %+v", list[0])
	}
	if list[0].Builtin {
		t.Error("User presets must not be marked built-in")
	}

	p.Chain = p.Chain[:1]
	if err := saveUserPreset(p); err != nil {
		t.Fatalf("Overwrite save failed: %v", err)
	}
	list, _ = loadUserPresets()
	if len(list) != 1 || len(list[0].Chain) != 1 {
		t.Errorf("After overwrite want exactly 1 entry with 1 section, got %d entries", len(list))
	}

	got, err := findPreset(p.Name)
	if err != nil || got.Name != p.Name {
		t.Errorf("findPreset cannot find user preset: %v", err)
	}

	if err := deleteUserPreset(p.Name); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if list, _ := loadUserPresets(); len(list) != 0 {
		t.Errorf("%d entries remain after delete", len(list))
	}
	if err := deleteUserPreset(p.Name); err == nil {
		t.Error("Repeated delete should fail")
	}
}

func TestUserPresetSkipsCorruptFiles(t *testing.T) {
	withTempPresetDirs(t)
	if err := os.MkdirAll(userPresetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(userPresetDir, "broken.json"), []byte("{oops"), 0o644)
	good := Preset{Name: "Good", Chain: []ChainItem{{Type: "peq", Enabled: true, Freq: 1000, GainDB: 1, Q: 1}}}
	if err := saveUserPreset(good); err != nil {
		t.Fatal(err)
	}
	list, err := loadUserPresets()
	if err != nil {
		t.Fatalf("One bad file must not fail the whole listing: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Good" {
		t.Errorf("Should return only the good one, got %+v", list)
	}
}

func TestRenameUserPreset(t *testing.T) {
	withTempPresetDirs(t)
	if err := os.MkdirAll(userPresetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mk := func(name string) Preset {
		return Preset{Name: name, Chain: []ChainItem{{Type: "peq", Enabled: true, Freq: 1000, GainDB: 2, Q: 1}}}
	}
	for _, n := range []string{"Old Name", "Other"} {
		if err := saveUserPreset(mk(n)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := renameUserPreset("Old Name", "New Name")
	if err != nil {
		t.Fatalf("Rename failed: %v", err)
	}
	if got != "New Name" {
		t.Errorf("Returned new name = %q", got)
	}
	list, _ := loadUserPresets()
	names := map[string]Preset{}
	for _, p := range list {
		names[p.Name] = p
	}
	if _, ok := names["Old Name"]; ok {
		t.Error("Old name still present (old file not removed)")
	}
	p, ok := names["New Name"]
	if !ok {
		t.Fatal("New name was not written")
	}
	if len(p.Chain) != 1 || p.Chain[0].Freq != 1000 {
		t.Errorf("Rename changed chain content: %+v", p.Chain)
	}

	if entries, _ := os.ReadDir(userPresetDir); len(entries) != 2 {
		t.Errorf("Want 2 files in dir after rename, got %d (possible temp file leak)", len(entries))
	}

	if _, err := renameUserPreset("New Name", "New Name"); err != nil {
		t.Errorf("Renaming to the same name should be a no-op, not an error: %v", err)
	}

	if _, err := renameUserPreset("New Name", "   "); err == nil {
		t.Error("Empty name must fail")
	}

	if _, err := renameUserPreset("New Name", "Other"); err == nil {
		t.Error("Colliding with an existing user preset must fail")
	}

	if _, err := renameUserPreset("Ghost", "Whatever"); err == nil {
		t.Error("Nonexistent preset must fail")
	}

	presetLibraryPath = filepath.Join("..", "presets", "library.json")
	if _, err := os.Stat(presetLibraryPath); err != nil {
		t.Skipf("no preset library in this repo (release ships no presets): %v", err)
	}
	lib, err := loadPresetLibrary()
	if err != nil || len(lib) == 0 {
		t.Fatalf("Cannot read built-in library: %v", err)
	}
	if _, err := renameUserPreset(lib[0].Name, "Renaming Built-in"); err == nil {
		t.Error("Built-in presets must refuse rename")
	} else if !strings.Contains(err.Error(), "built-in") {
		t.Errorf("Refusing a built-in rename should state the reason, got: %v", err)
	}

	if _, err := renameUserPreset("New Name", lib[0].Name); err == nil {
		t.Error("Renaming to a built-in name must fail")
	}
}

func TestPresetViewCarriesChain(t *testing.T) {
	withTempPresetDirs(t)
	presetLibraryPath = filepath.Join("..", "presets", "library.json")
	if _, err := os.Stat(presetLibraryPath); err != nil {
		t.Skipf("no preset library in this repo (release ships no presets): %v", err)
	}
	lib, err := loadPresetLibrary()
	if err != nil || len(lib) == 0 {
		t.Fatalf("Cannot read built-in library: %v", err)
	}
	var withChain int
	for _, p := range lib {
		v := presetToView(p)
		if len(p.Chain) > 0 {
			if len(v.Chain) != len(p.Chain) {
				t.Errorf("%s: view holds %d sections, actual %d sections", p.Name, len(v.Chain), len(p.Chain))
			}
			withChain++
		}
		if v.Bands != len(p.Chain) {
			t.Errorf("%s: bands=%d disagrees with chain length %d", p.Name, v.Bands, len(p.Chain))
		}
	}
	if withChain < 10 {
		t.Errorf("Only %d presets carry chains, too few", withChain)
	}

	v := presetToView(lib[1])
	if len(v.Chain) > 0 {
		it := v.Chain[0]
		if it.Freq <= 0 {
			t.Errorf("Chain freq point missing from view: %+v", it)
		}
	}
}

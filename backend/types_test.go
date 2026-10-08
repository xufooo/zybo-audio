// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withTempTypeDir(t *testing.T) string {
	t.Helper()
	old := userTypeDir
	dir := filepath.Join(t.TempDir(), "types")
	userTypeDir = dir
	t.Cleanup(func() { userTypeDir = old })
	return dir
}

func TestBuiltinTypesShape(t *testing.T) {
	withTempTypeDir(t)
	list, err := loadTypes()
	if err != nil {
		t.Fatalf("loadTypes failed: %v", err)
	}
	byName := map[string]SoundType{}
	for _, ty := range list {
		byName[ty.Name] = ty
	}
	for _, want := range []string{"Headphones", "Small Speakers", "Amplifier"} {
		if _, ok := byName[want]; !ok {
			t.Errorf("missing built-in type %q (actually have %v)", want, keysOf(byName))
		}
	}

	gong := byName["Amplifier"]
	if len(gong.Effects) == 0 {
		t.Error("Amplifier card group is empty")
	}
	for _, e := range gong.Effects {
		if e.Enabled && e.ID != "limiter" {
			t.Errorf("Amplifier should not enabled by default coloration effects, but %q is open", e.ID)
		}
	}

	var bands int
	for _, e := range byName["Headphones"].Effects {
		if e.Enabled {
			bands += len(e.Bands)
		}
	}
	if bands == 0 {
		t.Error("Headphones type default not enabled any what with section effect")
	}

	for _, ty := range list {
		if !ty.Builtin {
			t.Errorf("built-in type %q not marked builtin", ty.Name)
		}
	}
}

func keysOf(m map[string]SoundType) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestTypeRoundTripAndUserOverridesBuiltin(t *testing.T) {
	dir := withTempTypeDir(t)
	custom := SoundType{
		Name: "Headphones",
		Note: "my modified Headphones",
		Effects: []TypeEffect{
			{ID: "bass", Enabled: true, Mode: "strong", Bands: []ChainItem{
				{Type: "peq", Enabled: true, Freq: 80, GainDB: 6, Q: 1.0},
			}},
		},
	}
	if err := saveUserType(custom); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Headphones.json")); err != nil {
		t.Errorf("not written to %s: %v", dir, err)
	}
	got, err := findType("Headphones")
	if err != nil {
		t.Fatalf("findType failed: %v", err)
	}
	if got.Builtin {
		t.Error("same-name user-saved type should be a user type (builtin=false)")
	}
	if got.Note != "my modified Headphones" || len(got.Effects) != 1 {
		t.Errorf("read back content not correct: %+v", got)
	}

	list, _ := loadTypes()
	names := map[string]bool{}
	for _, ty := range list {
		names[ty.Name] = true
	}
	for _, want := range []string{"Headphones", "Small Speakers", "Amplifier"} {
		if !names[want] {
			t.Errorf("loadTypes is missing %q", want)
		}
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("left temp files %s (atomic write left debris)", e.Name())
		}
	}
}

func TestValidateTypeRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		typ  SoundType
		want string
	}{
		{"empty name", SoundType{Name: "  ", Effects: []TypeEffect{}}, "name"},
		{"wrong format", SoundType{Name: "x", Format: "other", Effects: []TypeEffect{}}, "format"},
		{"wrong version", SoundType{Name: "x", Version: 99, Effects: []TypeEffect{}}, "version"},
		{"effects missing", SoundType{Name: "x"}, "effects"},
		{"card missing id", SoundType{Name: "x", Effects: []TypeEffect{{Enabled: true}}}, "id"},
		{"section type unrecognized", SoundType{Name: "x", Effects: []TypeEffect{
			{ID: "bass", Bands: []ChainItem{{Type: "reverb", Enabled: true}}},
		}}, "reverb"},
	}
	for _, c := range cases {
		err := validateType(&c.typ)
		if err == nil {
			t.Errorf("%s: should error", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error should contain %q, actual %v", c.name, c.want, err)
		}
	}

	ok := SoundType{Name: "x", Effects: []TypeEffect{}}
	if err := validateType(&ok); err != nil {
		t.Fatalf("valid type should not error: %v", err)
	}
	if ok.Format != typeFormat || ok.Version != typeFormatVer {
		t.Errorf("not filled in format/version: %+v", ok)
	}
}

func TestDeleteAndRenameRules(t *testing.T) {
	withTempTypeDir(t)
	if err := os.MkdirAll(userTypeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mk := func(n string) SoundType {
		return SoundType{Name: n, Effects: []TypeEffect{{ID: "bass", Enabled: true}}}
	}
	if err := saveUserType(mk("my type")); err != nil {
		t.Fatal(err)
	}

	if err := deleteUserType("Amplifier"); err == nil {
		t.Error("built-in type should not be deletable")
	}
	if _, err := renameUserType("Amplifier", "Amplifier 2"); err == nil {
		t.Error("built-in type should not be renamable")
	}

	if _, err := renameUserType("my type", "renamed type"); err != nil {
		t.Fatalf("rename failed: %v", err)
	}
	got, err := findType("renamed type")
	if err != nil || len(got.Effects) != 1 {
		t.Errorf("renamed content wrong: %v %+v", err, got)
	}
	if _, err := findType("my type"); err == nil {
		t.Error("old name is still found (old file not deleted)")
	}

	if _, err := renameUserType("renamed type", "Small Speakers"); err == nil {
		t.Error("renaming to a built-in name should error")
	}
	if _, err := renameUserType("renamed type", "  "); err == nil {
		t.Error("empty name should error")
	}
	if _, err := renameUserType("nonexistent", "x"); err == nil {
		t.Error("nonexistent type should error")
	}

	if err := deleteUserType("renamed type"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	list, _ := loadUserTypes()
	if len(list) != 0 {
		t.Errorf("left after deletion %d", len(list))
	}
}

func TestTypeStoreKeepsArbitraryEffectParams(t *testing.T) {
	withTempTypeDir(t)
	ty := SoundType{
		Name: "future effect",
		Effects: []TypeEffect{
			{ID: "compressor", Enabled: true, Mode: "warm", Params: map[string]float64{
				"threshold_db": -18.5, "ratio": 3.2, "attack_ms": 12, "release_ms": 250,
			}},
			{ID: "crossfeed", Enabled: true, Params: map[string]float64{"level": 0.35, "delay_us": 300}},
		},
	}
	if err := saveUserType(ty); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	got, err := findType("future effect")
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	if len(got.Effects) != 2 {
		t.Fatalf("wrong card count: %d", len(got.Effects))
	}
	e := got.Effects[0]
	if e.ID != "compressor" || e.Mode != "warm" {
		t.Errorf("id/mode lost: %+v", e)
	}
	for k, want := range map[string]float64{"threshold_db": -18.5, "ratio": 3.2, "attack_ms": 12, "release_ms": 250} {
		if e.Params[k] != want {
			t.Errorf("parameter %s = %v, expect %v", k, e.Params[k], want)
		}
	}
}

func TestUserCardKeepsItsOwnNameAndKind(t *testing.T) {
	dir := t.TempDir()
	old := userTypeDir
	userTypeDir = dir
	t.Cleanup(func() { userTypeDir = old })

	in := SoundType{
		Name: "custom card test", Note: "test",
		Effects: []TypeEffect{
			{ID: "u_abc", Name: "my bass card", Kind: "bands", Enabled: true,
				Bands: []ChainItem{{Type: "peq", Enabled: true, Freq: 60, GainDB: 3, Q: 0.7}}},
			{ID: "limiter", Name: "limiter protection", Kind: "limiter", Enabled: true, Mode: "truepeak",
				Params: map[string]float64{"thr_db": 0}},
		},
	}
	if err := saveUserType(in); err != nil {
		t.Fatalf("store user type failed: %v", err)
	}
	got, err := loadUserTypes()
	if err != nil {
		t.Fatalf("read user type failed: %v", err)
	}
	var found *TypeEffect
	for i := range got {
		if got[i].Name != "custom card test" {
			continue
		}
		for j := range got[i].Effects {
			if got[i].Effects[j].ID == "u_abc" {
				found = &got[i].Effects[j]
			}
		}
	}
	if found == nil {
		t.Fatal("read back but cannot find that custom card")
	}
	if found.Name != "my bass card" {
		t.Errorf("card name lost: %q", found.Name)
	}
	if found.Kind != "bands" {
		t.Errorf("card kind lost: %q (losing it stops being treated as EQ card, section cannot enter the chain)", found.Kind)
	}
	if len(found.Bands) != 1 {
		t.Errorf("card sections lost too: %d", len(found.Bands))
	}
}

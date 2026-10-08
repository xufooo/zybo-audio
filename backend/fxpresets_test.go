// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestFXPresetAnalogXMatchesSource(t *testing.T) {

	wantHarm := [10]float64{0.01, 0.02, 0.0001, 0.001, 0, 0, 0, 0, 0, 0}

	want := [3]struct {
		gain, lpf float64
	}{
		{0.6, 19650.0},
		{1.2, 18233.0},
		{2.4, 16307.0},
	}

	wantCn := [3]string{"Slight", "Moderate", "Extreme"}

	wantEn := [3]string{"slight", "moderate", "extreme"}

	tbl := fxPresetTable()
	for i := 0; i < 3; i++ {
		p := tbl[i]
		if p.Name != "analogx-"+wantEn[i] {
			t.Errorf("preset group %d should be named analogx-%s, got %q (arrays.xml:266-270)",
				i, wantEn[i], p.Name)
		}
		if p.Cn != wantCn[i] {
			t.Errorf("%s Cn should be %q, got %q (values-zh-rCN/arrays.xml:103-107)",
				p.Name, wantCn[i], p.Cn)
		}
		if p.FX != "analogx" || p.Model != i {
			t.Errorf("%s should map to analogx model %d, got fx=%q model=%d", p.Name, i, p.FX, p.Model)
		}
		if p.WetGain != want[i].gain {
			t.Errorf("%s gain should be %g (AnalogX.cpp:67/77/87), got %g",
				p.Name, want[i].gain, p.WetGain)
		}
		if p.LPFHz != want[i].lpf {
			t.Errorf("%s LPF should be %g Hz (AnalogX.cpp:69/79/89), got %g",
				p.Name, want[i].lpf, p.LPFHz)
		}

		if p.HPFHz != 240.0 {
			t.Errorf("%s pre highpass should be 240 Hz (AnalogX.cpp:53-54), got %g", p.Name, p.HPFHz)
		}
		if p.Harmonics != wantHarm {
			t.Errorf("%s harmonic table should be %v (AnalogX.cpp:5-16), got %v", p.Name, wantHarm, p.Harmonics)
		}
		if p.Source == "" {
			t.Errorf("%s missing primary source (file:line)", p.Name)
		}
	}
}

func TestFXPresetVSEMatchesSource(t *testing.T) {

	var wantHarm [10]float64
	for i := 0; i < 10; i += 2 {
		wantHarm[i] = 0.02
	}

	p, err := fxPresetByName("vse")
	if err != nil {
		t.Fatal(err)
	}
	if p.FX != "vse" {
		t.Errorf("vse preset should map to vse, got %q", p.FX)
	}

	if p.HPFHz != 7600 {
		t.Errorf("VSE pre highpass should be 7600 Hz (SpectrumExtend.cpp:19 + :44-47), got %g", p.HPFHz)
	}

	if want := sampleRate/2 - 2000; p.LPFHz != want {
		t.Errorf("VSE post lowpass should be fs/2-2000 = %g Hz (SpectrumExtend.cpp:49-52), got %g", want, p.LPFHz)
	}
	if p.Harmonics != wantHarm {
		t.Errorf("VSE harmonic table should be 0.02 on each odd harmonic (SpectrumExtend.cpp:4-15), got %v", p.Harmonics)
	}

	if p.Gear != 0.1 {
		t.Errorf("VSE preset panel level should be default level 0.1, got %g", p.Gear)
	}
	if p.Reconstruct != 56 {
		t.Errorf("VSE default level core integer should be round(0.1x5.6x100) = 56, got %d", p.Reconstruct)
	}
	if math.Abs(p.WetGain-0.56) > 1e-12 {
		t.Errorf("VSE default level core wet should be 0.56, got %g", p.WetGain)
	}
}

func TestFXPresetTableSharesSingleSourceOfTruth(t *testing.T) {
	tbl := fxPresetTable()
	if len(tbl) != len(fxPresetNames) {
		t.Fatalf("preset table should have %d groups, got %d", len(fxPresetNames), len(tbl))
	}
	for i := range analogxTiers {
		if tbl[i].WetGain != analogxTiers[i].Gain {
			t.Errorf("%s gain drifted from analogxTiers[%d]: %g vs %g",
				tbl[i].Name, i, tbl[i].WetGain, analogxTiers[i].Gain)
		}
		if tbl[i].LPFHz != analogxTiers[i].LPHz {
			t.Errorf("%s LPF drifted from analogxTiers[%d]: %g vs %g",
				tbl[i].Name, i, tbl[i].LPFHz, analogxTiers[i].LPHz)
		}
		if tbl[i].Harmonics != analogxHarmonics {
			t.Errorf("%s harmonic table drifted from analogxHarmonics", tbl[i].Name)
		}
	}
	want, err := vseExciterParams(vseGearMin)
	if err != nil {
		t.Fatal(err)
	}
	vse := tbl[3]
	if vse.Harmonics != want.Harmonics || vse.HPFHz != want.HPFHz ||
		vse.LPFHz != want.LPFHz || math.Abs(vse.WetGain-want.Mix) > 1e-12 {
		t.Errorf("vse preset drifted from vseExciterParams(%g): %+v vs %+v", vseGearMin, vse, want)
	}

	for i, n := range fxPresetNames {
		if tbl[i].Name != n {
			t.Errorf("group %d name should be %q, got %q", i, n, tbl[i].Name)
		}
	}

	tiers := analogxTiersView()
	if len(tiers) != len(analogxTiers) {
		t.Fatalf("analogxTiersView should have %d levels, got %d", len(analogxTiers), len(tiers))
	}
	for i, tv := range tiers {
		if tv["gain"] != analogxTiers[i].Gain || tv["lowpass_hz"] != analogxTiers[i].LPHz {
			t.Errorf("caps level %d drifted from analogxTiers: %v", i, tv)
		}
		if tv["preset"] != tbl[i].Name {
			t.Errorf("caps level %d should point at preset %q, got %v", i, tbl[i].Name, tv["preset"])
		}
	}
}

func TestFXPresetRejectsUnknownName(t *testing.T) {
	for _, name := range []string{"", "analogx", "analogx-medium", "vse-0.5", "Slight", "analogx-slight "} {
		if _, err := fxPresetByName(name); err == nil {
			t.Errorf("preset name %q is not one of the four groups, must fail", name)
		}
		if err := applyFXPreset(name); err == nil {
			t.Errorf("applyFXPreset(%q) must fail", name)
		}
	}

	if got := fxPresetCurrent(); len(got) != 0 {
		t.Errorf("with no effect running, no preset should match, got %v", got)
	}
}

func TestFXPresetRejectsWhenHardwareLacksPOLY(t *testing.T) {
	savedGen, savedAX, savedExc := dspEngineGen, currentAnalogX, currentExciter
	savedAvail := dspAvailable
	t.Cleanup(func() {
		dspEngineGen, currentAnalogX, currentExciter = savedGen, savedAX, savedExc
		dspAvailable = savedAvail
	})
	dspEngineGen, dspAvailable = 1, false
	currentAnalogX, currentExciter = nil, nil

	for _, name := range fxPresetNames {
		err := applyFXPreset(name)
		if err == nil {
			t.Fatalf("without a POLY slot, applyFXPreset(%q) must fail, must not succeed silently", name)
		}
		if !strings.Contains(err.Error(), "POLY") {
			t.Errorf("applyFXPreset(%q) error should say the POLY slot is missing, got: %v", name, err)
		}
		if currentAnalogX != nil || currentExciter != nil {
			t.Fatalf("rejected applyFXPreset(%q) must not change in-memory state (analogx=%v exc=%v)",
				name, currentAnalogX, currentExciter)
		}
	}
}

func TestFXPresetCurrentReverseLookup(t *testing.T) {
	savedAX, savedExc := currentAnalogX, currentExciter
	t.Cleanup(func() { currentAnalogX, currentExciter = savedAX, savedExc })

	for i := range analogxTiers {
		p := analogxParams{Model: i}
		currentAnalogX, currentExciter = &p, nil
		got := fxPresetCurrent()
		want := "analogx-" + fxAnalogXPresetIDSuffix[i]
		if len(got) != 1 || got[0] != want {
			t.Errorf("model %d should reverse-lookup to [%s], got %v", i, want, got)
		}
	}

	ex := exciterDefaultParams()
	currentAnalogX, currentExciter = nil, &ex
	if got := fxPresetCurrent(); len(got) != 1 || got[0] != "vse" {
		t.Errorf("VSE default level should reverse-lookup to [vse], got %v", got)
	}

	ex.Harmonics[1] = 0.5
	if got := fxPresetCurrent(); len(got) != 0 {
		t.Errorf("custom harmonic shape must not match a preset, got %v", got)
	}

	g := 0.5
	p, err := vseExciterParams(g)
	if err != nil {
		t.Fatal(err)
	}
	currentExciter = &p
	if got := fxPresetCurrent(); len(got) != 0 {
		t.Errorf("VSE level %g is not one of the four preset groups, must not match, got %v", g, got)
	}

	ax := analogxParams{Model: 1}
	currentAnalogX, currentExciter = &ax, &ex
	ex = exciterDefaultParams()
	currentExciter = &ex
	got := fxPresetCurrent()
	if len(got) != 2 || got[0] != "analogx-moderate" || got[1] != "vse" {
		t.Errorf("when both cards match, should report [analogx-moderate vse], got %v", got)
	}
}

func TestFXPresetVSEPassesExciterValidation(t *testing.T) {
	p, err := fxPresetByName("vse")
	if err != nil {
		t.Fatal(err)
	}
	ex := exciterParams{Harmonics: p.Harmonics, Mix: p.WetGain, HPFHz: p.HPFHz, LPFHz: p.LPFHz}
	if _, err := harmonicQ315(ex); err != nil {
		t.Errorf("VSE preset harmonic table fails fixed-point expansion: %v", err)
	}
	if err := validateExciterMix(ex); err != nil {
		t.Errorf("VSE default level wet %g should not be rejected: %v", ex.Mix, err)
	}
	if _, err := exciterNodes(ex); err != nil {
		t.Errorf("VSE preset fails to compile into slot nodes: %v", err)
	}

	for i := range analogxTiers {
		if _, err := analogxNodes(analogxParams{Model: i}, sampleRate); err != nil {
			t.Errorf("AnalogX level %d fails to compile into slot nodes: %v", i, err)
		}
	}
}

const (
	refsAnalogXCPP        = "../../../refs/viperfx-re/src/viper/effects/AnalogX.cpp"
	refsSpectrumExtendCPP = "../../../refs/viperfx-re/src/viper/effects/SpectrumExtend.cpp"
)

func readRefsSource(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Skipf("reference source missing (%s): %v -- readable on dev machines; skipping only covers "+
			"the 'direct source read' recheck layer", path, err)
	}
	return string(b)
}

func TestFXPresetAnalogXSourceTextMatches(t *testing.T) {
	src := readRefsSource(t, refsAnalogXCPP)

	for _, lit := range []string{"0.01", "0.02", "0.0001", "0.001"} {
		if !strings.Contains(src, lit) {
			t.Errorf("AnalogX.cpp has no harmonic-table literal %q (expected in the :5-16 table)", lit)
		}
	}

	if !strings.Contains(src, "240.0, this->samplingRate, 0.717") {
		t.Error("AnalogX.cpp has no HP 240 Hz / Q0.717 (:53-54)")
	}

	re := regexp.MustCompile(`case (\d): \{[\s\S]*?this->gain = ([\d.]+);[\s\S]*?LOW_PASS, 0\.0, ([\d.]+),`)
	m := re.FindAllStringSubmatch(src, -1)
	if len(m) != 3 {
		t.Fatalf("extracted %d levels from AnalogX.cpp (want 3 levels) -- source layout changed, regex must follow", len(m))
	}
	tbl := fxPresetTable()
	for _, g := range m {
		idx, err := strconv.Atoi(g[1])
		if err != nil {
			t.Fatal(err)
		}
		gain, err := strconv.ParseFloat(g[2], 64)
		if err != nil {
			t.Fatal(err)
		}
		lpf, err := strconv.ParseFloat(g[3], 64)
		if err != nil {
			t.Fatal(err)
		}
		if idx < 0 || idx >= 3 {
			t.Fatalf("source level index %d out of range", idx)
		}
		if tbl[idx].WetGain != gain || tbl[idx].LPFHz != lpf {
			t.Errorf("AnalogX.cpp case %d has gain=%g / LPF=%g, preset table has gain=%g / LPF=%g",
				idx, gain, lpf, tbl[idx].WetGain, tbl[idx].LPFHz)
		}
	}
}

func TestFXPresetVSESourceTextMatches(t *testing.T) {
	src := readRefsSource(t, refsSpectrumExtendCPP)

	odd := strings.Count(src, "0.02f,")
	if odd != 5 {
		t.Errorf("SpectrumExtend.cpp harmonic table should hold five 0.02f (odd harmonics), got %d -- see :4-15", odd)
	}

	if !strings.Contains(src, "referenceFreq = 7600") {
		t.Error("SpectrumExtend.cpp has no `referenceFreq = 7600` (:19)")
	}

	if !strings.Contains(src, "this->samplingRate / 2.0f - 2000.0f") {
		t.Error("SpectrumExtend.cpp has no `samplingRate / 2.0f - 2000.0f` (:49-52)")
	}
}

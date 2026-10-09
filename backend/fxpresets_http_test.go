// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func fakeHardware(t *testing.T, opcodes uint32) {
	t.Helper()
	savedMem, savedAvail, savedGen := dspMem, dspAvailable, dspEngineGen
	t.Cleanup(func() { dspMem, dspAvailable, dspEngineGen = savedMem, savedAvail, savedGen })
	dspMem = make([]byte, dspMapSize)
	dspAvailable = true
	dspEngineGen = 1

	regWrite(regCap0, uint32(capMagic)<<16|1)
	regWrite(regCap1, opcodes)
}

func chainPut(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/dsp/chain", strings.NewReader(body))
	handleDSPChain(rec, req)
	return rec
}

func TestFXPresetsAndCrossfeedTiersInCapabilities(t *testing.T) {
	rec := httptest.NewRecorder()
	handleDSPCapabilities(rec, httptest.NewRequest(http.MethodGet, "/api/dsp/capabilities", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("capabilities should be 200, got %d", rec.Code)
	}
	var caps struct {
		FXPresets struct {
			Presets []struct {
				Name      string      `json:"name"`
				Cn        string      `json:"cn"`
				FX        string      `json:"fx"`
				Harmonics [10]float64 `json:"harmonics"`
				Source    string      `json:"source"`
			} `json:"presets"`
			On []string `json:"on"`
		} `json:"fx_presets"`
		Crossfeed struct {
			Tiers struct {
				Tiers []map[string]any `json:"tiers"`
			} `json:"tiers"`
		} `json:"crossfeed"`
		AnalogX struct {
			Tiers []map[string]any `json:"tiers"`
		} `json:"analogx"`
		Clarity struct {
			Modes       []string `json:"modes"`
			Unsupported []string `json:"unsupported"`
		} `json:"clarity"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatalf("capabilities response is not valid JSON: %v", err)
	}

	if len(caps.FXPresets.Presets) != len(fxPresetNames) {
		t.Fatalf("caps fx_presets should hold %d groups, got %d", len(fxPresetNames), len(caps.FXPresets.Presets))
	}
	for i, n := range fxPresetNames {
		p := caps.FXPresets.Presets[i]
		if p.Name != n {
			t.Errorf("Preset group %d name should be %q, got %q", i, n, p.Name)
		}
		if p.Cn == "" || p.Source == "" {
			t.Errorf("%s caps entry lacks a display name or primary source: %+v", n, p)
		}
	}

	if len(caps.Crossfeed.Tiers.Tiers) != len(crossfeedTiers) {
		t.Fatalf("caps crossfeed.tiers should hold %d tiers, got %d",
			len(crossfeedTiers), len(caps.Crossfeed.Tiers.Tiers))
	}
	for i, row := range caps.Crossfeed.Tiers.Tiers {
		if row["name_cn"] != crossfeedTiers[i].Cn {
			t.Errorf("crossfeed tier %d display name should be %q, got %v", i, crossfeedTiers[i].Cn, row["name_cn"])
		}
	}

	if len(caps.AnalogX.Tiers) != len(analogxTiers) {
		t.Fatalf("caps analogx.tiers should hold %d tiers, got %d", len(analogxTiers), len(caps.AnalogX.Tiers))
	}
	for i, row := range caps.AnalogX.Tiers {
		if row["preset"] != "analogx-"+fxAnalogXPresetIDSuffix[i] {
			t.Errorf("analogx tier %d should point at preset %q, got %v",
				i, "analogx-"+fxAnalogXPresetIDSuffix[i], row["preset"])
		}
	}

	has := func(list []string, want string) bool {
		for _, v := range list {
			if v == want {
				return true
			}
		}
		return false
	}
	if !has(caps.Clarity.Modes, "xhifi") {
		t.Errorf("caps clarity.modes should include xhifi (implemented and used by the baseline), got %v", caps.Clarity.Modes)
	}
	if has(caps.Clarity.Unsupported, "xhifi") {
		t.Error("caps clarity.unsupported must not list xhifi anymore -- it diverged from the implementation")
	}
}

func TestFXPresetApplyInstallsRightState(t *testing.T) {
	fakeHardware(t, capOpcodePoly)
	withStateFile(t)
	savedAX, savedExc := currentAnalogX, currentExciter
	t.Cleanup(func() { currentAnalogX, currentExciter = savedAX, savedExc })
	currentAnalogX, currentExciter = nil, nil

	for i := range analogxTiers {
		name := "analogx-" + fxAnalogXPresetIDSuffix[i]
		if err := applyFXPreset(name); err != nil {
			t.Fatalf("applyFXPreset(%q): %v", name, err)
		}
		if currentAnalogX == nil || currentAnalogX.Model != i {
			t.Fatalf("%s should set AnalogX to model %d, got %+v", name, i, currentAnalogX)
		}
		if got := fxPresetCurrent(); len(got) != 1 || got[0] != name {
			t.Errorf("%s should reverse-lookup to [%s] after install, got %v", name, name, got)
		}
	}
	if err := applyFXPreset("vse"); err != nil {
		t.Fatalf("applyFXPreset(vse): %v", err)
	}
	if currentExciter == nil {
		t.Fatal("vse preset should install the harmonic exciter")
	}
	def := exciterDefaultParams()
	if *currentExciter != def {
		t.Errorf("vse preset should land on the VSE default tier (= factory shape):\ngot %+v\nwant %+v", *currentExciter, def)
	}
	if got := fxPresetCurrent(); len(got) != 2 || got[0] != "analogx-extreme" || got[1] != "vse" {
		t.Errorf("With both cards running, reverse lookup should yield two presets, got %v", got)
	}
}

func TestFXPresetPersistsThroughExistingFields(t *testing.T) {
	fakeHardware(t, capOpcodePoly)
	withStateFile(t)
	savedAX, savedExc := currentAnalogX, currentExciter
	t.Cleanup(func() { currentAnalogX, currentExciter = savedAX, savedExc })
	currentAnalogX, currentExciter = nil, nil

	if err := applyFXPreset("analogx-moderate"); err != nil {
		t.Fatal(err)
	}
	if err := applyFXPreset("vse"); err != nil {
		t.Fatal(err)
	}
	st := snapshotRuntimeState()
	if st.AnalogX == nil || st.AnalogX.Model != 1 {
		t.Errorf("On-disk snapshot analogx field should be model 1, got %+v", st.AnalogX)
	}
	if st.Exciter == nil {
		t.Error("On-disk snapshot exciter field should hold the VSE preset (VSE takes no new field, see vse.go)")
	}

	if err := saveRuntimeState(); err != nil {
		t.Fatal(err)
	}
	currentAnalogX, currentExciter = nil, nil
	st2, ok, err := loadRuntimeState()
	if err != nil || !ok {
		t.Fatalf("Read-back failed: ok=%v err=%v", ok, err)
	}
	applyRuntimeState(st2, true, true, true, true, true)
	if currentAnalogX == nil || currentAnalogX.Model != 1 {
		t.Errorf("After reboot-and-replay AnalogX should still be model 1, got %+v", currentAnalogX)
	}
	if got := fxPresetCurrent(); len(got) != 2 {
		t.Errorf("After reboot-and-replay reverse lookup should still yield two presets, got %v", got)
	}
}

func TestFXPresetHTTPField(t *testing.T) {
	fakeHardware(t, capOpcodePoly)
	withStateFile(t)
	savedAX, savedExc := currentAnalogX, currentExciter
	t.Cleanup(func() { currentAnalogX, currentExciter = savedAX, savedExc })
	currentAnalogX, currentExciter = nil, nil

	rec := chainPut(t, `{"chain":[],"fx_preset":"analogx-slight"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT fx_preset=analogx-slight should be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if currentAnalogX == nil || currentAnalogX.Model != 0 {
		t.Fatalf("After PUT AnalogX should be model 0, got %+v", currentAnalogX)
	}

	recGet := httptest.NewRecorder()
	handleDSPChain(recGet, httptest.NewRequest(http.MethodGet, "/api/dsp/chain", nil))
	var got struct {
		FXPreset []string `json:"fx_preset"`
	}
	if err := json.Unmarshal(recGet.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET response is not valid JSON: %v", err)
	}
	if len(got.FXPreset) != 1 || got.FXPreset[0] != "analogx-slight" {
		t.Errorf("GET fx_preset should be [analogx-slight], got %v", got.FXPreset)
	}

	before := *currentAnalogX
	recBad := chainPut(t, `{"chain":[],"fx_preset":"analogx-turbo"}`)
	if recBad.Code != http.StatusBadRequest {
		t.Errorf("Unknown preset name should be 400, got %d", recBad.Code)
	}
	if currentAnalogX == nil || *currentAnalogX != before {
		t.Errorf("Rejected requests must not change AnalogX state: %+v", currentAnalogX)
	}

	model := 2
	b, err := json.Marshal(map[string]any{
		"chain": []any{}, "fx_preset": "analogx-slight",
		"analogx": map[string]any{"model": model},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec := chainPut(t, string(b)); rec.Code != http.StatusOK {
		t.Fatalf("Preset + explicit fields should be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if currentAnalogX == nil || currentAnalogX.Model != model {
		t.Errorf("Explicit analogx.model should override the preset, got %+v", currentAnalogX)
	}
}

func TestFXPresetHTTPRejectsWithoutPOLY(t *testing.T) {
	fakeHardware(t, 0)
	withStateFile(t)
	savedAX, savedExc := currentAnalogX, currentExciter
	t.Cleanup(func() { currentAnalogX, currentExciter = savedAX, savedExc })
	currentAnalogX, currentExciter = nil, nil

	for _, name := range []string{"analogx-extreme", "vse"} {
		rec := chainPut(t, `{"chain":[],"fx_preset":"`+name+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("Without a POLY slot fx_preset=%s should be 400, got %d", name, rec.Code)
		}
		if currentAnalogX != nil || currentExciter != nil {
			t.Fatalf("Rejected fx_preset=%s must not change state", name)
		}
	}
}

var snapshotExempt = map[string]string{}

var snapshotFieldToGlobal = map[string]string{
	"dyn":     "currentDynBass",
	"dynbass": "currentDynamicBass",
	"cross":   "currentCrossfeed",
	"sur":     "currentSurround",
	"exc":     "currentExciter",
	"bass":    "currentViPERBass",
	"tube":    "currentTube",
	"col":     "currentColorful",
	"clarity": "currentClarity",
	"spk":     "currentSpeakerCorrection",
	"analogx": "currentAnalogX",
	"fir":     "currentFIR",
}

func funcBody(src, header string) string {
	i := strings.Index(src, header)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest[1:], "\nfunc "); j >= 0 {
		return rest[:j+1]
	}
	return rest
}

func TestChainSnapshotCoversEveryRuntimeEffect(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^var (current\w+) \*(\w+)`)
	declared := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			declared[m[1]] = f
		}
	}
	if len(declared) < 10 {
		t.Fatalf("Scanned only %d current* effect globals (want 10+) -- regex or file layout changed", len(declared))
	}

	chainSrc, err := os.ReadFile("chain.go")
	if err != nil {
		t.Fatal(err)
	}
	captureBody := funcBody(string(chainSrc), "func captureChainState()")
	restoreBody := funcBody(string(chainSrc), "func (s chainStateSnapshot) restore()")
	if captureBody == "" || restoreBody == "" {
		t.Fatal("captureChainState / restore not found in chain.go -- signatures changed, this guard must follow")
	}

	for name, file := range declared {
		if why, ok := snapshotExempt[name]; ok {
			t.Logf("Exempt: %s (%s) -- %s", name, file, why)
			continue
		}
		field := ""
		for f, g := range snapshotFieldToGlobal {
			if g == name {
				field = f
				break
			}
		}
		if field == "" {
			t.Errorf("Runtime effect global %s (%s) is not registered in snapshotFieldToGlobal -- "+
				"a rejected PUT would leave it in memory (hit once each on 2026-09-20 and 2026-09-22)."+
				"Register it in all three places in chain.go (chainStateSnapshot / captureChainState / restore),"+
				"add the mapping in this file, or state the exemption reason in snapshotExempt", name, file)
			continue
		}
		if !strings.Contains(captureBody, name) {
			t.Errorf("%s does not appear in captureChainState (snapshot misses it)", name)
		}
		if !strings.Contains(restoreBody, name) {
			t.Errorf("%s does not appear in restore (rollback cannot put it back)", name)
		}
	}

	rt := reflect.TypeOf(chainStateSnapshot{})
	ptrFields := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Type.Kind() == reflect.Ptr {
			ptrFields[f.Name] = true
		}
	}
	for f := range ptrFields {
		if _, ok := snapshotFieldToGlobal[f]; !ok {
			t.Errorf("Pointer field %q in chainStateSnapshot is not in snapshotFieldToGlobal -- "+
				"either the name is misspelled or a mapping is missing", f)
		}
	}
	for f, g := range snapshotFieldToGlobal {
		if !ptrFields[f] {
			t.Errorf("Mapped field %q (-> %s) does not exist in chainStateSnapshot or is a non-pointer type", f, g)
		}
	}
}

func TestChainSnapshotRollsBackClaritySpeakerAnalogX(t *testing.T) {
	savedClarity, savedSpk, savedAX := currentClarity, currentSpeakerCorrection, currentAnalogX
	t.Cleanup(func() {
		currentClarity, currentSpeakerCorrection, currentAnalogX = savedClarity, savedSpk, savedAX
	})

	clr := clarityParams{Mode: clarityModeOzone, Level: 50}
	spk := speakerCorrectionParams{}
	ax := analogxParams{Model: 0}
	currentClarity, currentSpeakerCorrection, currentAnalogX = &clr, &spk, &ax

	snap := captureChainState()

	newClr := clarityParams{Mode: clarityModeXHIFI, Level: 90}
	newAX := analogxParams{Model: 2}
	currentClarity, currentSpeakerCorrection, currentAnalogX = &newClr, nil, &newAX

	snap.restore()
	if currentClarity == nil || *currentClarity != clr {
		t.Errorf("Clarity was not rolled back: %+v (want %+v)", currentClarity, clr)
	}
	if currentSpeakerCorrection == nil {
		t.Error("SpeakerCorrection was not rolled back (changed to nil)")
	}
	if currentAnalogX == nil || *currentAnalogX != ax {
		t.Errorf("AnalogX was not rolled back: %+v (want %+v)", currentAnalogX, ax)
	}
}

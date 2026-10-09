// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRestoreFailureKeepsResourceRefs(t *testing.T) {
	fakeHardware(t, 0xFFFFFFFF)
	p := withStateFile(t)
	resetRuntimeGlobals()

	svExciter, svBass, svClar, svDyn := currentExciter, currentViPERBass, currentClarity, currentDynBass
	svDynB, svCross, svSurr, svTube := currentDynamicBass, currentCrossfeed, currentSurround, currentTube
	svAx, svSpk, svFIR := currentAnalogX, currentSpeakerCorrection, currentFIR
	svChain, svVol := currentUserChain, currentVolume
	svDDC := append([][5]int32(nil), ddcSections...)
	svDir := ddcDir
	svPlan := append([]int32(nil), lastPlanCoefs...)
	t.Cleanup(func() {
		resetRuntimeGlobals()
		currentExciter, currentViPERBass, currentClarity, currentDynBass = svExciter, svBass, svClar, svDyn
		currentDynamicBass, currentCrossfeed, currentSurround, currentTube = svDynB, svCross, svSurr, svTube
		currentAnalogX, currentSpeakerCorrection, currentFIR = svAx, svSpk, svFIR
		currentUserChain, currentVolume = svChain, svVol
		ddcMu.Lock()
		ddcSections = svDDC
		ddcMu.Unlock()
		ddcDir = svDir
		lastPlanCoefs = svPlan
	})

	vp, err := vseExciterParams(0.6)
	if err != nil {
		t.Fatal(err)
	}
	currentExciter = &vp
	vb := viperBassParams{Mode: 0, CutoffHz: 57, Gain: 150}
	currentViPERBass = &vb
	cl := clarityParams{Mode: clarityModeNatural, Level: 50}
	currentClarity = &cl
	db := dynParams{GainDB: 6, CutDB: 0, RefDB: -25, KS: 0.75, AttMs: 5, RelMs: 200}
	currentDynBass = &db
	currentUserChain = []ChainItem{
		{Type: "peq", Enabled: true, Freq: 1000, GainDB: 0, Q: 1},
		{Type: "ddc", Enabled: true, Name: "Butterworth.vdc",
			Params: map[string]float64{"sections": 21}},
	}
	currentVolume = 90
	ddcDir = os.Getenv("HOME") + "/.config/jamesdsp/vdc"
	if _, err := os.Stat(ddcDir + "/Butterworth.vdc"); err != nil {
		t.Skipf("no local Butterworth.vdc (private asset, not in repo): skipping restore-ref test")
	}

	chain := make([]ChainItem, 0, 11)
	for i := 0; i < 10; i++ {
		chain = append(chain, ChainItem{Type: "peq", Enabled: true,
			Freq: 1000, GainDB: 0, Q: 1})
	}
	chain = append(chain, ChainItem{Type: "ddc", Enabled: true,
		Name: "Butterworth.vdc", Params: map[string]float64{"sections": 21}})
	currentUserChain = chain
	raw, err := marshalRuntimeState()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	resetRuntimeGlobals()
	currentUserChain = nil
	ddcMu.Lock()
	ddcSections = nil
	ddcMu.Unlock()

	restoreRuntimeState()

	found := false
	for _, it := range currentUserChain {
		if kind, ok := chainEffectType[normalizeChainType(it.Type)]; ok && resourceChainKinds[kind] {
			found = true
		}
	}
	if !found {
		t.Errorf("chain has no fir/ddc items after failed restore (refs wiped by fallback)")
	}

	raw2, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var st2 runtimeState
	if err := json.Unmarshal(raw2, &st2); err != nil {
		t.Fatalf("state file corrupted by write: %v", err)
	}

	if len(lastPlanCoefs) == 0 {
		t.Errorf("fallback seems to have deployed nothing (lastPlanCoefs empty)")
	}

	antiPopCancelRamp()
}

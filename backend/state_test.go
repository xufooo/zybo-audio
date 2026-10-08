// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withStateFile(t *testing.T) string {
	t.Helper()
	saved := stateFilePath()
	dir := t.TempDir()
	p := filepath.Join(dir, "state.json")
	setStateFilePath(p)

	t.Cleanup(func() {
		flushPendingState()
		setStateFilePath(saved)
	})

	stateMu.Lock()
	prevBoot := stateBootRestoreDone
	stateBootRestoreDone = true
	stateMu.Unlock()
	t.Cleanup(func() {
		flushPendingState()
		stateMu.Lock()
		stateBootRestoreDone = prevBoot
		stateMu.Unlock()
	})
	return p
}

func resetRuntimeGlobals() {
	currentUserChain = nil
	currentPreampDB = 0
	currentPreampAppliedDB = 0
	chainGainDB = 0
	currentDynBass = nil
	currentCrossfeed = nil
	currentSurround = nil
	selectedTypeName = ""
	loudnessOn = true
	loudnessStrength = 0.5
	loudnessRefOffsetDB = 10
	dspLimiterEnabled = true
	dspLimiterTP = true
	dspLimiterThrDB = 0
	dspLimiterAttMs = limDefaultAttMs
	dspLimiterRelMs = limDefaultRelMs
	dspHeadroomOn = false
}

func TestRuntimeStateRoundTrip(t *testing.T) {
	withStateFile(t)
	savedDyn := currentDynBass
	savedChain := currentUserChain
	t.Cleanup(func() {
		resetRuntimeGlobals()
		currentDynBass, currentUserChain = savedDyn, savedChain
	})

	resetRuntimeGlobals()
	selectedTypeName = "Headphones"
	currentUserChain = []ChainItem{
		{Type: "peq", Enabled: true, Freq: 60, GainDB: 5, Q: 0.7},
		{Type: "peq", Enabled: true, Freq: 3000, GainDB: -2, Q: 1.0},
	}
	currentPreampDB = -1.5
	chainGainDB = 3
	d := dynParams{GainDB: 8, CutDB: 1, RefDB: -20, KS: 0.6, AttMs: 3, RelMs: 150}
	currentDynBass = &d
	xf := crossfeedParams{FcutHz: 900, Feed: 45}
	currentCrossfeed = &xf
	sp := surroundParams{DelayMs: 4.5}
	currentSurround = &sp
	loudnessOn = false
	loudnessStrength = 0.8
	loudnessRefOffsetDB = 15
	dspLimiterEnabled = true
	dspLimiterTP = false
	dspLimiterThrDB = -2
	dspHeadroomOn = true

	if err := saveRuntimeState(); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	resetRuntimeGlobals()
	st, ok, err := loadRuntimeState()
	if err != nil || !ok {
		t.Fatalf("read back failed: ok=%v err=%v", ok, err)
	}
	applyRuntimeState(st, true, true, true, true, true)

	if selectedTypeName != "Headphones" {
		t.Errorf("type name = %q, expect Headphones", selectedTypeName)
	}
	if len(currentUserChain) != 2 || currentUserChain[0].Freq != 60 || currentUserChain[0].GainDB != 5 {
		t.Errorf("chain not restored: %+v", currentUserChain)
	}
	if chainGainDB != 3 {
		t.Errorf("overall gain = %v, expect 3", chainGainDB)
	}
	if currentDynBass == nil || currentDynBass.GainDB != 8 || currentDynBass.RelMs != 150 {
		t.Errorf("dynamic bass not restored: %+v", currentDynBass)
	}
	if currentCrossfeed == nil || currentCrossfeed.FcutHz != 900 || currentCrossfeed.Feed != 45 {
		t.Errorf("crossfeed not restored: %+v", currentCrossfeed)
	}
	if currentSurround == nil || currentSurround.DelayMs != 4.5 {
		t.Errorf("surround not restored: %+v", currentSurround)
	}
	if loudnessOn || loudnessStrength != 0.8 || loudnessRefOffsetDB != 15 {
		t.Errorf("loudness not restored: on=%v strength=%v offset=%v", loudnessOn, loudnessStrength, loudnessRefOffsetDB)
	}
	if dspLimiterTP || dspLimiterThrDB != -2 {
		t.Errorf("limiting not restored: tp=%v thr=%v", dspLimiterTP, dspLimiterThrDB)
	}
	if !dspHeadroomOn {
		t.Error("headroom switch not restored")
	}
}

func TestRuntimeStateDropsDynOnOldBitstream(t *testing.T) {
	withStateFile(t)
	savedDyn := currentDynBass
	t.Cleanup(func() {
		resetRuntimeGlobals()
		currentDynBass = savedDyn
	})

	resetRuntimeGlobals()
	currentUserChain = []ChainItem{{Type: "peq", Enabled: true, Freq: 100, GainDB: 4, Q: 1}}
	d := dynParams{GainDB: 6, RefDB: -25, KS: 0.75, AttMs: 5, RelMs: 200}
	currentDynBass = &d
	xf := crossfeedDefaultParams()
	currentCrossfeed = &xf
	sp := surroundDefaultParams()
	currentSurround = &sp
	if err := saveRuntimeState(); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	resetRuntimeGlobals()
	st, _, err := loadRuntimeState()
	if err != nil {
		t.Fatalf("read back failed: %v", err)
	}
	applyRuntimeState(st, false, false, false, false, false)

	if currentDynBass != nil {
		t.Errorf("legacy bitstream on should not enable dynamic bass, actual %+v", currentDynBass)
	}
	if len(currentUserChain) != 1 || currentUserChain[0].Freq != 100 {
		t.Errorf("dropping dynamic bass should not drop the chain with it: %+v", currentUserChain)
	}
	if currentCrossfeed != nil {
		t.Errorf("legacy bitstream (missing MIX2) on should not enable crossfeed, actual %+v", currentCrossfeed)
	}
	if currentSurround != nil {
		t.Errorf("legacy bitstream (missing DELAY) on should not enable surround, actual %+v", currentSurround)
	}
}

func TestRuntimeStateCorruptFileIsNotFatal(t *testing.T) {
	p := withStateFile(t)
	if err := os.WriteFile(p, []byte("{not JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := loadRuntimeState(); err == nil || !ok {
		t.Errorf("bad file should error: ok=%v err=%v", ok, err)
	}

	resetRuntimeGlobals()
	restoreRuntimeState()
	if selectedTypeName != "" || currentDynBass != nil || chainGainDB != 0 {
		t.Error("bad file when should not change move run when state")
	}
}

func TestRuntimeStateFormatMismatch(t *testing.T) {
	p := withStateFile(t)
	if err := os.WriteFile(p, []byte(`{"format":99,"chain":[],"type":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := loadRuntimeState(); err == nil || !ok {
		t.Errorf("format unrecognized should error: ok=%v err=%v", ok, err)
	}
	resetRuntimeGlobals()
	restoreRuntimeState()
	if selectedTypeName != "" {
		t.Errorf("format unrecognized when should not apply use file in type name, actual %q", selectedTypeName)
	}
}

func TestRuntimeStateMissingFile(t *testing.T) {
	withStateFile(t)
	if _, ok, err := loadRuntimeState(); ok || err != nil {
		t.Errorf("missing file should be 'missing' is not error: ok=%v err=%v", ok, err)
	}
}

func TestMarkStateDirtyCoalesces(t *testing.T) {
	p := withStateFile(t)
	savedDebounce := stateDebounce
	stateDebounce = 50 * time.Millisecond
	t.Cleanup(func() { stateDebounce = savedDebounce })
	t.Cleanup(resetRuntimeGlobals)

	resetRuntimeGlobals()
	currentUserChain = []ChainItem{{Type: "peq", Enabled: true, Freq: 200, GainDB: 3, Q: 1}}
	for i := 0; i < 5; i++ {
		markStateDirty()
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("wrote before the coalescing window elapsed (should be 2 later before writing)")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(p); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("not written after coalescing: %v", err)
	}

	fi1, _ := os.Stat(p)
	time.Sleep(120 * time.Millisecond)
	fi2, _ := os.Stat(p)
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("5 mark should merge and 1 write, actually wrote multiple times")
	}
}

func TestVolumeDefaultAndPersist(t *testing.T) {
	orig := currentVolume
	defer func() { currentVolume = orig }()

	currentVolume = 90
	applyRuntimeState(runtimeState{}, true, true, true, true, true)
	if currentVolume != 90 {
		t.Errorf("no volume words section when should protect hold default 90, actual %d", currentVolume)
	}

	applyRuntimeState(runtimeState{Volume: 65}, true, true, true, true, true)
	if currentVolume != 65 {
		t.Errorf("should restore 65, actual %d", currentVolume)
	}

	applyRuntimeState(runtimeState{Volume: 0}, true, true, true, true, true)
	if currentVolume != 65 {
		t.Errorf("volume=0 should not adopt (would mute the board), actual %d", currentVolume)
	}
	applyRuntimeState(runtimeState{Volume: 101}, true, true, true, true, true)
	if currentVolume != 65 {
		t.Errorf("volume=101 out of range should not adopt, actual %d", currentVolume)
	}
}

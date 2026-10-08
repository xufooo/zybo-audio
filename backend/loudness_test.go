// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
	"time"
)

func withVolume(t *testing.T, db float64, fn func()) {
	savedPct, savedDB, savedAt := volumePct, volumeDB, volumeReadAt
	volumePct, volumeDB, volumeReadAt = dbToPct(db), db, time.Now()
	defer func() { volumePct, volumeDB, volumeReadAt = savedPct, savedDB, savedAt }()
	fn()
}

func TestLoudnessFollowsVolume(t *testing.T) {
	savedOn, savedS, savedO := loudnessOn, loudnessStrength, loudnessRefOffsetDB
	defer func() { loudnessOn, loudnessStrength, loudnessRefOffsetDB = savedOn, savedS, savedO }()
	loudnessOn, loudnessStrength, loudnessRefOffsetDB = true, 1.0, 10.0

	withVolume(t, -30, func() {

		if got := loudnessAmount(); math.Abs(got-7.0) > 0.05 {
			t.Errorf("volume -30 dB (offset 10) when 60Hz should boost 7.0 dB, actual %.2f", got)
		}
	})
	withVolume(t, -3, func() {

		if got := loudnessAmount(); got != 0 {
			t.Errorf("volume -3 dB should not boost, actual %.2f dB", got)
		}
	})

	withVolume(t, -20, func() {
		loudnessRefOffsetDB = 15
		if got := loudnessAmount(); math.Abs(got-1.75) > 0.05 {
			t.Errorf("offset 15 when -20 dB should only boost 1.75 dB (content is already loud enough), actual %.2f", got)
		}
		loudnessRefOffsetDB = 5
		if got := loudnessAmount(); math.Abs(got-5.25) > 0.05 {
			t.Errorf("offset 5 when -20 dB should boost 5.25 dB, actual %.2f", got)
		}
	})
	withVolume(t, -60, func() {

		if got := loudnessAmount(); math.Abs(got-loudnessMaxBoostDB) > 0.01 {
			t.Errorf("minimum volume should be capped at %.1f dB, actual %.2f", loudnessMaxBoostDB, got)
		}
	})

	loudnessOn = false
	withVolume(t, -40, func() {
		if got := loudnessAmount(); got != 0 {
			t.Errorf("turn off loudness compensation should not boost, actual %.2f", got)
		}
	})
	loudnessOn, loudnessStrength = true, 0
	withVolume(t, -40, func() {
		if got := loudnessAmount(); got != 0 {
			t.Errorf("strength 0 should not boost, actual %.2f", got)
		}
	})
}

func TestLoudnessMergesIntoChain(t *testing.T) {
	savedOn, savedS, savedO := loudnessOn, loudnessStrength, loudnessRefOffsetDB
	defer func() { loudnessOn, loudnessStrength, loudnessRefOffsetDB = savedOn, savedS, savedO }()
	loudnessOn, loudnessStrength, loudnessRefOffsetDB = true, 1.0, 10.0

	withVolume(t, -30, func() {
		user := []ChainItem{
			{Type: "peq", Enabled: true, Freq: 60, GainDB: 2.0, Q: 0.7},
			{Type: "peq", Enabled: true, Freq: 1000, GainDB: -1.0, Q: 1.0},
		}
		out := withLoudness(user)
		if len(out) != len(user)+3 {
			t.Fatalf("merged section count = %d, expect %d (60Hz should merge and, its remaining 3 more points added)",
				len(out), len(user)+3)
		}
		if math.Abs(out[0].GainDB-(2.0+7.0)) > 0.05 {
			t.Errorf("60Hz should mutual add %.1f dB, actual %.1f", 2.0+7.0, out[0].GainDB)
		}
		for _, c := range out {
			if c.Freq == 1000 && c.GainDB != -1.0 {
				t.Errorf("user' s own 1kHz section should not change, actual %.1f", c.GainDB)
			}
		}
	})
}

func TestCompileCurrentChainRecompiles(t *testing.T) {
	savedChain, savedSlots := currentUserChain, currentSlots
	savedOn, savedVol := loudnessOn, volumePct
	volumeMu.Lock()
	savedDB := volumeDB
	volumeMu.Unlock()
	defer func() {
		currentUserChain, currentSlots = savedChain, savedSlots
		loudnessOn, volumePct = savedOn, savedVol
		volumeMu.Lock()
		volumeDB = savedDB
		volumeMu.Unlock()
	}()

	setDB := func(db float64) {
		volumeMu.Lock()
		volumeDB = db
		volumeMu.Unlock()
	}

	currentUserChain = nil
	loudnessOn = true
	setDB(-18)
	if err := compileCurrentChain(); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	low := dspActiveBands()
	if low == 0 {
		t.Fatalf("equal loudness on, volume -18 dB when should has compensation sections, actual 0 section (amount=%.2f)", loudnessAmount())
	}

	loudnessOn = false
	if err := compileCurrentChain(); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if off := dspActiveBands(); off != 0 {
		t.Errorf("turn off equal loudness still left %d sections compensation (should 0)", off)
	}

	loudnessOn = false
	currentSlots[0] = slotConfig{Type: "peq", Freq: 60, GainDB: 9}
	currentSlots[1] = slotConfig{Type: "peq", Freq: 150, GainDB: 9}
	if err := compileCurrentChain(); err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if n := dspActiveBands(); n != 0 {
		t.Errorf("re compile leftover sections not clear drop (still report %d sections) - description reuse legacy currentSlots", n)
	}

	loudnessOn = true
	setDB(-18)
	_ = compileCurrentChain()
	lowN := dspActiveBands()
	setDB(0)
	_ = compileCurrentChain()
	highN := dspActiveBands()
	if lowN == 0 || highN != 0 {
		t.Errorf("compensation should with volume remove lose: low volume %d sections / 0 dB when %d sections (expect >0 / 0)", lowN, highN)
	}
}

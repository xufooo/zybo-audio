// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
)

func withSlots(t *testing.T, slots [maxBands]slotConfig, preampDB float64, fn func()) {
	t.Helper()
	oldSlots, oldPre, oldApplied := currentSlots, currentPreampDB, currentPreampAppliedDB
	defer func() {
		currentSlots, currentPreampDB, currentPreampAppliedDB = oldSlots, oldPre, oldApplied
	}()
	currentSlots, currentPreampDB = slots, preampDB
	fn()
}

func rawCoeffs(slots [maxBands]slotConfig) [maxBands][coefPerBand]int32 {
	var c [maxBands][coefPerBand]int32
	for i, sc := range slots {
		c[i] = [coefPerBand]int32{qOne, 0, 0, 0, 0}
		if isBandOff(sc.Type) {
			continue
		}
		if b0, b1, b2, a1, a2, err := designBiquad(sanitizeBand(sc)); err == nil {
			c[i] = [coefPerBand]int32{b0, b1, b2, a1, a2}
		}
	}
	return c
}

func TestRockPresetNeedsHeadroom(t *testing.T) {
	rock := presets["rock"].Slots
	raw := cascadeMaxGainDB(rawCoeffs(rock))

	if raw < 2.5 || raw > 3.5 {
		t.Fatalf("rock preset max boost = %.2f dB, want about +3.0 dB (did the formula or preset change?)", raw)
	}
	t.Logf("rock preset max boost = %+.2f dB -> auto preamp = %+.2f dB (boost + 3dB safety headroom)", raw, -(raw + preampSafetyMarginDB))

	withSlots(t, rock, 0, func() {
		coefs, pre := dspFinalCoeffs()

		if got := cascadeMaxGainDB(coefs); got > -preampSafetyMarginDB+0.5 {
			t.Errorf("max boost after compensation %+.2f dB, want <= %+.1f dB (safety headroom insufficient)", got, -preampSafetyMarginDB+0.5)
		}
		if math.Abs(pre-(-(raw + preampSafetyMarginDB))) > 0.1 {
			t.Errorf("auto preamp = %.2f dB, want %.2f dB", pre, -(raw + preampSafetyMarginDB))
		}
	})
}

func TestFlatPresetNeedsNoHeadroom(t *testing.T) {
	withSlots(t, presets["flat"].Slots, 0, func() {
		coefs, pre := dspFinalCoeffs()
		if pre != 0 {
			t.Errorf("flat preset should not take headroom, got %.2f dB", pre)
		}
		if got := cascadeMaxGainDB(coefs); math.Abs(got) > 0.01 {
			t.Errorf("flat preset response is 0 dB, got %+.2f dB", got)
		}
	})
}

func TestPreampGoesToTheFirstActiveBand(t *testing.T) {

	rock := presets["rock"].Slots
	withSlots(t, rock, 0, func() {
		coefs, _ := dspFinalCoeffs()
		raw := rawCoeffs(rock)
		if coefs[0][0] >= raw[0][0] {
			t.Errorf("first-stage numerator was not pushed down: %d -> %d", raw[0][0], coefs[0][0])
		}
		for b := 1; b < maxBands; b++ {
			if coefs[b] != raw[b] {
				t.Errorf("stage %d should not have been touched: %v -> %v", b, raw[b], coefs[b])
			}
		}

		for b := 0; b < maxBands; b++ {
			if coefs[b][3] != raw[b][3] || coefs[b][4] != raw[b][4] {
				t.Errorf("stage %d a1/a2 were changed", b)
			}
		}
	})
}

func TestUserPreampWinsWhenDeeper(t *testing.T) {
	rock := presets["rock"].Slots

	withSlots(t, rock, -12, func() {
		coefs, pre := dspFinalCoeffs()
		if math.Abs(pre-(-12)) > 0.05 {
			t.Errorf("user preamp -12 dB should be honored, got %.2f dB", pre)
		}
		if got := cascadeMaxGainDB(coefs); got > 0.01 {
			t.Errorf("still has %+.2f dB peak gain after compensation", got)
		}
	})
}

func TestUserPreampTooShallowIsOverridden(t *testing.T) {
	rock := presets["rock"].Slots

	withSlots(t, rock, -2, func() {
		coefs, pre := dspFinalCoeffs()
		if pre > -5.9 {
			t.Errorf("when user preamp is not enough, should deepen automatically to <= -6.0 dB, got %.2f dB", pre)
		}
		if got := cascadeMaxGainDB(coefs); got > -preampSafetyMarginDB+0.5 {
			t.Errorf("still has %+.2f dB boost after compensation", got)
		}
	})
}

func TestEveryPresetGetsHeadroom(t *testing.T) {

	for name, p := range presets {
		name, p := name, p
		t.Run(name, func(t *testing.T) {
			raw := cascadeMaxGainDB(rawCoeffs(p.Slots))
			withSlots(t, p.Slots, p.PreampDB, func() {
				coefs, pre := dspFinalCoeffs()
				got := cascadeMaxGainDB(coefs)
				if raw > 0.01 {
					if pre > -(preampSafetyMarginDB - 0.01) {
						t.Errorf("preset %s has %+.2f dB boost but took no headroom (preamp %.2f dB)", name, raw, pre)
					}
					if got > raw-1.5 {
						t.Errorf("preset %s: before compensation %+.2f dB, after %+.2f dB, barely did anything", name, raw, got)
					}
				} else if pre != 0 {
					t.Errorf("preset %s has no boost, level must not be pushed down (preamp %.2f dB)", name, pre)
				}
				t.Logf("  preset %-10s boost %+5.2f dB -> preamp %+6.2f dB -> after compensation %+5.2f dB", name, raw, pre, got)
			})
		})
	}
}

func TestPureCutChainGetsNoPreamp(t *testing.T) {

	var cut [maxBands]slotConfig
	cut[0] = slotConfig{Type: "LP", Freq: 600, Q: 0.7}
	if got := cascadeMaxGainDB(rawCoeffs(cut)); got <= 0 || got > preampDeadZoneDB {
		t.Fatalf("lowpass cascaded max gain should land in the dead zone (0, %.2f] (quantization noise floor), got %+.4f dB",
			preampDeadZoneDB, got)
	}
	if pre := effectivePreampDB(cascadeMaxGainDB(rawCoeffs(cut)), 0); pre != 0 {
		t.Errorf("all-cut chain should not take headroom, got %.2f dB", pre)
	}

	var softish [maxBands]slotConfig
	softish[0] = slotConfig{Type: "PK", Freq: 3000, Q: 1.0, GainDB: -2.8}
	softish[1] = slotConfig{Type: "PK", Freq: 10000, Q: 0.8, GainDB: -4.0}
	if pre := effectivePreampDB(cascadeMaxGainDB(rawCoeffs(softish)), 0); pre != 0 {
		t.Errorf("fully-cut chain should not take headroom, got %.2f dB", pre)
	}

	if pre := effectivePreampDB(6.0, 0); pre > -(6.0+preampSafetyMarginDB)+0.001 {
		t.Errorf("+6 dB boost must take %.1f dB headroom, got %.2f dB", 6.0+preampSafetyMarginDB, pre)
	}
}

func TestHeadroomPolicyKeepsLoudness(t *testing.T) {

	soft := 0.0
	if pre := effectivePreampDB(4.5-headroomDB, 0); pre != 0 {
		t.Errorf("+4.5 dB boost within 18 dB headroom should not push the level down, got %.2f dB", pre)
	}
	if pre := effectivePreampDB(15.0-headroomDB, 0); pre != 0 {
		t.Errorf("+15 dB boost within 18 dB headroom should not push the level down, got %.2f dB", pre)
	}

	want := -(3.0 + preampSafetyMarginDB)
	if pre := effectivePreampDB(21.0-headroomDB, 0); pre > want+0.01 {
		t.Errorf("+21 dB boost exceeds 18 dB headroom by 3 dB, should take %.2f dB, got %.2f", want, pre)
	}
	_ = soft
}

func TestHeadroomCapabilityComesFromHardware(t *testing.T) {
	cases := []struct {
		name string
		caps dspEngineCaps
		want bool
	}{
		{"0.2 bitstream with headroom (bitmap 0x0703)", dspEngineCaps{Present: true, Opcodes: 0x0703}, true},
		{"same generation without headroom (bitmap 0x0303)", dspEngineCaps{Present: true, Opcodes: 0x0303}, false},
		{"old bitstream (no magic)", dspEngineCaps{Present: false, Opcodes: 0x0703}, false},
	}
	for _, c := range cases {
		if got := c.caps.HeadroomAvailable(); got != c.want {
			t.Errorf("%s: HeadroomAvailable() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFIRPeakVsL1(t *testing.T) {

	if len(irBank[0]) == 0 {
		taps := make([]int32, 1024)
		taps[0] = 32768
		for i := 1; i < len(taps); i++ {
			taps[i] = taps[i-1] * 9 / 10
		}
		irMu.Lock()
		irBank[0] = taps
		irBank[1] = append([]int32(nil), taps...)
		irMu.Unlock()
		defer func() { irMu.Lock(); irBank[0] = nil; irBank[1] = nil; irMu.Unlock() }()
	}
	peak := firPeakGainDBForHeadroom()

	irMu.Lock()
	sum := 0.0
	for c := 0; c < 2; c++ {
		t := 0.0
		for _, v := range irBank[c] {
			if v < 0 {
				t -= float64(v)
			} else {
				t += float64(v)
			}
		}
		if t > sum {
			sum = t
		}
	}
	irMu.Unlock()
	l1 := 20 * math.Log10(sum/32768.0+1e-12)
	if peak > l1+0.01 {
		t.Fatalf("peak %.2f dB exceeds the L1 bound %.2f dB => the metric is wrong", peak, l1)
	}
	t.Logf("IR: response peak %.2f dB, L1 bound %.2f dB (saved %.2f dB headroom = %.1f bits of resolution)",
		peak, l1, l1-peak, (l1-peak)/6)
}

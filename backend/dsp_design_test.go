// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
)

func deq(v int32) float64 { return float64(v) / float64(qOne) }

func stable(a1, a2 int32) bool {
	x, y := deq(a1), deq(a2)
	return math.Abs(y) < 1 && math.Abs(x) < 1+y
}

func magDB(b0, b1, b2, a1, a2 int32, f float64) float64 {
	w := 2 * math.Pi * f / sampleRate
	c1, s1 := math.Cos(w), math.Sin(w)
	c2, s2 := math.Cos(2*w), math.Sin(2*w)
	numRe := deq(b0) + deq(b1)*c1 + deq(b2)*c2
	numIm := -(deq(b1)*s1 + deq(b2)*s2)
	denRe := 1 + deq(a1)*c1 + deq(a2)*c2
	denIm := -(deq(a1)*s1 + deq(a2)*s2)
	return 20 * math.Log10(math.Hypot(numRe, numIm)/math.Hypot(denRe, denIm))
}

func design(t *testing.T, sc slotConfig) (int32, int32, int32, int32, int32) {
	t.Helper()
	sc = sanitizeBand(sc)
	b0, b1, b2, a1, a2, err := designBiquad(sc)
	if err != nil {
		t.Fatalf("designBiquad(%+v) failed: %v", sc, err)
	}
	return b0, b1, b2, a1, a2
}

func TestPresetBandsStableAndInRange(t *testing.T) {
	for name, p := range presets {
		for i, sc := range p.Slots {
			if isBandOff(sc.Type) {
				continue
			}
			b0, b1, b2, a1, a2 := design(t, sc)
			if !stable(a1, a2) {
				t.Errorf("preset %s %d sections %+v pole unstable: a1=%d a2=%d", name, i+1, sc, a1, a2)
			}

			want := sc.GainDB
			if sc.Type == "LS" || sc.Type == "HS" {
				want = sc.GainDB / 2
			}
			if got := magDB(b0, b1, b2, a1, a2, sc.Freq); math.Abs(got-want) > 0.6 {
				t.Errorf("preset %s %d sections %+v: %.0fHz respond %.2f dB, expect %.2f dB",
					name, i+1, sc, sc.Freq, got, want)
			}
		}
	}
}

func TestShelfDirection(t *testing.T) {
	for _, g := range []float64{6, -6} {
		b0, b1, b2, a1, a2 := design(t, slotConfig{Type: "HS", Freq: 1000, Q: 0.7, GainDB: g})
		if got := magDB(b0, b1, b2, a1, a2, 20000); math.Abs(got-g) > 1.0 {
			t.Errorf("high rack %+.0fdB @1k: 20kHz %.2f dB, should~=%.0f", g, got, g)
		}
		if got := magDB(b0, b1, b2, a1, a2, 50); math.Abs(got) > 1.0 {
			t.Errorf("high rack %+.0fdB @1k: 50Hz %.2f dB, should~=0", g, got)
		}

		b0, b1, b2, a1, a2 = design(t, slotConfig{Type: "LS", Freq: 1000, Q: 0.7, GainDB: g})
		if got := magDB(b0, b1, b2, a1, a2, 50); math.Abs(got-g) > 1.0 {
			t.Errorf("low rack %+.0fdB @1k: 50Hz %.2f dB, should~=%.0f", g, got, g)
		}
		if got := magDB(b0, b1, b2, a1, a2, 20000); math.Abs(got) > 1.0 {
			t.Errorf("low rack %+.0fdB @1k: 20kHz %.2f dB, should~=0", g, got)
		}
	}
}

func TestCoeffReadbackDecode(t *testing.T) {
	cases := []struct {
		reg  uint32
		want int32
	}{
		{17906, 17906},
		{0xFFFFA29D, -23907},
		{1042, 1042},
		{0xFFFBFFFF, -262145},
		{0x0001FFFF, 131071},
		{0x00020000, 131072},
	}
	for _, c := range cases {
		if got := decodeCoefReadback(c.reg); got != c.want {
			t.Errorf("decodeCoefReadback(%#x) = %d, should be %d", c.reg, got, c.want)
		}
	}
}

func TestZeroGainPeakingIsExactUnity(t *testing.T) {
	for _, f := range []float64{60, 150, 400, 1000, 3000, 10000, 20000} {
		b0, b1, b2, a1, a2 := design(t, slotConfig{Type: "PK", Freq: f, Q: 0.7})
		if b0 != qOne || b1 != a1 || b2 != a2 {
			t.Errorf("%.0fHz 0dB PK is not exact bypass: b0=%d b1=%d a1=%d b2=%d a2=%d",
				f, b0, b1, a1, b2, a2)
		}
		if m := magDB(b0, b1, b2, a1, a2, 1000); math.Abs(m) > 1e-9 {
			t.Errorf("%.0fHz 0dB PK in 1kHz respond %.6f dB, should be 0", f, m)
		}
	}
}

func TestUnityCoefficientsAreTransparent(t *testing.T) {
	if qOne != 32768 {
		t.Fatalf("Q3.15 1.0 should be 32768, actual %d", qOne)
	}
	if m := magDB(qOne, 0, 0, 0, 0, 1000); math.Abs(m) > 1e-9 {
		t.Errorf("single bit coefficients respond %.6f dB, should be 0", m)
	}
}

func TestLowFrequencyFloor(t *testing.T) {
	low := sanitizeBand(slotConfig{Type: "PK", Freq: 20, Q: 0.7, GainDB: 12})
	if low.Freq != minBandFreq {
		t.Errorf("20Hz should clamped to %.0fHz, actual %.0fHz", minBandFreq, low.Freq)
	}

	for _, g := range []float64{12, -12} {
		b0, b1, b2, a1, a2 := design(t, slotConfig{Type: "PK", Freq: 60, Q: 10, GainDB: g})
		if !stable(a1, a2) {
			t.Errorf("60Hz Q=10 %+.0fdB pole unstable: a1=%d a2=%d", g, a1, a2)
		}
		for n, v := range map[string]int32{"b0": b0, "b1": b1, "b2": b2, "a1": a1, "a2": a2} {
			if v > coefMax || v < coefMin {
				t.Errorf("60Hz Q=10 %+.0fdB coefficients %s=%d out of range", g, n, v)
			}
		}
	}
}

func TestUIBandFrequencies(t *testing.T) {
	uiFreqs := []float64{60, 150, 400, 1000, 3000, 10000}
	if len(uiFreqs) != legacyBands {
		t.Fatalf("0.1 panel frequency point count %d != 0.1 hard item fixed set section count %d", len(uiFreqs), legacyBands)
	}
	for _, f := range uiFreqs {
		if f < minBandFreq || f > maxBandFreq {
			t.Errorf("WebUI frequency point %.0fHz exceeds safe all range [%.0f, %.0f]", f, minBandFreq, maxBandFreq)
		}
		for _, g := range []float64{12, -12} {
			_, _, _, a1, a2 := design(t, slotConfig{Type: "PK", Freq: f, Q: 0.7, GainDB: g})
			if !stable(a1, a2) {
				t.Errorf("%.0fHz %+.0fdB pole unstable", f, g)
			}
		}
	}
}

func TestCoefficientLayoutAndBandCount(t *testing.T) {
	saved := currentSlots
	defer func() { currentSlots = saved }()

	for i := range currentSlots {
		currentSlots[i] = slotConfig{Type: "off"}
	}
	if n := dspActiveBands(); n != 0 {
		t.Errorf("all off when NR_BANDS should be 0, actual %d", n)
	}

	currentSlots[0] = slotConfig{Type: "PK", Freq: 1000, Q: 0.7, GainDB: 3}
	currentSlots[4] = slotConfig{Type: "PK", Freq: 3000, Q: 0.7, GainDB: -3}
	if n := dspActiveBands(); n != 5 {
		t.Errorf("0 and 4 section raw effect when NR_BANDS should be 5, actual %d", n)
	}

	exp := dspExpectedCoeffs()
	if len(exp) != coefTotal {
		t.Fatalf("coefficient count %d != %d", len(exp), coefTotal)
	}

	for band := 1; band <= 3; band++ {
		if exp[band*coefPerBand] != qOne {
			t.Errorf("%d sections(off) b0 should be %d, actual %d", band, qOne, exp[band*coefPerBand])
		}
		for k := 1; k < coefPerBand; k++ {
			if exp[band*coefPerBand+k] != 0 {
				t.Errorf("%d sections(off) %d coefficients should be 0", band, k)
			}
		}
	}

	if exp[0] == qOne {
		t.Error("0 section +3dB b0 should not equals 1.0")
	}
}

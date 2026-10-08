// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"errors"
	"fmt"
	"math"
	"math/cmplx"
	"testing"
)

func TestZZTwentyHzIsMarginallyStableBeforeFix(t *testing.T) {

	_, _, _, a1, a2 := rbjPeakingEQ(20, 0.7, 3.5, sampleRate)
	r := biquadPoleRadius(a1, a2)
	if biquadStableByJury(a1, a2) {
		t.Skipf("field already not reproduce: radius %.9f already judge stable (may quantize path change)", r)
	}
	t.Logf("reproduced OK: 20 Hz/+3.5dB/Q0.7 post-quantization pole radius = %.9f, Jury judge for unstable (marginal)", r)
}

func TestZZDesignBiquadReturnsStableCoefficients(t *testing.T) {

	cases := []struct {
		f, g, q float64
		typ     string
	}{
		{20, 3.5, 0.7, "PK"}, {20, 0, 0.7, "PK"}, {20, -3, 1.4, "PK"},
		{20, 6, 1.0, "PK"}, {20, 10, 0.7, "PK"}, {25, 12, 1.4, "PK"},
		{30, 5, 2.0, "PK"}, {40, 3, 1.4, "PK"}, {42, 0.5, 0.5, "PK"},
		{88.3, 3.5, 0.5, "PK"}, {20, 1, 0.7, "LS"}, {20, -1, 0.7, "HS"},
		{20, 0, 0.7, "HP"}, {20, 0, 0.7, "LP"}, {1000, 3, 1.4, "PK"},
		{16000, 2.5, 0.7, "PK"},
	}
	for _, c := range cases {
		sc := slotConfig{Type: c.typ, Freq: c.f, GainDB: c.g, Q: c.q}
		b0, b1, b2, a1, a2, err := designBiquad(sc)
		tag := fmt.Sprintf("%s %.0fHz %+gdB Q%.1f", c.typ, c.f, c.g, c.q)
		if err != nil {

			var se *stabilityError
			if !errors.As(err, &se) {
				t.Errorf("%s: report is not stable nature error: %v", tag, err)
			}
			t.Logf("%s: rejected faithfully (%v)", tag, err)
			continue
		}
		r := biquadPoleRadius(a1, a2)
		if !biquadStableByJury(a1, a2) {
			t.Errorf("%s deploy but Jury criterion judge for unstable (radius %.9f)", tag, r)
		}
		if b0 == 0 && b1 == 0 && b2 == 0 && a1 == 0 && a2 == 0 {
			t.Errorf("%s returned all-zero coefficients", tag)
		}
	}
}

func TestZZStabilityFixKeepsFrequencyAndGain(t *testing.T) {
	b0, b1, b2, a1, a2, err := designStableBiquad(20, 0.7, 3.5, rbjPeakingEQ)
	if err != nil {
		t.Fatalf("redesign failed: %v", err)
	}
	if !biquadStableByJury(a1, a2) {
		t.Errorf("redesign still unstable: radius %.9f", biquadPoleRadius(a1, a2))
	}

	pk, pf := peakNearOneSection(b0, b1, b2, a1, a2)
	if pk < 3.0 || pk > 4.0 {
		t.Errorf("peak %+.2f dB not in +3.5 dB near (gain target change)@ %.0f Hz", pk, pf)
	}
	if pf > 200 {
		t.Errorf("peak frequency %.0f Hz drift to low-frequency region outside (resonance drift)", pf)
	}
	t.Logf("20 Hz/+3.5dB/Q0.7 redesign: peak %+.2f dB @ %.0f Hz, radius %.9f (a1=%d a2=%d)",
		pk, pf, biquadPoleRadius(a1, a2), a1, a2)
}

func TestZZZeroGainPeakingStaysBitExact(t *testing.T) {
	raw0, raw1, raw2, rawA1, rawA2 := rbjPeakingEQ(1000, 0.7, 0, sampleRate)
	if !(raw1 == rawA1 && raw2 == rawA2) {
		t.Fatalf("precondition not stand: gain=0 peaking itself is not bypass (%d/%d vs %d/%d)",
			raw1, raw2, rawA1, rawA2)
	}
	b0, b1, b2, a1, a2, err := designStableBiquad(1000, 0.7, 0, rbjPeakingEQ)
	if err != nil {
		t.Fatalf("returned error: %v", err)
	}
	if b0 != raw0 || b1 != raw1 || b2 != raw2 || a1 != rawA1 || a2 != rawA2 {
		t.Errorf("gain=0 bypass section redesign: %v/%v/%v/%v/%v => %d/%d/%d/%d/%d",
			raw0, raw1, raw2, rawA1, rawA2, b0, b1, b2, a1, a2)
	}
	for _, f := range []float64{31, 200, 1000, 8000, 16000} {
		if g := oneSectionDB(b0, b1, b2, a1, a2, f); g > 1e-9 || g < -1e-9 {
			t.Errorf("gain=0 bypass section in %.0f Hz on is not 0 dB: %.3e", f, g)
		}
	}
}

func oneSectionDB(b0, b1, b2, a1, a2 int32, f float64) float64 {
	w := 2 * math.Pi * f / sampleRate
	z1 := complex(math.Cos(-w), math.Sin(-w))
	z2 := z1 * z1
	num := complex(float64(b0)/qOne, 0) +
		complex(float64(b1)/qOne, 0)*z1 + complex(float64(b2)/qOne, 0)*z2
	den := complex(1, 0) + complex(float64(a1)/qOne, 0)*z1 + complex(float64(a2)/qOne, 0)*z2
	return 20 * math.Log10(cmplx.Abs(num/den)+1e-18)
}

func peakNearOneSection(b0, b1, b2, a1, a2 int32) (float64, float64) {
	mx, mf := -99.0, 0.0
	for i := 0; i < 200000; i++ {
		f := 20 * math.Pow(1000, float64(i)/200000.0)
		if v := oneSectionDB(b0, b1, b2, a1, a2, f); v > mx {
			mx, mf = v, f
		}
	}
	return mx, mf
}

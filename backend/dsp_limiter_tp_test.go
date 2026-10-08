// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
)

func tauMeasured(k uint32) float64 {
	g, tgt := 0.0, -1.0
	for n := 1; n <= 40*limSampleRateHz; n++ {
		g += (tgt - g) * float64(k) / qOne
		if g <= tgt*(1-1.0/math.E) {
			return float64(n) / limSampleRateHz * 1000.0
		}
	}
	return math.Inf(1)
}

func TestLimKTauSelfConsistent(t *testing.T) {
	for _, ms := range []float64{0.05, 0.5, 5, 50, 200, 341} {
		k := limKFromMsTP(ms)
		measured := tauMeasured(k)
		reported := limMsFromKTP(k)
		if math.Abs(measured-reported)/reported > 0.05 {
			t.Errorf("%g ms:recurrence measurement τ=%.2f ms,but limMsFromKTP report %.2f ms", ms, measured, reported)
		}
	}
}

func TestLimKQuantizationWithinRange(t *testing.T) {
	for _, ms := range []float64{0.5, 5, 50, 200} {
		got := limMsFromKTP(limKFromMsTP(ms))
		if math.Abs(got-ms)/ms > 0.20 {
			t.Errorf("%g ms -> actual %.2f ms(deviation %.1f%%),exceeds quantize sane range", ms, got, 100*(got-ms)/ms)
		}
	}
}

func TestLimKRangeCeiling(t *testing.T) {
	if k := limKFromMsTP(1000); k < 1 {
		t.Fatalf("1000 ms k out of range:%d", k)
	}
	got := limMsFromKTP(limKFromMsTP(1000))
	if got > limMaxMsTP*1.05 {
		t.Errorf("still reports after clamping %.1f ms > cap %.1f ms", got, limMaxMsTP)
	}

	for _, ms := range []float64{0.001, 0.05, 1, 100, 341, 5000} {
		k := limKFromMsTP(ms)
		if k < 1 || k >= qOne {
			t.Errorf("%g ms -> k=%d out of range(requiring 1 ≤ k < %d)", ms, k, qOne)
		}
	}
}

func TestLimKMonotoneAndSign(t *testing.T) {
	prev := uint32(1 << 30)
	for _, ms := range []float64{0.05, 0.5, 5, 50, 200, 341} {
		k := limKFromMsTP(ms)
		if k > prev {
			t.Errorf("%g ms -> k=%d no monotonically decrease(previous %d)", ms, k, prev)
		}
		prev = k
	}
	if limKFromMsTP(200) >= qOne {
		t.Error("new curverelease coefficient must < 1.0(log2 domain first-order),otherwise will diverge")
	}

	oldRel := math.Exp(1.0 / (200.0 / 1000.0 * sampleRate))
	if oldRel <= 1.0 {
		t.Error("old curverelease coefficient should > 1.0(control group broken,since formula was broken)")
	}
}

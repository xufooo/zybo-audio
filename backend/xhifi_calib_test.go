// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
)

var oracleXHIFI = map[float64]map[float64]float64{
	0: {
		20: -0.07, 30: -0.41, 50: -0.79, 80: -1.70, 120: -1.99, 200: -0.07,
		300: 0.80, 500: -4.02, 800: -3.76, 1200: -6.64, 1800: -3.36, 2500: 0.06,
		4000: 0.31, 6000: 1.04, 8000: 1.31, 10000: 1.43, 12000: 1.49,
		14000: 1.53, 16000: 1.55, 18000: 1.57, 20000: 1.58,
	},
	50: {
		20: -0.11, 30: -0.44, 50: -0.81, 80: -1.56, 120: -1.07, 200: 2.12,
		300: 3.28, 500: 0.08, 800: -0.92, 1200: -3.18, 1800: 0.44, 2500: 3.45,
		4000: 3.89, 6000: 4.59, 8000: 4.84, 10000: 4.95, 12000: 5.02,
		14000: 5.05, 16000: 5.08, 18000: 5.09, 20000: 5.10,
	},
}

func xhifiModelResponse(t *testing.T, level float64, freqs []float64) map[float64]float64 {
	t.Helper()
	n, err := xhifiNode(level, sampleRate)
	if err != nil {
		t.Fatalf("xhifiNode(%g)：%v", level, err)
	}

	w1 := float64(n.Mix[0]) / float64(qOne)
	w2 := float64(n.Mix[1]) / float64(qOne)
	dBP, dLP := float64(n.Flags), float64(n.Len)

	out := make(map[float64]float64, len(freqs))
	for _, f := range freqs {
		hp := biquadResponse(n.Coefs, f, sampleRate)
		lpBP := biquadResponse(n.Hi, f, sampleRate)
		hpBP := biquadResponse(n.Side, f, sampleRate)
		lp := biquadResponse(n.Extra, f, sampleRate)

		hpPath := complex(w1, 0) * hp * hp * hp
		bpPath := complex(w2, 0) * lpBP * lpBP * lpBP * hpBP * hpBP * hpBP * zPow(f, dBP)
		lpPath := lp * zPow(f, dLP)
		h := hpPath + bpPath + lpPath
		out[f] = 20 * math.Log10(cmplxAbs(h))
	}
	return out
}

func zPow(f, n float64) complex128 {
	ph := -2 * math.Pi * f * n / sampleRate
	return complex(math.Cos(ph), math.Sin(ph))
}

func cmplxAbs(c complex128) float64 { return math.Hypot(real(c), imag(c)) }

func TestXHIFIMatchesOfficialCore(t *testing.T) {
	const tol = 0.6
	for level, table := range oracleXHIFI {
		freqs := make([]float64, 0, len(table))
		for f := range table {
			freqs = append(freqs, f)
		}
		got := xhifiModelResponse(t, level, freqs)
		var sumSq, worst, worstF float64
		for _, f := range freqs {
			d := got[f] - table[f]
			sumSq += d * d
			if math.Abs(d) > math.Abs(worst) {
				worst, worstF = d, f
			}
			if math.Abs(d) > tol {
				t.Errorf("level=%g f=%g Hz:model %.2f dB vs official-core measurement %.2f dB(diff %+.2f dB > %.1f)"+
					" -- XHIFI structure/weights/delaybroken by an edit ?see refs/viperfx_oracle/README.md",
					level, f, got[f], table[f], d, tol)
			}
		}
		rms := math.Sqrt(sumSq / float64(len(freqs)))
		t.Logf("level=%g: RMS deviation %.3f dB, worst %+.2f dB at %g Hz (%d points total)",
			level, rms, worst, worstF, len(freqs))
		if rms > 0.25 {
			t.Errorf("level=%g overall RMS deviation %.3f dB toolarge(at calibration timeis 0.04 dB)", level, rms)
		}
	}
}

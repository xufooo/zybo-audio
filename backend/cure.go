// SPDX-License-Identifier: GPL-2.0-only
package main

import "math"

func iir1LPF_BW(freq, fs float64) (float64, float64, float64) {
	t := math.Tan(math.Pi * freq / fs)
	b0 := t / (1 + t)
	a1 := (1 - t) / (1 + t)
	return b0, b0, -a1
}

func iir1HPF_BW(freq, fs float64) (float64, float64, float64) {
	t := math.Tan(math.Pi * freq / fs)
	b0 := 1 / (1 + t)
	a1 := (1 - t) / (1 + t)
	return b0, -b0, -a1
}

func biquadFrom1st(b0, b1, a1slot float64) [5]int32 {
	return [5]int32{
		q315Round(b0), q315Round(b1), 0,
		q315Round(a1slot), 0,
	}
}

func curePassFilterSections(fs float64) [4][5]int32 {
	cutoff := 18000.0
	if fs < 44100 {
		cutoff = fs - 100
	}
	h0, h1, ha := iir1HPF_BW(10.0, fs)
	l0, l1, la := iir1LPF_BW(cutoff, fs)
	return [4][5]int32{
		biquadFrom1st(h0, h1, ha),
		biquadFrom1st(l0, l1, la),
		biquadFrom1st(l0, l1, la),
		biquadFrom1st(l0, l1, la),
	}
}

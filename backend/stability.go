// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"strconv"
)

func strconvItoa(v int) string            { return strconv.Itoa(v) }
func strconvFtoa(v float64, p int) string { return strconv.FormatFloat(v, 'f', p, 64) }

func biquadStableByJury(a1, a2 int32) bool {
	x := float64(a1) / qOne
	y := float64(a2) / qOne
	return math.Abs(y) < 1 && math.Abs(x) < 1+y
}

func biquadPoleRadius(a1, a2 int32) float64 {
	A := float64(a1) / qOne
	B := float64(a2) / qOne
	disc := A*A - 4*B
	if disc >= 0 {
		s := math.Sqrt(disc)
		return math.Max(math.Abs((-A+s)/2), math.Abs((-A-s)/2))
	}
	if B <= 0 {
		return math.Inf(1)
	}
	return math.Sqrt(B)
}

const qBackoff = 0.99

const maxStabilityRetries = 12

type stabilityError struct {
	Freq  float64
	Q     float64
	Gain  float64
	Tried int
}

func (e *stabilityError) Error() string {
	return "This filter cannot be built in Q3.15 (a pole would land on the unit circle; " +
		"tried backing Q off to 0.886x and it is still marginal): " +
		"Frequency may be too low or Q too high (currently " + stabFtoa(e.Freq, 0) + " Hz / Q " +
		stabFtoa(e.Q, 2) + " / " + stabFtoa(e.Gain, 1) + " dB), " +
		"lower Q or raise the frequency"
}

func stabItoa(v int) string            { return strconvItoa(v) }
func stabFtoa(v float64, p int) string { return strconvFtoa(v, p) }

func designStableBiquad(freq, Q, gainDB float64,
	design func(freq, Q, gainDB, fs float64) (int32, int32, int32, int32, int32)) (
	int32, int32, int32, int32, int32, error) {

	if gainDB == 0 {
		b0, b1, b2, a1, a2 := design(freq, Q, gainDB, sampleRate)
		return b0, b1, b2, a1, a2, nil
	}

	q := Q
	for i := 0; i < maxStabilityRetries; i++ {
		b0, b1, b2, a1, a2 := design(freq, q, gainDB, sampleRate)
		if biquadStableByJury(a1, a2) {
			return b0, b1, b2, a1, a2, nil
		}
		q *= qBackoff
	}
	return 0, 0, 0, 0, 0, &stabilityError{
		Freq: freq, Q: Q, Gain: gainDB, Tried: maxStabilityRetries,
	}
}

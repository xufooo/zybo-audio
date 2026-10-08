// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"math/cmplx"
)

const preampSafetyMarginDB = 3.0

const preampDeadZoneDB = 0.25

func cascadeMaxGainDB(coefs [maxBands][coefPerBand]int32) float64 {
	const (
		fLo = 20.0
		fHi = 20000.0
		n   = 400
	)
	maxG := 0.0
	first := true
	for i := 0; i < n; i++ {
		f := fLo * math.Pow(fHi/fLo, float64(i)/float64(n-1))
		w := 2 * math.Pi * f / sampleRate
		z1 := complex(math.Cos(w), -math.Sin(w))
		z2 := z1 * z1
		h := complex(1, 0)
		for b := 0; b < maxBands; b++ {
			c := coefs[b]
			num := complex(float64(c[0]), 0) + complex(float64(c[1]), 0)*z1 + complex(float64(c[2]), 0)*z2
			den := complex(float64(qOne), 0) + complex(float64(c[3]), 0)*z1 + complex(float64(c[4]), 0)*z2
			if den == complex(0, 0) {
				continue
			}
			h *= num / den
		}
		db := 20 * math.Log10(cmplx.Abs(h))
		if first || db > maxG {
			maxG = db
			first = false
		}
	}
	return maxG
}

func effectivePreampDB(maxGainDB, userPreampDB float64) float64 {
	if userPreampDB > 0 {
		userPreampDB = 0
	}

	need := 0.0
	if maxGainDB > preampDeadZoneDB {
		need = -(maxGainDB + preampSafetyMarginDB)
	}
	if userPreampDB < need {
		return userPreampDB
	}
	return need
}

func firPeakGainDBForHeadroom() float64 {
	irMu.Lock()
	defer irMu.Unlock()
	if len(irBank[0]) == 0 && len(irBank[1]) == 0 {
		return 0
	}
	best := -120.0
	for f := 20.0; f <= 20000.0; f *= 1.0293022366434921 {
		for c := 0; c < 2; c++ {
			taps := irBank[c]
			if len(taps) == 0 {
				continue
			}
			re, im := 0.0, 0.0
			for i, t := range taps {
				a := 2 * math.Pi * f * float64(i) / sampleRate
				re += float64(t) * math.Cos(a)
				im -= float64(t) * math.Sin(a)
			}

			if g := 20 * math.Log10(math.Hypot(re, im)/32768.0+1e-12); g > best {
				best = g
			}
		}
	}
	return best
}

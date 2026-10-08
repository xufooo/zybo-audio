// SPDX-License-Identifier: GPL-2.0-only
package main

import "math"

func fitCenters(maxBands int) []float64 {
	hi := 16000.0
	if cap := sampleRate * 0.45; hi > cap {
		hi = cap
	}
	if hi < minBandFreq*2 {
		hi = minBandFreq * 2
	}
	g := logGrid(minBandFreq, hi, maxBands)
	for i := range g {
		g[i] = math.Round(g[i]*10) / 10
	}
	return g
}

var fitQSet = []float64{0.5, 0.7, 1.0, 1.4}

func fitBandsToTarget(target, freqs []float64, maxBands int) ([]ChainItem, float64, error) {
	if len(freqs) == 0 || len(freqs) != len(target) {
		return nil, 0, errFitBadInput
	}
	if maxBands < 1 {
		maxBands = 1
	}
	centers := fitCenters(maxBands)

	chain := make([]ChainItem, maxBands)
	band := make([][]float64, maxBands)
	for i, fc := range centers {
		chain[i] = ChainItem{Type: "peq", Enabled: true, Freq: fc, GainDB: 0, Q: 0.7}
		band[i] = bandResponseDB(slotConfig{Type: "PK", Freq: fc, Q: 0.7, GainDB: 0}, freqs)
	}
	sum := make([]float64, len(freqs))
	addBand := func(dst, src []float64, sign float64) {
		for i := range dst {
			dst[i] += sign * src[i]
		}
	}
	for i := range band {
		addBand(sum, band[i], 1)
	}

	const (
		gainLo, gainHi, gainStep = -12.0, 12.0, 0.5
		passes                   = 4
	)
	for pass := 0; pass < passes; pass++ {
		moved := false
		for i := range centers {
			best := chain[i]
			bestSSE := sse(sum, target)
			changed := false
			for _, q := range fitQSet {
				for g := gainLo; g <= gainHi+1e-9; g += gainStep {
					cand := ChainItem{Type: "peq", Enabled: true, Freq: centers[i], GainDB: math.Round(g*10) / 10, Q: q}
					r := bandResponseDB(slotConfig{Type: "PK", Freq: cand.Freq, Q: cand.Q, GainDB: cand.GainDB}, freqs)

					addBand(sum, band[i], -1)
					addBand(sum, r, 1)
					e := sse(sum, target)
					addBand(sum, r, -1)
					addBand(sum, band[i], 1)
					if e < bestSSE-1e-9 {
						bestSSE, best, changed = e, cand, true
					}
				}
			}
			if changed {
				r := bandResponseDB(slotConfig{Type: "PK", Freq: best.Freq, Q: best.Q, GainDB: best.GainDB}, freqs)
				addBand(sum, band[i], -1)
				addBand(sum, r, 1)
				band[i] = r
				chain[i] = best
				moved = true
			}
		}
		if !moved {
			break
		}
	}
	return chain, rmsDB(sum, target), nil
}

func sse(got, want []float64) float64 {
	s := 0.0
	for i := range got {
		d := got[i] - want[i]
		s += d * d
	}
	return s
}

var errFitBadInput = errFitInput{}

type errFitInput struct{}

func (errFitInput) Error() string {
	return "invalid fit input (target curve and frequency-point lengths differ)"
}

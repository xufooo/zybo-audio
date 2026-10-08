// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

type exciterParams struct {
	Harmonics [10]float64 `json:"harmonics"`

	Mix float64 `json:"mix"`

	HPFHz float64 `json:"hpf_hz"`
	LPFHz float64 `json:"lpf_hz"`
}

const maxMixQ315 = 131071.0 / 32768.0

func exciterWetScale(p exciterParams) float64 {
	c := harmonicMonomialCoeffs(p.Harmonics)
	if mx := polyMaxCoef(c); mx > 3.9 {
		return 3.9 / mx
	}
	return 1.0
}

func validateExciterMix(p exciterParams) error {
	if p.Mix < 0 {
		return fmt.Errorf("harmonic exciter mix cannot be negative(got %.3f)", p.Mix)
	}
	wet := p.Mix / exciterWetScale(p)
	limit := maxMixQ315 * maxMixQ315
	if wet > limit {
		return fmt.Errorf("harmonic exciter equivalent wet gain mix/scale=%.3f exceeds this unit limit %.2f: "+
			"wet signal lands on MIX2 Q3.15 coefficients (18-bit signed => per-coefficient cap 131071/32768~=4.0, "+
			"gain stage multiplies by another 4.0 => %.2f total); excess saturates silently (no UI indication)",
			wet, limit, limit)
	}
	return nil
}

func exciterMuteSamples(p exciterParams) int {
	big := 0.0
	for _, a := range p.Harmonics {
		if v := math.Abs(a); v > big {
			big = v
		}
	}
	n := int(big * 10000.0)
	if n < 0 {
		n = 0
	}
	if n > 65535 {
		n = 65535
	}
	return n
}

func exciterDefaultParams() exciterParams {
	var p exciterParams
	for i := 0; i < 10; i += 2 {
		p.Harmonics[i] = 0.02
	}
	p.Mix = 0.56
	p.HPFHz = 7600
	p.LPFHz = float64(sampleRate)/2 - 2000
	return p
}

func harmonicMonomialCoeffs(amps [10]float64) [11]float64 {
	var u1 [11]float64
	absSum := 0.0
	for _, a := range amps {
		absSum += math.Abs(a)
	}
	scale := 1.0
	if absSum > 1.0 {
		scale = 1.0 / absSum
	}
	for i := 0; i < 10; i++ {
		u1[i+1] = amps[i] * scale
	}

	var c [11]float64
	var u2 [11]float64
	c[10] = u1[10]
	for i := 2; i < 11; i++ {
		for j := 0; j < i; j++ {
			tmp := u2[i-j]
			u2[i-j] = c[i-j]
			c[i-j] = c[i-j-1]*2.0 - tmp
		}
		tmp := u1[10-i+1] - u2[0]
		u2[0] = c[0]
		c[0] = tmp
	}
	for i := 1; i < 11; i++ {
		c[10-i+1] = c[10-i] - u2[10-i+1]
	}
	c[0] = u1[0]/2.0 - u2[0]
	return c
}

func harmonicQ315(p exciterParams) ([12]int32, error) {
	if len(p.Harmonics) != 10 {
		return [12]int32{}, fmt.Errorf("harmonic amplitudes must be 10 numbers (got %d)", len(p.Harmonics))
	}
	for i, a := range p.Harmonics {
		if math.IsNaN(a) || math.IsInf(a, 0) {
			return [12]int32{}, fmt.Errorf("harmonic %damplitude is not finite number", i+1)
		}
		if math.Abs(a) > 1.0 {
			return [12]int32{}, fmt.Errorf("harmonic %d amplitude %.4f exceeds 1.0 (decide how much you want before normalizing)", i+1, a)
		}
	}
	c := harmonicMonomialCoeffs(p.Harmonics)
	var out [12]int32
	for i, v := range c {
		out[i] = q315Round(v)
	}
	out[11] = int32(exciterMuteSamples(p))
	return out, nil
}

func q315Round(v float64) int32 {
	const half = 0.5 / 32768.0
	q := math.Round((v + math.Copysign(half, v)) * 32768.0)
	if q > 131071 {
		q = 131071
	}
	if q < -131072 {
		q = -131072
	}
	return int32(q)
}

func polyMaxCoef(c [11]float64) float64 {
	m := 0.0
	for _, v := range c {
		if a := math.Abs(v); a > m {
			m = a
		}
	}
	return m
}

var currentExciter *exciterParams

func exciterAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().PolyAvailable()
}

func setExciter(p *exciterParams) error {
	if p == nil {
		currentExciter = nil
		markStateDirty()
		return nil
	}
	if !exciterAvailable() {
		return fmt.Errorf("this hardware has no POLY stage (CAP1 bit6 not set), harmonic exciter cannot dispatch")
	}
	q := *p
	if _, err := harmonicQ315(q); err != nil {
		return err
	}
	if err := validateExciterMix(q); err != nil {
		return err
	}
	currentExciter = &q
	markStateDirty()
	return nil
}

func exciterView() any {
	if currentExciter == nil {
		return nil
	}
	return currentExciter
}

func exciterNodes(p exciterParams) ([]planNode, error) {
	coefs, err := harmonicQ315(p)
	if err != nil {
		return nil, err
	}

	c := harmonicMonomialCoeffs(p.Harmonics)
	scale := exciterWetScale(p)
	if scale != 1.0 {
		for i := 0; i < 11; i++ {
			coefs[i] = q315Round(c[i] * scale)
		}
	}
	mix := p.Mix / scale

	var gainStage int32
	wet := mix
	if wet > maxMixQ315 {
		gainStage = q315Round(wet / maxMixQ315)
		wet = maxMixQ315
	}
	h0, h1, h2, h3, h4 := rbjHighPass(p.HPFHz, 0.717, sampleRate)
	hpf := [5]int32{h0, h1, h2, h3, h4}
	l0, l1, l2, l3, l4 := rbjLowPass(p.LPFHz, 0.717, sampleRate)
	lpf := [5]int32{l0, l1, l2, l3, l4}
	return []planNode{
		{Kind: planKindExciter, Coefs: hpf, Hi: lpf, Poly: coefs,
			Mix: [2]int32{32767, q315Round(wet)},

			MixB: [2]int32{gainStage, 0}},
	}, nil
}

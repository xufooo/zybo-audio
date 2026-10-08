// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

var analogxTiers = [3]struct {
	Gain float64
	LPHz float64
}{
	{0.6, 19650.0},
	{1.2, 18233.0},
	{2.4, 16307.0},
}

var analogxHarmonics = [10]float64{0.01, 0.02, 0.0001, 0.001, 0, 0, 0, 0, 0, 0}

type analogxParams struct {
	Model int `json:"model"`
}

func analogxDefaultParams() analogxParams { return analogxParams{Model: 0} }

func analogxPeak(fs float64) [5]int32 {
	const (
		gainAmp  = 0.58
		freq     = 633.0
		q        = 6.28
		postGain = 0.8
	)

	gain := math.Pow(10, gainAmp/40.0)
	omega := 2 * math.Pi * freq / fs
	sinO, cosO := math.Sin(omega), math.Cos(omega)

	y := math.Sinh((q*math.Ln2*omega/2)/sinO) * sinO

	a0 := 1 + y/gain
	a2 := 1 - y/gain
	b0 := 1 + y*gain
	b1 := -2 * cosO
	b2 := 1 - y*gain

	return [5]int32{
		q315Round(b0 / a0 * postGain),
		q315Round(b1 / a0 * postGain),
		q315Round(b2 / a0 * postGain),
		q315Round(-2 * cosO / a0),
		q315Round(a2 / a0),
	}
}

func analogxNodes(p analogxParams, fs float64) ([]planNode, error) {
	if p.Model < 0 || p.Model >= len(analogxTiers) {
		return nil, fmt.Errorf("AnalogX only has levels 0/1/2 (got %d)", p.Model)
	}
	tier := analogxTiers[p.Model]

	poly, err := harmonicQ315(exciterParams{Harmonics: analogxHarmonics, Mix: 1.0,
		HPFHz: 240.0, LPFHz: tier.LPHz})
	if err != nil {
		return nil, err
	}
	hp, err := multiBiquad("HP", 0, 240.0, 0.717, fs, false)
	if err != nil {
		return nil, err
	}
	lp, err := multiBiquad("LP", 0, tier.LPHz, 0.717, fs, false)
	if err != nil {
		return nil, err
	}
	return []planNode{{
		Kind:  planKindAnalogX,
		Coefs: hp,
		Hi:    lp,
		Poly:  poly,

		Mix:  [2]int32{32767, q315Round(tier.Gain)},
		Side: analogxPeak(fs),
	}}, nil
}

func multiBiquad(kind string, gainAmp, freq, q, fs float64, param7 bool) ([5]int32, error) {
	omega := 2 * math.Pi * freq / fs
	sinO, cosO := math.Sin(omega), math.Cos(omega)

	gain := math.Pow(10, gainAmp/20.0)
	if kind == "PEAK" || kind == "LOW_SHELF" || kind == "HIGH_SHELF" {
		gain = math.Pow(10, gainAmp/40.0)
	}
	var y float64
	switch {
	case kind == "LOW_SHELF" || kind == "HIGH_SHELF":
		y = sinO / 2 * math.Sqrt((1/gain+gain)*(1/q-1)+2)
	case param7:
		y = math.Sinh((q*math.Ln2*omega/2)/sinO) * sinO
	default:
		y = sinO / (q + q)
	}

	var a0, a1, a2, b0, b1, b2 float64
	switch kind {
	case "LP":
		a0, a1, a2 = 1+y, -2*cosO, 1-y
		b0, b1, b2 = (1-cosO)/2, 1-cosO, (1-cosO)/2
	case "HP":
		a0, a1, a2 = 1+y, -2*cosO, 1-y
		b0, b1, b2 = (1+cosO)/2, -(1 + cosO), (1+cosO)/2
	case "BP":
		a0, a1, a2 = 1+y, -2*cosO, 1-y
		b0, b1, b2 = y, 0, -y
	case "PEAK":
		a0, a1, a2 = 1+y/gain, -2*cosO, 1-y/gain
		b0, b1, b2 = 1+y*gain, -2*cosO, 1-y*gain
	default:
		return [5]int32{}, fmt.Errorf("multiBiquad: unknown kind %q", kind)
	}

	return [5]int32{
		q315Round(b0 / a0), q315Round(b1 / a0), q315Round(b2 / a0),
		q315Round(a1 / a0), q315Round(a2 / a0),
	}, nil
}

var currentAnalogX *analogxParams

func analogxAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().PolyAvailable()
}

func setAnalogX(p *analogxParams) error {
	if p == nil {
		currentAnalogX = nil
		markStateDirty()
		return nil
	}
	if !analogxAvailable() {
		return fmt.Errorf("AnalogX needs a POLY slot (CAP1 bit6 not set); this hardware cannot do it")
	}
	if p.Model < 0 || p.Model >= len(analogxTiers) {
		return fmt.Errorf("AnalogX only has levels 0/1/2 (got %d)", p.Model)
	}
	q := *p
	currentAnalogX = &q
	markStateDirty()
	return nil
}

func analogxView() map[string]any {
	if currentAnalogX == nil {
		return nil
	}
	t := analogxTiers[currentAnalogX.Model]
	return map[string]any{
		"on":         true,
		"model":      currentAnalogX.Model,
		"gain":       t.Gain,
		"lowpass_hz": t.LPHz,
		"sections":   4,
		"note":       "V4A AnalogX: HP240 -> POLY -> LP(in + poly·gain) -> Peak633(+0.58dB)",
	}
}

func analogxTiersView() []map[string]any {
	out := make([]map[string]any, 0, len(analogxTiers))
	for i, t := range analogxTiers {
		out = append(out, map[string]any{
			"model":      i,
			"gain":       t.Gain,
			"lowpass_hz": t.LPHz,

			"name_en": fxAnalogXPresetIDSuffix[i],
			"name_cn": fxAnalogXCn[i],
			"preset":  "analogx-" + fxAnalogXPresetIDSuffix[i],
		})
	}
	return out
}

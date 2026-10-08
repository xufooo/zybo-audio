// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

const (
	viperBassModeNatural   = 0
	viperBassModePBP       = 1
	viperBassModeSubwoofer = 2
)

const viperBassQ = 0.53

const viperBassPreScale = 32.0

type viperBassParams struct {
	Mode int `json:"mode"`

	CutoffHz float64 `json:"cutoff_hz"`

	Gain float64 `json:"gain"`
}

func viperBassDefaultParams() viperBassParams {
	return viperBassParams{Mode: viperBassModeNatural, CutoffHz: 40, Gain: 50}
}

var currentViPERBass *viperBassParams

func viperBassAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().Mix2Available()
}

func viperBassValidate(p viperBassParams) error {
	switch p.Mode {
	case viperBassModeNatural:
	case viperBassModePBP:

	case viperBassModeSubwoofer:
		return fmt.Errorf("ViPERBass Subwoofer tier not implemented locally (it uses the pole filter in utils/Subwoofer.cpp " +
			"which is a different structure from Natural Bass)")
	default:
		return fmt.Errorf("ViPERBass mode only accepts 0/1/2 (Natural Bass / Pure Bass+ / Subwoofer), got %d", p.Mode)
	}
	if math.IsNaN(p.CutoffHz) || p.CutoffHz < 30 || p.CutoffHz > 100 {
		return fmt.Errorf("ViPERBass cutoff %g outside panel range 30-100 Hz (vbass_freq)", p.CutoffHz)
	}
	if math.IsNaN(p.Gain) || p.Gain < 0 || p.Gain > 600 {
		return fmt.Errorf("ViPERBass gain %g outside panel range 0-600 (vbass_boost)", p.Gain)
	}
	return nil
}

func setViPERBass(p *viperBassParams) error {
	if p == nil {
		currentViPERBass = nil
		markStateDirty()
		return nil
	}
	if !viperBassAvailable() {
		return fmt.Errorf("This hardware has no MIX2 slot (CAP not set); ViPERBass dry/wet sum cannot be deployed")
	}
	if err := viperBassValidate(*p); err != nil {
		return err
	}

	if p.Mode == viperBassModePBP && !sfirAvailableNow() {
		return fmt.Errorf("ViPERBass Pure Bass+ needs a second small FIR (CAP1 bit7 = OP_SFIR,"+
			"CAP4 reports its capacity); this bitstream does not advertise it (offline/no-hardware takes this path too): the reference core's dry path"+
			"is not a bypass but a **%d-tap FIR** (taps identical to refs/viperfx-re "+
			"POLYPHASE_COEFFICIENTS_2, main tap at #31, DC -13.85 dB),"+
			"plus a lowpassed wet signal delayed by **%d samples** and scaled by bassFactor. Approximating it with biquads needs 31 sections;"+
			"while the bypass + delayed-wet alternative is already 8.4 dB off at 30 Hz, so better not to do it",
			viperBassPBPKernelLen, viperBassPBPWetDelay)
	}
	q := *p
	currentViPERBass = &q
	markStateDirty()
	return nil
}

func viperBassView() any {
	if currentViPERBass == nil {
		return nil
	}
	return currentViPERBass
}

func viperBassNodes(p viperBassParams) ([]planNode, error) {
	if err := viperBassValidate(p); err != nil {
		return nil, err
	}
	if p.Mode == viperBassModePBP {
		return viperBassPBPNodes(p)
	}
	wet := p.Gain / 100.0
	w, k := wet, viperBassPreScale
	if wet > maxMixQ315 {
		w = maxMixQ315
		k = viperBassPreScale * wet / w
	}
	b0, b1, b2, a1, a2 := rbjLowPassFloat(p.CutoffHz, viperBassQ, sampleRate)

	pre := [5]int32{floatToQ315(1.0 / viperBassPreScale), 0, 0, 0, 0}

	lp := [5]int32{
		floatToQ315(b0 * k), floatToQ315(b1 * k), floatToQ315(b2 * k),
		floatToQ315(a1), floatToQ315(a2),
	}
	for i, v := range lp {
		if v > coefMax || v < coefMin {
			return nil, fmt.Errorf("ViPERBass lowpass coefficient %d exceeds Q3.15 range (%d) --"+
				"cutoff %g Hz / gain %g cannot be expressed locally", i, v, p.CutoffHz, p.Gain)
		}
	}

	wq := int32(0)
	if w > 0 {
		wq = q315Round(w)
	}
	return []planNode{{
		Kind:  planKindViPERBass,
		Extra: pre,
		Coefs: lp,
		Mix:   [2]int32{q315Round(1.0), wq},
	}}, nil
}

func viperBassPBPNodes(p viperBassParams) ([]planNode, error) {
	wet := p.Gain / 100.0
	w, k := wet, viperBassPreScale
	if wet > maxMixQ315 {
		w = maxMixQ315
		k = viperBassPreScale * wet / w
	}
	b0, b1, b2, a1, a2 := rbjLowPassFloat(p.CutoffHz, viperBassQ, sampleRate)
	pre := [5]int32{floatToQ315(1.0 / viperBassPreScale), 0, 0, 0, 0}
	lp := [5]int32{
		floatToQ315(b0 * k), floatToQ315(b1 * k), floatToQ315(b2 * k),
		floatToQ315(a1), floatToQ315(a2),
	}
	for i, v := range lp {
		if v > coefMax || v < coefMin {
			return nil, fmt.Errorf("ViPERBass PBP lowpass coefficient %d exceeds Q3.15 range (%d) --"+
				"cutoff %g Hz / gain %g cannot be expressed locally", i, v, p.CutoffHz, p.Gain)
		}
	}

	wq := int32(0)
	if w > 0 {
		wq = q315Round(w)
	}

	sfir := make([]int32, sfirTaps)
	for i, v := range viperBassPBPKernel {
		sfir[i] = floatToQ315(v)
	}
	return []planNode{{
		Kind:  planKindViPERBassPBP,
		Len:   viperBassPBPWetDelay,
		Extra: pre,
		Coefs: lp,
		Mix:   [2]int32{q315Round(1.0), wq},
		SFir:  sfir,
	}}, nil
}

const (
	viperBassPBPKernelLen = 63
	viperBassPBPWetDelay  = 64
)

var viperBassPBPKernel = [viperBassPBPKernelLen]float64{
	-0.002339, -0.002073, -0.001940, -0.001675, -0.001515, -0.001329,
	-0.001223, -0.001037, -0.000904, -0.000851, -0.000532, -0.000851,
	-0.000106, -0.001010, 0.000558, -0.001435, 0.001302, -0.001967,
	0.002259, -0.002605, 0.003216, -0.003562, 0.004784, -0.005475,
	0.007655, -0.008506, 0.017622, -0.024639, 0.028679, -0.017303,
	-0.032507, 0.623321, 0.184702, -0.166867, 0.025729, -0.078490,
	-0.015735, -0.041199, -0.023151, -0.031524, -0.020121, -0.024985,
	-0.017303, -0.019616, -0.015018, -0.015204, -0.012838, -0.011881,
	-0.010951, -0.009516, -0.009090, -0.007788, -0.007442, -0.006353,
	-0.006087, -0.005183, -0.004970, -0.004253, -0.003987, -0.003482,
	-0.003216, -0.002871, -0.002578,
}

// SPDX-License-Identifier: GPL-2.0-only
package main

import "math"

type bq struct{ b0, b1, b2, a1, a2 float64 }

func (c bq) i32() [5]int32 {
	return [5]int32{q315Round(c.b0), q315Round(c.b1), q315Round(c.b2),
		q315Round(c.a1), q315Round(c.a2)}
}

func rbj(kind string, freq, q, fs float64) bq {
	omega := 2 * math.Pi * freq / fs
	sinO, cosO := math.Sin(omega), math.Cos(omega)
	alpha := sinO / (2 * q)

	a0 := 1 + alpha
	a1 := -2 * cosO
	a2 := 1 - alpha
	var b0, b1, b2 float64
	switch kind {
	case "LP":
		b0 = (1 - cosO) / 2
		b1 = 1 - cosO
		b2 = b0
	case "HP":
		b0 = (1 + cosO) / 2
		b1 = -(1 + cosO)
		b2 = b0
	case "BP":
		b0 = alpha
		b1 = 0
		b2 = -alpha
	}
	return bq{b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0}
}

func speakerCorrectionBiquads(fs float64) [3]bq {
	lp := rbj("LP", 13500.0, 1.0, fs)
	hp := rbj("HP", 80.0, 1.0, fs)
	bp := rbj("BP", 420.0, 3.88, fs)

	sum := bq{
		b0: 0.5 * (bp.b0 + 1),
		b1: 0.5 * (bp.b1 + bp.a1),
		b2: 0.5 * (bp.b2 + bp.a2),
		a1: bp.a1,
		a2: bp.a2,
	}
	return [3]bq{lp, hp, sum}
}

func speakerCorrectionNodes(fs float64) []planNode {
	bqs := speakerCorrectionBiquads(fs)
	out := make([]planNode, 0, len(bqs))
	for _, c := range bqs {
		out = append(out, planNode{Kind: planKindBiquad, Coefs: c.i32()})
	}
	return out
}

type speakerCorrectionParams struct{}

var currentSpeakerCorrection *speakerCorrectionParams

func speakerCorrectionAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().BiquadAvailable()
}

func setSpeakerCorrection(p *speakerCorrectionParams) error {
	if p == nil {
		currentSpeakerCorrection = nil
		markStateDirty()
		return nil
	}
	if !speakerCorrectionAvailable() {
		return errDSPUnavailable
	}
	currentSpeakerCorrection = &speakerCorrectionParams{}
	markStateDirty()
	return nil
}

func speakerCorrectionView() map[string]any {
	if currentSpeakerCorrection == nil {
		return nil
	}
	return map[string]any{
		"on":       true,
		"sections": 3,

		"lowpass_hz":  13500.0,
		"lowpass_q":   1.0,
		"highpass_hz": 80.0,
		"highpass_q":  1.0,
		"bandpass_hz": 420.0,
		"bandpass_q":  3.88,
		"note":        "V4A SpeakerCorrection：z = HP80(LP13500(x))，y = 0.5·(z + BP420(z))",
	}
}

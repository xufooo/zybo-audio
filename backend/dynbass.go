// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	dynamicBassLPHz = 55.0

	dynamicBassQDivisor = 666.0
	dynamicBassQBase    = 0.5

	dynamicBassQPeakMax = 1600.0

	dynamicBassBassMaxPct = 100.0

	dynamicBassPreScale = 256.0
)

type dynamicBassParams struct {
	Coeffs string  `json:"coeffs"`
	Bass   float64 `json:"bass"`
}

func dynamicBassDefaultParams() dynamicBassParams {
	return dynamicBassParams{Coeffs: "100;5600;40;80;50;50", Bass: 0}
}

func dynamicBassCoeffsOf(p dynamicBassParams) ([6]float64, error) {
	var c [6]float64
	parts := strings.Split(strings.TrimSpace(p.Coeffs), ";")
	if len(parts) != 6 {
		return c, fmt.Errorf("DynamicBass coeffs must be 6 numbers (\"x1;x2;x3;x4;x5;x6\"), got %q", p.Coeffs)
	}
	for i, s := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return c, fmt.Errorf("DynamicBass coeffs entry %d %q is not a number", i+1, s)
		}
		if v < 0 {
			return c, fmt.Errorf("DynamicBass coeffs entry %d = %g; client only sends non-negative integers", i+1, v)
		}
		c[i] = v
	}
	return c, nil
}

func dynamicBassBassGain(pct float64) float64 {
	return (pct*20.0 + 100.0) / 100.0
}

func dynamicBassQPeak(bassGain float64) float64 {
	x := (bassGain - 1.0) / 20.0 * 1600.0
	if x > dynamicBassQPeakMax {
		x = dynamicBassQPeakMax
	}
	if x < 0 {
		x = 0
	}
	return x
}

func dynamicBassQ(qPeak float64) float64 {
	return qPeak/dynamicBassQDivisor + dynamicBassQBase
}

func dynamicBassQForBass(pct float64) float64 {
	return dynamicBassQ(dynamicBassQPeak(dynamicBassBassGain(pct)))
}

func dynamicBassPreScaleWeight() int32 { return int32(qOne / dynamicBassPreScale) }

func dynamicBassLowPassCoefs(pct float64) ([5]int32, error) {
	q := dynamicBassQForBass(pct)
	b0, b1, b2, a1, a2 := rbjLowPassFloat(dynamicBassLPHz, q, sampleRate)
	k := dynamicBassPreScale
	c := [5]int32{
		floatToQ315(b0 * k), floatToQ315(b1 * k), floatToQ315(b2 * k),
		floatToQ315(a1), floatToQ315(a2),
	}
	for i, v := range c {
		if v > coefMax || v < coefMin {
			return c, fmt.Errorf("DynamicBass 55 Hz lowpass coefficient %d = %d out of Q3.15 range"+
				"(bass=%g => Q=%g)", i, v, pct, q)
		}
	}
	if c[0] == 0 && c[1] == 0 && c[2] == 0 {
		return c, fmt.Errorf("DynamicBass 55 Hz lowpass numerator all zero after quantization (bass=%g) -"+
			"scaling K=%g not applied, lowpass would always output 0", pct, k)
	}
	return c, nil
}

func dynamicBassValidate(p dynamicBassParams) ([6]float64, error) {
	c, err := dynamicBassCoeffsOf(p)
	if err != nil {
		return c, err
	}
	if p.Bass < 0 || p.Bass > dynamicBassBassMaxPct {
		return c, fmt.Errorf("DynamicBass bass percent must be within 0..%g, got %g"+
			"(client `dynamicsystem_bass_values` caps at %g)",
			dynamicBassBassMaxPct, p.Bass, dynamicBassBassMaxPct)
	}
	if c[0] > dynamicBassSimpleBranchMaxX1 {
		return c, fmt.Errorf("V4A DynamicBass ** full branch ** unavailable: x1 = %g > %g"+
			"(`DynamicBass.cpp:29-41`) - not implemented on this unit (can be built from existing opcodes, ~11-12 slots,"+
			"but the engine caps at 24 slots with 22 used by the baseline chain => so it does not fit the existing chain; ** not missing hardware **)."+
			"pick a preset with x1 <= %g (V4A official default \"100;5600;40;80;50;50\" is);"+
			"full branch is tracked for 0.3 (effects + UI)",
			c[0], dynamicBassSimpleBranchMaxX1, dynamicBassSimpleBranchMaxX1)
	}
	if _, err := dynamicBassLowPassCoefs(p.Bass); err != nil {
		return c, err
	}
	return c, nil
}

var currentDynamicBass *dynamicBassParams

func dynamicBassAvailable() bool {
	if dspEngineGen != 1 {
		return false
	}
	c := dspReadEngineCaps()
	return c.BiquadAvailable() && c.Mix2Available()
}

func setDynamicBass(p *dynamicBassParams) error {
	if p == nil {
		currentDynamicBass = nil
		markStateDirty()
		return nil
	}
	if _, err := dynamicBassValidate(*p); err != nil {
		return err
	}
	if !dynamicBassAvailable() {
		return fmt.Errorf("This hardware cannot run the V4A DynamicBass simple branch: need BIQUAD (CAP1 bit1)"+
			"and MIX2 (CAP1 bit3) slots, but the bitstream reports opcode bitmap %#x", dspReadEngineCaps().Opcodes)
	}
	q := *p
	currentDynamicBass = &q
	markStateDirty()
	return nil
}

func dynamicBassNodes(p dynamicBassParams) ([]planNode, error) {
	if _, err := dynamicBassValidate(p); err != nil {
		return nil, err
	}
	lp, err := dynamicBassLowPassCoefs(p.Bass)
	if err != nil {
		return nil, err
	}
	return []planNode{{Kind: planKindDynBass, Coefs: lp}}, nil
}

const (
	dynamicBassSlots      = 4
	dynamicBassSections   = 3
	dynamicBassCoefWords  = 15
	dynamicBassSideLagSmp = 1
)

func dynamicBassView() any {
	if currentDynamicBass == nil {
		return nil
	}
	p := *currentDynamicBass
	c, _ := dynamicBassCoeffsOf(p)
	q := dynamicBassQForBass(p.Bass)
	lp, _ := dynamicBassLowPassCoefs(p.Bass)
	return map[string]any{
		"coeffs": p.Coeffs, "bass": p.Bass,
		"x1": c[0], "branch": dynamicBassBranchOf(c[0]),
		"lowpass_hz": dynamicBassLPHz, "q": q,
		"q_peak":             dynamicBassQPeak(dynamicBassBassGain(p.Bass)),
		"bass_gain":          dynamicBassBassGain(p.Bass),
		"lowpass_coefs_q315": []int32{lp[0], lp[1], lp[2], lp[3], lp[4]},
		"pre_scale_k":        dynamicBassPreScale,
		"available":          dynamicBassAvailable(),
		"slots":              dynamicBassSlots, "sections": dynamicBassSections,
		"coef_words": dynamicBassCoefWords,
		"inert_coeffs": "x2..x6 only matter for the full branch (x1>120), unused by the simple branch;" +
			"still stored here so switching presets loses nothing",
		"side_channel_lag_samples": dynamicBassSideLagSmp,
		"side_channel_lag_note": "Frame n: R pass reads same-frame L[n] (0-sample lag) => right channel bit-equals V4A;" +
			"L pass reads previous-frame R[n-1] (1-sample lag = 0.4125deg at 55 Hz) => R in the left side signal lags by 1 sample." +
			"basis = per-pass measurement in fpga/src/tb/tb_engine_xphase.v",
		"source": "refs/viperfx-re/src/viper/utils/DynamicBass.cpp:11-28 (simple branch); " +
			"Biquad.cpp:91-107 (55 Hz lowpass); " +
			"ViPER4AndroidService.java:1825-1839 (parameter convention)",
	}
}

func dynBassSideLagDegrees() float64 {
	return 360.0 * dynamicBassLPHz / sampleRate
}

func dynamicBassFullBranchBlocker() string {
	return "Full branch (x1 > 120) not implemented yet: can be built from existing opcodes (~11-12 slots)," +
		"but the engine caps at 24 slots with 22 used by the baseline chain, so it does not fit. ** No new hardware needed **, tracked for 0.3."
}

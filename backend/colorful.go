// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

type colorfulParams struct {
	Depth    int     `json:"depth"`
	Widening float64 `json:"widening"`
	MidImage float64 `json:"mid_image"`
}

const (
	colorfulWidenMin = 0.0
	colorfulWidenMax = 4.0

	colorfulMidMin   = 0.0
	colorfulMidMax   = 4.0
	colorfulDepthMax = 1000

	colorfulDepthMin = 1
)

func colorfulDefaultParams() colorfulParams {
	return colorfulParams{Depth: 200, Widening: 1.2, MidImage: 1.5}
}

var currentColorful *colorfulParams

func colorfulAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().JointStereoAvailable()
}

func colorfulDepthGain(depth int) (float64, float64) {
	if depth <= 0 {
		return 0, 0
	}
	g := math.Pow(10, ((float64(depth)/1000.0)*10.0-15.0)/20.0)
	if g > 1.0 {
		g = 1.0
	}
	if depth >= 500 {
		return g, -g
	}
	return g, g
}

func colorfulHP800(fs float64) (b0, b1, b2, a1, a2 float64) {
	const freq, dbGain, q = 800.0, -11.0, 0.72
	omega := 2.0 * math.Pi * freq / fs
	sinOmega, cosOmega := math.Sin(omega), math.Cos(omega)
	A := math.Pow(10.0, dbGain/40.0)
	sqrtA := math.Sqrt(A)
	z := sinOmega / 2.0 * math.Sqrt((1.0/A+A)*(1.0/q-1.0)+2.0)
	a0 := (A + 1.0) - (A-1.0)*cosOmega + (sqrtA*2.0)*z
	ra1 := ((A - 1.0) - (A+1.0)*cosOmega) * 2.0
	ra2 := (A + 1.0) - (A-1.0)*cosOmega - (sqrtA*2.0)*z
	rb0 := ((A + 1.0) + (A-1.0)*cosOmega + (sqrtA*2.0)*z) * A * omega
	rb1 := A * -2.0 * ((A - 1.0) + (A+1.0)*cosOmega) * omega
	rb2 := ((A + 1.0) + (A-1.0)*cosOmega - (sqrtA*2.0)*z) * A * omega

	return rb0 / a0, rb1 / a0, rb2 / a0, -(ra1 / a0), -(ra2 / a0)
}

func colorfulMatrix(widen, midImage float64) (float64, float64) {
	tmp := widen + 1.0
	x := tmp + 1.0
	y := 0.5
	if x >= 2.0 {
		y = 1.0 / x
	}
	cL := midImage * y
	cR := tmp * y
	return cL + cR, cL - cR
}

func colorfulJointCoefs(p colorfulParams, fs float64) ([9]int32, [2]int32, error) {
	var jsd [9]int32
	var js3 [2]int32
	q := func(name string, v float64) (int32, error) {
		n := floatToQ315(v)
		if n > coefMax || n < coefMin {
			return 0, fmt.Errorf("ColorfulMusic %s = %.6f exceeds Q3.15 range (%d)", name, v, n)
		}
		return n, nil
	}
	g, g1 := colorfulDepthGain(p.Depth)
	b0, b1, b2, a1, a2 := colorfulHP800(fs)
	ca, cb := colorfulMatrix(p.Widening, p.MidImage)
	l0 := int(fs * 0.02)
	l1 := int(fs * 0.014)
	if l0 < 1 || l1 < 1 {
		return jsd, js3, fmt.Errorf("At sample rate %.0f the two delay lengths compute to %d/%d (<1)", fs, l0, l1)
	}
	var err error
	for _, v := range []struct {
		name string
		val  float64
		dst  *int32
	}{
		{"g", g, &jsd[0]}, {"g1", g1, &jsd[1]},
		{"HP800 b0", b0, &jsd[4]}, {"HP800 b1", b1, &jsd[5]}, {"HP800 b2", b2, &jsd[6]},
		{"HP800 a1", a1, &jsd[7]}, {"HP800 a2", a2, &jsd[8]},
		{"3D matrix ca", ca, &js3[0]}, {"3D matrix cb", cb, &js3[1]},
	} {
		if *v.dst, err = q(v.name, v.val); err != nil {
			return jsd, js3, err
		}
	}
	jsd[2], jsd[3] = int32(l0), int32(l1)

	if l0 > coefMax || l1 > coefMax {
		return jsd, js3, fmt.Errorf("Delay lengths %d/%d do not fit in 18-bit coefficients", l0, l1)
	}
	return jsd, js3, nil
}

func colorfulJointDelayWords(fs float64) int {
	l0, l1 := int(fs*0.02), int(fs*0.014)
	if l0 < l1 {
		l0 = l1
	}
	if l0 < 1 {
		l0 = 1
	}
	return l0
}

func colorfulNodes(p colorfulParams) ([]planNode, error) {
	jsd, js3, err := colorfulJointCoefs(p, sampleRate)
	if err != nil {
		return nil, err
	}
	return []planNode{{Kind: planKindColorfulMusic, Joint: jsd, Mix3D: js3}}, nil
}

func setColorfulMusic(p *colorfulParams) error {
	if p == nil {
		currentColorful = nil
		markStateDirty()
		return nil
	}
	if !colorfulAvailable() {
		return fmt.Errorf("This hardware has no joint-stereo frame pass (CAP1 bit12 not set):" +
			"ColorfulMusic DepthSurround is a joint-stereo processor (needs L and R in the same frame)" +
			"that a per-channel engine cannot run; cannot deploy")
	}
	q, err := colorfulValidate(*p)
	if err != nil {
		return err
	}
	currentColorful = &q
	markStateDirty()
	return nil
}

func colorfulValidate(p colorfulParams) (colorfulParams, error) {
	if p.Depth < 0 {
		p.Depth = 0
	}
	if p.Depth > colorfulDepthMax {
		return p, fmt.Errorf("ColorfulMusic depth=%d exceeds core range 0..%d"+
			"(reference client panel is 200..800)", p.Depth, colorfulDepthMax)
	}
	if p.Widening < colorfulWidenMin {
		p.Widening = colorfulWidenMin
	}
	if p.Widening > colorfulWidenMax {
		return p, fmt.Errorf("ColorfulMusic widening=%.2f outside accepted range %.1f..%.1f"+
			"(reference client panel is 1.2..2.0)", p.Widening, colorfulWidenMin, colorfulWidenMax)
	}
	if p.MidImage < colorfulMidMin {
		p.MidImage = colorfulMidMin
	}
	if p.MidImage > colorfulMidMax {
		return p, fmt.Errorf("ColorfulMusic mid_image=%.2f outside accepted range %.1f..%.1f"+
			"(reference client panel is 1.0..2.0)", p.MidImage, colorfulMidMin, colorfulMidMax)
	}
	if p.Depth != 0 && p.Depth < colorfulDepthMin {
		p.Depth = colorfulDepthMin
	}
	return p, nil
}

func colorfulView() any {
	if currentColorful == nil {
		return nil
	}
	p := *currentColorful
	g, g1 := colorfulDepthGain(p.Depth)
	ca, cb := colorfulMatrix(p.Widening, p.MidImage)
	l0 := int(sampleRate * 0.02)
	l1 := int(sampleRate * 0.014)
	return map[string]any{
		"depth":     p.Depth,
		"widening":  p.Widening,
		"mid_image": p.MidImage,
		"available": colorfulAvailable(),

		"gain": g, "gain_prev1": g1,
		"matrix_a": ca, "matrix_b": cb,
		"delay0_samples": l0, "delay1_samples": l1,

		"client_widening": int(math.Round(p.Widening * 100)),
		"client_midimage": int(math.Round(p.MidImage * 100)),
		"note": "V4A ColorfulMusic = DepthSurround (joint stereo: shared prev0/prev1 + " +
			"two 960/672-sample delays + HP800) -> Stereo3DSurround (2x2 matrix)." +
			"Parameter convention: client sends widening/depth (coeffs " +
			"\"120;200\") and midimage (\"150\"); core divides by 100. Only active in the Headphone FX tier.",
	}
}

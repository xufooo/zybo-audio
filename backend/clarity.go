// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

const (
	clarityModeNatural = 0
	clarityModeOzone   = 1
	clarityModeXHIFI   = 2
)

type clarityParams struct {
	Mode  int     `json:"mode"`
	Level float64 `json:"level"`
}

func clarityDefaultParams() clarityParams { return clarityParams{Mode: clarityModeOzone, Level: 50} }

func clarityModeFromString(s string) (int, bool) {
	switch normalizeChainType(s) {
	case "natural", "noisesharpening", "noise_sharpening":
		return clarityModeNatural, true
	case "ozone":
		return clarityModeOzone, true
	case "xhifi", "x-hifi", "hi-fi", "hifi":
		return clarityModeXHIFI, true
	}
	return 0, false
}

func clarityModeName(m int) string {
	switch m {
	case clarityModeNatural:
		return "natural"
	case clarityModeOzone:
		return "ozone"
	case clarityModeXHIFI:
		return "xhifi"
	}
	return fmt.Sprintf("unknown(%d)", m)
}

func highShelfOzone(freq, fs, gainLinear float64) [5]int32 {

	gainDB := 20 * math.Log10(gainLinear)
	x := 2 * math.Pi * freq / fs
	sinX, cosX := math.Sin(x), math.Cos(x)
	y := math.Exp(gainDB * math.Ln10 / 40.0)
	z := math.Sqrt(2*y) * sinX
	a := (y - 1) * cosX
	b := (y + 1) - a
	c := z + b
	d := (y + 1) * cosX
	e := (y + 1) + a
	f := (y - 1) - d

	a0 := 1 / c

	b0 := (e + z) * y * a0
	b1 := -y * 2 * ((y - 1) + d) * a0
	b2 := (e - z) * y * a0
	a1 := 2 * f * a0
	a2 := (b - z) * a0
	return [5]int32{q315Round(b0), q315Round(b1), q315Round(b2), q315Round(a1), q315Round(a2)}
}

func noiseSharpeningBiquad(fs, g float64) [5]int32 {
	l0, _, la := iir1LPF_BW(fs/2.0-1000.0, fs)

	b0 := l0 * (1 + g)
	b1 := l0
	b2 := -l0 * g
	return [5]int32{q315Round(b0), q315Round(b1), q315Round(b2), q315Round(la), 0}
}

func clarityNodes(p clarityParams, fs float64) ([]planNode, error) {
	g := p.Level / 100.0
	if g < 0 {
		g = 0
	}
	switch p.Mode {
	case clarityModeNatural:
		return []planNode{{Kind: planKindBiquad, Coefs: noiseSharpeningBiquad(fs, g)}}, nil
	case clarityModeOzone:

		return []planNode{{Kind: planKindBiquad, Coefs: highShelfOzone(8250.0, fs, g+1.0)}}, nil
	case clarityModeXHIFI:

		n, err := xhifiNode(p.Level, fs)
		if err != nil {
			return nil, err
		}
		return []planNode{n}, nil
	}
	return nil, fmt.Errorf("Unknown Clarity tier %d", p.Mode)
}

var currentClarity *clarityParams

func clarityAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().BiquadAvailable()
}

func setClarity(p *clarityParams) error {
	if p == nil {
		currentClarity = nil
		markStateDirty()
		return nil
	}
	if p.Level < 0 {
		p.Level = 0
	}

	if p.Level > 200 {
		return fmt.Errorf("Clarity range 0..200, got %g", p.Level)
	}
	if p.Mode != clarityModeNatural && p.Mode != clarityModeOzone && p.Mode != clarityModeXHIFI {
		return fmt.Errorf("Unknown Clarity tier %d", p.Mode)
	}
	cur := *p
	currentClarity = &cur
	markStateDirty()
	return nil
}

func clarityView() map[string]any {
	if currentClarity == nil {
		return nil
	}
	return map[string]any{
		"mode":     clarityModeName(currentClarity.Mode),
		"mode_id":  currentClarity.Mode,
		"level":    currentClarity.Level,
		"gain_db":  clarityGainDB(*currentClarity),
		"sections": 1,
		"note":     "V4A ViPERClarity; all three tiers implemented (natural / ozone / xhifi)",
	}
}

func clarityGainDB(p clarityParams) float64 {
	g := p.Level / 100.0
	if p.Mode == clarityModeOzone {
		return 20 * math.Log10(g+1)
	}
	return 20 * math.Log10(1+2*g)
}

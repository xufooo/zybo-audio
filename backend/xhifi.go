// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

func xhifiSections(fs float64) (hp, bpL, bpH, lp [3][5]int32) {
	h0, h1, ha := iir1HPF_BW(1200.0, fs)
	lh0, lh1, lha := iir1HPF_BW(120.0, fs)
	ll0, ll1, lla := iir1LPF_BW(1200.0, fs)
	pl0, pl1, pla := iir1LPF_BW(120.0, fs)
	for i := 0; i < 3; i++ {
		hp[i] = biquadFrom1st(h0, h1, ha)
		bpL[i] = biquadFrom1st(ll0, ll1, lla)
		bpH[i] = biquadFrom1st(lh0, lh1, lha)
	}
	lp[0] = biquadFrom1st(pl0, pl1, pla)
	return
}

func xhifiNode(level, fs float64) (planNode, error) {
	if level < 0 || level > 100 {
		return planNode{}, fmt.Errorf("Clarity range 0..100 (native V4A panel values), got %g", level)
	}
	gain := level/100.0 + 1.0
	hp, bpL, bpH, lp := xhifiSections(fs)
	return planNode{
		Kind:  planKindXHIFI,
		Coefs: hp[0],
		Hi:    bpL[0],
		Side:  bpH[0],
		Extra: lp[0],

		Mix: [2]int32{q315Round(1.2 * gain), q315Round(gain)},

		MixB: [2]int32{q315Round(1.0), q315Round(1.0)},

		Flags: int(math.Round(fs / 400.0)),
		Len:   int(math.Round(fs / 200.0)),
	}, nil
}

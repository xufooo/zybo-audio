// SPDX-License-Identifier: GPL-2.0-only
package main

import "testing"

func TestEffectCardsSlotsMatchDocs(t *testing.T) {
	fakeHardware(t, 0xFFFFFFFF)

	withCapDelaySlots(t, 24)

	eq10 := func() []planNode {
		out := make([]planNode, 0, 10)
		for i := 0; i < 10; i++ {
			out = append(out, planNode{Kind: planKindBiquad,
				Coefs: [5]int32{32768, 0, 0, 0, 0}})
		}
		return out
	}

	eqGains := func(gs []float64) []planNode {
		if len(gs) != 10 {
			panic("EQ must have 10 bands")
		}
		return eq10()
	}
	must := func(n []planNode, err error) []planNode {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	dynGain := func(g float64) planNode {
		p := dynDefaultParams()
		p.GainDB = g
		c, err := dynParamsToCoefs(p)
		if err != nil {
			t.Fatal(err)
		}
		return planNode{Kind: planKindDyn, Coefs: c, Side: dynSideChainCoefs()}
	}
	vseOf := func(gear float64) []planNode {
		vp, err := vseExciterParams(gear)
		if err != nil {
			t.Fatal(err)
		}
		return must(exciterNodes(vp))
	}
	dynFull := func() planNode {
		c, err := dynParamsToCoefs(dynParams{GainDB: 6, CutDB: 0, RefDB: -25,
			KS: 0.75, AttMs: 5, RelMs: 200})
		if err != nil {
			t.Fatal(err)
		}
		return planNode{Kind: planKindDyn, Coefs: c, Side: dynSideChainCoefs()}
	}
	crossOf := func(fcut, feed float64) planNode {
		lo, hi, mix, err := crossfeedCoefs(crossfeedParams{FcutHz: fcut, Feed: feed})
		if err != nil {
			t.Fatal(err)
		}
		return planNode{Kind: planKindCross, Coefs: lo, Hi: hi, Mix: mix}
	}
	bassOf := func(mode int, fc, g float64) []planNode {
		return must(viperBassNodes(viperBassParams{Mode: mode, CutoffHz: fc, Gain: g}))
	}
	clarOf := func(mode int, lv float64) []planNode {
		return must(clarityNodes(clarityParams{Mode: mode, Level: lv}, 48000))
	}
	colOf := func(widening float64, depth int, mid float64) []planNode {
		return must(colorfulNodes(colorfulParams{Depth: depth, Widening: widening, MidImage: mid}))
	}

	cases := []struct {
		name  string
		nodes []planNode
		want  int
	}{
		{"ClearPenguin",
			append([]planNode{dynGain(5)}, eq10()...), 6},
		{"Headphones Basic Crossfeed",
			[]planNode{crossOf(650, 95)}, 3},
		{"Dynamic Punch",
			append(append(eq10(), must(clarityNodes(clarityParams{Mode: clarityModeNatural, Level: 50}, 48000))...), colOf(1.5, 425, 1.6)...), 6},
		{"LoongFX Music",
			append(append(append(append(vseOf(0.6), dynFull()), eq10()...), bassOf(0, 57, 150)...), must(clarityNodes(clarityParams{Mode: clarityModeNatural, Level: 50}, 48000))...), 14},
		{"bass",
			append(eq10(), bassOf(0, 60, 300)...), 6},
		{"bass_clarity",
			append(append(eq10(), bassOf(0, 60, 300)...), clarOf(clarityModeXHIFI, 50)...), 14},
		{"dynamic_bass",
			append(must(dynamicBassNodes(dynamicBassParams{Coeffs: "100;5600;40;80;50;50", Bass: 0})), eq10()...), 8},
		{"clarity",
			append(append(vseOf(0.3), eq10()...), must(clarityNodes(clarityParams{Mode: clarityModeNatural, Level: 50}, 48000))...), 9},
		{"headphone_surround",
			append(append(eq10(), crossOf(700, 60)), colOf(1.8, 650, 2.0)...), 9},
		{"soundstage",
			append(eq10(), planNode{Kind: planKindDelay, Len: surroundDelaySamples(surroundParams{DelayMs: 20}), Flags: flDlyRight}), 5},
		{"tube",
			append(append(eq10(), tubeNodes()...), must(analogxNodes(analogxParams{Model: 0}, 48000))...), 9},
		{"loudness",
			[]planNode{dynGain(6)}, 2},
		{"Best-headset",
			append(append(append(eq10(), bassOf(0, 60, 150)...), clarOf(clarityModeNatural, 50)...), must(analogxNodes(analogxParams{Model: 1}, 48000))...), 12},
		{"Audiophile-headset",
			append(bassOf(0, 40, 200), clarOf(clarityModeNatural, 100)...), 3},
		{"AKG_BASS",
			append(bassOf(0, 96, 350), clarOf(clarityModeNatural, 50)...), 3},
		{"JBL C100Si",
			append(bassOf(0, 40, 150), clarOf(clarityModeOzone, 50)...), 3},
		{"All_Rounder",
			append(append(append(append(vseOf(1.0), eqGains([]float64{3.0, 6.0, 3.5, 0, 0, 0, 0, 0, 0, -0.5})...), bassOf(0, 80, 200)...), clarOf(clarityModeOzone, 150)...), must(analogxNodes(analogxParams{Model: 1}, 48000))...), 17},
	}
	for _, c := range cases {
		plan, err := buildSlotPlanNodes(c.nodes)
		if err != nil {
			t.Errorf("%s: planning failed: %v", c.name, err)
			continue
		}
		t.Logf("%s: slots=%d (card %d) sections=%d coefs=%d",
			c.name, plan.Slots, c.want, plan.Sections, len(plan.Coefs))
		if plan.Slots != c.want {
			t.Errorf("%s: slots %d differ from card %d (card or compiler changed)",
				c.name, plan.Slots, c.want)
		}
	}
}

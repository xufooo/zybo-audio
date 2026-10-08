// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
)

var pbpKernelQ315 = [63]int32{
	-77, -68, -64, -55, -50, -44,
	-40, -34, -30, -28, -17, -28,
	-3, -33, 18, -47, 43, -64,
	74, -85, 105, -117, 157, -179,
	251, -279, 577, -807, 940, -567,
	-1065, 20425, 6052, -5468, 843, -2572,
	-516, -1350, -759, -1033, -659, -819,
	-567, -643, -492, -498, -421, -389,
	-359, -312, -298, -255, -244, -208,
	-199, -170, -163, -139, -131, -114,
	-105, -94, -84,
}

func TestViPERBassPBPKernelMatchesModel(t *testing.T) {
	nodes, err := viperBassNodes(viperBassParams{Mode: viperBassModePBP, CutoffHz: 40, Gain: 50})
	if err != nil {
		t.Fatalf("viperBassNodes(PBP)：%v", err)
	}
	if len(nodes) != 1 || nodes[0].Kind != planKindViPERBassPBP {
		t.Fatalf("PBP should compile to one PBP node, got %+v", nodes)
	}
	sf := nodes[0].SFir
	if len(sf) != sfirTaps {
		t.Fatalf("dry-path taps should be padded to %d, got %d", sfirTaps, len(sf))
	}
	for i, want := range pbpKernelQ315 {
		if sf[i] != want {
			t.Errorf("tap %d: Go downloads %d, reference model %d -- "+
				"when they disagree, a green tb_engine_sfir only proves the RTL computed **another** tap set", i, sf[i], want)
		}
	}

	for i := 0; i < viperBassPBPKernelLen; i++ {
		if viperBassPBPKernel[i] == 0 && sf[i] != 0 {
			t.Errorf("core tap %d is 0, should quantize to 0, got %d", i, sf[i])
		}
	}

	if sf[63] != 0 {
		t.Errorf("padding tap h[63] must be exactly 0, got %d", sf[63])
	}

	var sum float64
	for _, v := range viperBassPBPKernel {
		sum += v
	}
	if db := 20 * math.Log10(sum); math.Abs(db+13.85) > 0.05 {
		t.Errorf("dry-path DC gain %+.2f dB, measured -13.85 dB", db)
	}
	t.Logf("PBP dry path: %d Q3.15 taps (+1 zero pad), sum_h_q = %d, DC %+.2f dB",
		viperBassPBPKernelLen, func() int {
			s := int32(0)
			for _, v := range sf {
				s += v
			}
			return int(s)
		}(),
		20*math.Log10(sum))
}

func pbpPlan(t *testing.T, p viperBassParams) slotPlan {
	t.Helper()

	saveTaps, saveOvh := hwSFIRMaxTaps, hwSFIROvh
	defer func() { hwSFIRMaxTaps, hwSFIROvh = saveTaps, saveOvh }()
	hwSFIRMaxTaps, hwSFIROvh = sfirTaps, sfirOvh

	nodes, err := viperBassNodes(p)
	if err != nil {
		t.Fatalf("failed to compile ViPERBass(%+v): %v", p, err)
	}
	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("failed to plan ViPERBass(%+v): %v", p, err)
	}
	return plan
}

func TestViPERBassPBPSlotExpansion(t *testing.T) {
	const in0 = 7
	saveTaps := hwSFIRMaxTaps
	defer func() { hwSFIRMaxTaps = saveTaps }()
	hwSFIRMaxTaps = sfirTaps

	p, err := buildSlotPlanNodes([]planNode{
		{Kind: planKindViPERBassPBP,
			Len:   viperBassPBPWetDelay,
			Extra: [5]int32{1024, 0, 0, 0, 0},
			Coefs: [5]int32{7, 14, 7, -65213, 32446},
			Mix:   [2]int32{32768, 16384},
			SFir:  make([]int32, sfirTaps)},
	})
	if err != nil {
		t.Fatalf("buildSlotPlanNodes(PBP)：%v", err)
	}
	if p.Slots != 4 {
		t.Fatalf("PBP should take **4 slots** (delay/biquad/small-FIR/MIX2), got %d", p.Slots)
	}
	if p.Sections != 3 {
		t.Errorf("PBP should take **3 sections** (scaling+lowpass+MIX2 accounting; small FIR and delay take none), got %d",
			p.Sections)
	}
	slots := decodeSlots(t, p)
	wantOp := []int{opDelay, opBiquad, opSFIR, opMix2}
	for i, op := range wantOp {
		if slots[i].Op != op {
			t.Errorf("slot %d opcode = %d, want %d (slot order = delay->biquad->small-FIR->MIX2)",
				i, slots[i].Op, op)
		}
	}

	if got := p.Coefs[0]; got != viperBassPBPWetDelay {
		t.Errorf("wet delay = %d, measured %d (aligned RMS=0.0; 63/65 both 0.59)",
			got, viperBassPBPWetDelay)
	}
	if slots[0].In != 0 || slots[0].Out != 1 {
		t.Errorf("delay slot buses = %d -> %d, want 0 -> 1", slots[0].In, slots[0].Out)
	}

	if slots[1].N != 2 {
		t.Errorf("biquad slot section count = %d, want 2 (scaling + lowpass)", slots[1].N)
	}
	if slots[1].In != 1 || slots[1].Out != 2 {
		t.Errorf("biquad slot buses = %d -> %d, want 1 -> 2", slots[1].In, slots[1].Out)
	}

	if slots[2].N != 3 {
		t.Errorf("small FIR slot n = %d, want 3 (= log2(sfirTaps/sfirMACS) = log2(8))", slots[2].N)
	}
	if slots[2].In != 0 || slots[2].Out != 3 {
		t.Errorf("small FIR slot buses = %d -> %d, want 0 -> 3 (dry path takes the **raw bus**, "+
			"chaining after the lowpass would leave 'only low frequencies')", slots[2].In, slots[2].Out)
	}

	if slots[3].In != 3 || slots[3].InB != 2 || slots[3].Out != 2 {
		t.Errorf("MIX2 buses = dry%d / wet%d / out%d, want dry3 / wet2 / out2",
			slots[3].In, slots[3].InB, slots[3].Out)
	}

	if len(p.Coefs) != 20 {
		t.Errorf("PBP biquad coefficient space should take 20 words (5+10+5), got %d", len(p.Coefs))
	}

	if len(p.SFirCoefs) != 2*sfirTaps {
		t.Fatalf("small FIR taps should be stored separately as 2x%d (one copy per channel), got %d",
			sfirTaps, len(p.SFirCoefs))
	}
	for i := 0; i < sfirTaps; i++ {
		if p.SFirCoefs[i] != p.SFirCoefs[sfirTaps+i] {
			t.Errorf("channel 0 and channel 1 tap %d differ (%d vs %d) -- "+
				"reference core uses one polyphase table for both channels",
				i, p.SFirCoefs[i], p.SFirCoefs[sfirTaps+i])
		}
	}

	for _, s := range slots {
		for _, b := range []int{s.In, s.InB, s.Out} {
			if b != 0xFFFF && b >= crossLoBus {
				t.Errorf("slot uses reserved bus number %d (reserved numbers start at %d)", b, crossLoBus)
			}
		}
	}
}

func TestViPERBassPBPWetWeights(t *testing.T) {
	cases := []struct {
		gain     float64
		wantWet  int32
		wantFold bool
	}{
		{0, 0, false},
		{50, 16385, false},
		{400, 131071, false},
		{600, 131071, true},
	}
	for _, c := range cases {
		plan := pbpPlan(t, viperBassParams{Mode: viperBassModePBP, CutoffHz: 40, Gain: c.gain})

		n := len(plan.Coefs)
		dry, wet := plan.Coefs[n-5], plan.Coefs[n-4]
		if dry != q315Round(1.0) {
			t.Errorf("gain=%g: dry weight = %d, want 1.0 (%d) -- dry path is the source shaped by the small FIR",
				c.gain, dry, q315Round(1.0))
		}
		if wet != c.wantWet {
			t.Errorf("gain=%g: wet weight = %d, want %d (bassFactor = gain/100)", c.gain, wet, c.wantWet)
		}

		if c.wantFold {
			if plan.Coefs[10] <= floatToQ315(0) {
				t.Errorf("gain=%g: wet excess over the MIX2 range should fold into the lowpass numerator, "+
					"but lowpass b0 = %d (looks unfolded)", c.gain, plan.Coefs[10])
			}
		}
	}

	if _, err := viperBassNodes(viperBassParams{Mode: viperBassModePBP, CutoffHz: 40, Gain: -1}); err == nil {
		t.Error("gain = -1 outside the panel range, must fail")
	}
}

func TestViPERBassPBPRequiresSmallFIR(t *testing.T) {
	saveTaps := hwSFIRMaxTaps
	defer func() { hwSFIRMaxTaps = saveTaps }()
	hwSFIRMaxTaps = 0

	nodes, err := viperBassNodes(viperBassParams{Mode: viperBassModePBP, CutoffHz: 40, Gain: 50})
	if err != nil {

		t.Logf("node layer already rejected: %v", err)
		return
	}
	_, err = buildSlotPlanNodes(nodes)
	if err == nil {
		t.Fatal("without small FIR in the bitstream, PBP must be rejected (else the dry path cannot download, leaving only wet + bypass)")
	}
	if !containsAny(err.Error(), "small FIR", "OP_SFIR") {
		t.Errorf("rejection should say **small FIR** is missing, got: %v", err)
	}
	t.Logf("rejected as expected: %v", err)
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

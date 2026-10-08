// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"math/cmplx"
	"strings"
	"testing"
)

func v4aBiquadLowPass(frequency, samplingRate, qFactor float64) (b0, b1, b2, a1, a2 float64) {
	omega := 2.0 * math.Pi * frequency / samplingRate
	sinOmega := math.Sin(omega)
	cosOmega := math.Cos(omega)
	alpha := sinOmega / (qFactor + qFactor)
	ra0 := alpha + 1.0
	ra1 := cosOmega * -2.0
	ra2 := 1.0 - alpha
	rb0 := (1.0 - cosOmega) / 2.0
	rb1 := 1.0 - cosOmega
	rb2 := (1.0 - cosOmega) / 2.0

	return rb0 / ra0, rb1 / ra0, rb2 / ra0, ra1 / ra0, ra2 / ra0
}

func TestDynamicBassLowPassIsV4ABiquad(t *testing.T) {
	for _, pct := range []float64{0, 10, 30, 50, 100} {
		q := dynamicBassQForBass(pct)
		wb0, wb1, wb2, wa1, wa2 := v4aBiquadLowPass(dynamicBassLPHz, sampleRate, q)
		gb0, gb1, gb2, ga1, ga2 := rbjLowPassFloat(dynamicBassLPHz, q, sampleRate)
		for i, pair := range [][2]float64{{wb0, gb0}, {wb1, gb1}, {wb2, gb2}, {wa1, ga1}, {wa2, ga2}} {
			if math.Abs(pair[0]-pair[1]) > 1e-15 {
				t.Errorf("bass=%g (Q=%g) %d coefficients: V4A %g vs this machine %g", pct, q, i, pair[0], pair[1])
			}
		}

		dc := (gb0 + gb1 + gb2) / (1 + ga1 + ga2)
		if math.Abs(dc-1) > 1e-9 {
			t.Errorf("bass=%g: lowpass DC gain %g != 1", pct, dc)
		}
	}
}

func TestDynamicBassQMatchesSource(t *testing.T) {

	if q := dynamicBassQForBass(0); q != 0.5 {
		t.Errorf("bass=0 when Q must is 0.5 (qPeak=0), got %g", q)
	}
	if g := dynamicBassBassGain(0); g != 1.0 {
		t.Errorf("bass=0 when bassGain must is 1.0, got %g", g)
	}

	wantQ := 1600.0/666.0 + 0.5
	if q := dynamicBassQForBass(100); math.Abs(q-wantQ) > 1e-12 {
		t.Errorf("bass=100 when Q = %g, expect %g", q, wantQ)
	}

	for _, pct := range []float64{5, 12, 30, 60, 90} {
		qp := dynamicBassQPeak(dynamicBassBassGain(pct))
		if math.Abs(qp-16*pct) > 1e-9 {
			t.Errorf("bass=%g: qPeak = %g, expect %g (= 16*bass)", pct, qp, 16*pct)
		}
		if q := dynamicBassQForBass(pct); math.Abs(q-(qp/666+0.5)) > 1e-12 {
			t.Errorf("bass=%g: Q = %g, expect qPeak/666+0.5 = %g", pct, q, qp/666+0.5)
		}
	}

	if qp := dynamicBassQPeak(99); qp > 1600 {
		t.Errorf("qPeak must clamped high at in 1600 (DynamicBass.cpp:54-56), got %g", qp)
	}

	if dynamicBassLPHz != 55.0 {
		t.Errorf("lowpass cutoff must is 55 Hz, got %g", dynamicBassLPHz)
	}
}

func TestDynamicBassLowPassQuantisedGain(t *testing.T) {
	c, err := dynamicBassLowPassCoefs(0)
	if err != nil {
		t.Fatalf("compute coefficients failed: %v", err)
	}
	if c[0] == 0 || c[1] == 0 || c[2] == 0 {
		t.Fatalf("numerator must not quantize to 0 (scaling K=%g not applied): %v", dynamicBassPreScale, c)
	}

	b0, _, _, _, _ := rbjLowPassFloat(dynamicBassLPHz, 0.5, sampleRate)
	for i, want := range []float64{b0 * dynamicBassPreScale * qOne,
		2 * b0 * dynamicBassPreScale * qOne, b0 * dynamicBassPreScale * qOne} {
		if got := float64(c[i]); math.Abs(got-want) > 1.0 {
			t.Errorf("numerator %d = %v, expect ~= %g (reason think coefficients x K then rounded)", i, got, want)
		}
	}

	denom := float64(qOne+c[3]+c[4]) / 32768.0
	_, _, _, ia1, ia2 := rbjLowPassFloat(dynamicBassLPHz, 0.5, sampleRate)
	ideal := 1 + ia1 + ia2
	if math.Abs(denom-2.0/32768.0) > 1e-12 {
		t.Errorf("after quantization 1+a1+a2 = %g, expect 2 LSB = %g", denom, 2.0/32768.0)
	}
	if !(denom > ideal) {
		t.Errorf("after quantization denominator %g should greater than reason think value %g (is low-frequency dip 1 dB reason)", denom, ideal)
	}

	table := []struct {
		hz   float64
		want float64
	}{
		{10, -1.36}, {20, -1.06}, {30, -0.69}, {55, -0.02},
		{80, +0.19}, {120, +0.19}, {200, +0.11}, {400, +0.04},
	}
	for _, c0 := range table {

		got := 20 * math.Log10(cmplx.Abs(biquadResponse(c, c0.hz, sampleRate))/dynamicBassPreScale/
			cmplx.Abs(biquadResponseV4A(dynamicBassLPHz, 0.5, c0.hz)))
		if math.Abs(got-c0.want) > 0.03 {
			t.Errorf("%g Hz: post-quantization deviation %+.2f dB, test table records %+.2f dB", c0.hz, got, c0.want)
		}
	}
}

func biquadResponseV4A(freq, q, at float64) complex128 {
	b0, b1, b2, a1, a2 := v4aBiquadLowPass(freq, sampleRate, q)
	z := cmplx.Rect(1, -2*math.Pi*at/sampleRate)
	num := complex(b0, 0) + complex(b1, 0)*z + complex(b2, 0)*z*z
	den := complex(1, 0) + complex(a1, 0)*z + complex(a2, 0)*z*z
	return num / den
}

func TestDynamicBassRejectsFullBranch(t *testing.T) {

	presets := []string{
		"140;6200;40;60;10;80", "180;5800;55;80;10;70", "300;5600;60;105;10;50",
		"600;5400;60;105;10;20", "100;5600;40;80;50;50", "1200;6200;40;80;0;20",
		"1000;6200;40;80;0;10", "800;6200;40;80;10;0", "400;6200;40;80;10;0",
		"1200;6200;50;90;15;10",
	}
	simple, full := 0, 0
	for _, preset := range presets {
		p := dynamicBassDefaultParams()
		p.Coeffs = preset
		_, err := dynamicBassValidate(p)
		x1, _ := dynamicBassCoeffsOf(p)
		if x1[0] <= dynamicBassSimpleBranchMaxX1 {
			if err != nil {
				t.Errorf("%s: x1=%g <= 120 should compile, yet reject: %v", preset, x1[0], err)
			}
			simple++
			continue
		}
		full++
		if err == nil {
			t.Fatalf("%s: x1=%g > 120 via**full branch**, this machine must reject (must not fake it with an approximation)", preset, x1[0])
		}
		msg := err.Error()
		for _, want := range []string{"full branch", "slot", "not missing hardware"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s rejection reason must mention %q, got: %v", preset, want, err)
			}
		}

		for _, bad := range []string{"missing new hardware", "new opcode", "needs new"} {
			if strings.Contains(msg, bad) {
				t.Errorf("%s rejection reason should not contain old wording %q (after correction: buildable from existing opcode, the bottleneck is slots): %v", preset, bad, err)
			}
		}
	}
	if simple != 1 || full != 9 {
		t.Errorf("official default preset distribution change: simple branch %d / full branch %d (expect 1/9,"+
			"arrays.xml:525-534）", simple, full)
	}

	for _, c := range []struct {
		x1 string
		ok bool
	}{{"120;5600;40;80;50;50", true}, {"121;5600;40;80;50;50", false}} {
		p := dynamicBassDefaultParams()
		p.Coeffs = c.x1
		_, err := dynamicBassValidate(p)
		if (err == nil) != c.ok {
			t.Errorf("coeffs=%q: compiles=%v, expect %v (%v)", c.x1, err == nil, c.ok, err)
		}
	}

	for _, bad := range []string{"", "100", "100;5600;40;80;50", "100;5600;40;80;50;50;1",
		"100;5600;40;80;50;x", "100;5600;-40;80;50;50"} {
		p := dynamicBassDefaultParams()
		p.Coeffs = bad
		if _, err := dynamicBassValidate(p); err == nil {
			t.Errorf("coeffs=%q should reject", bad)
		}
	}

	p := dynamicBassDefaultParams()
	p.Bass = 101
	if _, err := dynamicBassValidate(p); err == nil {
		t.Error("bass=101 exceeds client user end cap 100, should reject")
	}
}

func TestDynamicBassDefaultPresetPlanShape(t *testing.T) {
	p := dynamicBassDefaultParams()
	if p.Coeffs != "100;5600;40;80;50;50" || p.Bass != 0 {
		t.Fatalf("default preset must is V4A official default (100;5600;40;80;50;50 / bass 0), got %+v", p)
	}
	nodes, err := dynamicBassNodes(p)
	if err != nil {
		t.Fatalf("default preset should compile: %v", err)
	}
	if len(nodes) != 1 || nodes[0].Kind != planKindDynBass {
		t.Fatalf("should produce 1 dynbass node, got %+v", nodes)
	}
	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("default preset planning failed: %v", err)
	}
	if plan.Slots != dynamicBassSlots {
		t.Errorf("slot count = %d, expect %d", plan.Slots, dynamicBassSlots)
	}
	if plan.Sections != dynamicBassSections {
		t.Errorf("section count = %d, expect %d", plan.Sections, dynamicBassSections)
	}
	if len(plan.Coefs) != dynamicBassCoefWords {
		t.Errorf("coefficient word count = %d, expect %d", len(plan.Coefs), dynamicBassCoefWords)
	}

	type slotView struct {
		op       int
		n        int
		ina, inb int
		outb     int
		cfb, stb int
	}
	var got []slotView
	wordAt := func(slot, off int) int {
		for _, w := range plan.Words {
			if w.Addr == slot*slotWordSize+off {
				return int(w.Value)
			}
		}
		t.Fatalf("slot %d missing item %d words", slot, off)
		return -1
	}
	for s := 0; s < plan.Slots; s++ {
		cfg := wordAt(s, swCfg)
		got = append(got, slotView{
			op: cfg & 0xFF, n: (cfg >> 16) & 0xFF,
			ina: wordAt(s, swInA), inb: wordAt(s, swInB), outb: wordAt(s, swOutB),
			cfb: wordAt(s, swCfb), stb: wordAt(s, swStb),
		})
	}
	if got[0].op != opNop || got[0].n != 0 {
		t.Errorf("slot 0 must is n=0 transport use slot (via S_DISP bypass branch), got op=%d n=%d", got[0].op, got[0].n)
	}
	if got[0].outb != crossLoBus {
		t.Errorf("slot 0 output must is cross-channel bus %d, got %d", crossLoBus, got[0].outb)
	}
	if got[1].op != opMix2 || got[1].inb != crossLoBus || got[1].outb != 1 {
		t.Errorf("slot 1 should be MIX2(base + cross-channel -> base+1), got %+v", got[1])
	}
	if got[2].op != opBiquad || got[2].ina != 1 || got[2].outb != 2 {
		t.Errorf("slot 2 should be BIQUAD(side signal -> avg), got %+v", got[2])
	}
	if got[3].op != opMix2 || got[3].ina != 0 || got[3].inb != 2 || got[3].outb != 3 {
		t.Errorf("slot 3 should be MIX2(dry + avg -> output), got %+v", got[3])
	}
	if got[0].ina != 0 {
		t.Errorf("chain head enter mouth bus should be 0, got %d", got[0].ina)
	}

	w := dynamicBassPreScaleWeight()
	if w != 128 || float64(w) != float64(qOne)/dynamicBassPreScale {
		t.Errorf("1/K Q3.15 weight = %d, expect 128 (exact)", w)
	}

	if plan.Coefs[0] != w || plan.Coefs[1] != w {
		t.Errorf("side signal MIX2 two weight should be %d/%d, got %d/%d", w, w, plan.Coefs[0], plan.Coefs[1])
	}
	lp, _ := dynamicBassLowPassCoefs(p.Bass)
	for i := 0; i < 5; i++ {
		if plan.Coefs[5+i] != lp[i] {
			t.Errorf("lowpass coefficients %d: plan plan in is %d, expect %d", i, plan.Coefs[5+i], lp[i])
		}
	}
	if plan.Coefs[10] != q315Round(1.0) || plan.Coefs[11] != q315Round(1.0) {
		t.Errorf("add back MIX2 two weight should be 1.0/1.0, got %d/%d", plan.Coefs[10], plan.Coefs[11])
	}

	for s, sv := range got {
		switch sv.op {
		case opNop, opBiquad, opMix2:
		default:
			t.Errorf("slot %d use opcode %d - hard item frozen, only ready-made ones may be used OP_NOP/OP_BIQUAD/OP_MIX2", s, sv.op)
		}
	}
	if plan.StereoFrame {
		t.Error("simple branch**is not**joint-stereo frame pass (that is ColorfulMusic hardcoded path), should not set sf_en")
	}
}

func TestDynamicBassSectionCountIsHonest(t *testing.T) {
	if dynamicBassSections != 3 || dynamicBassSlots != 4 || dynamicBassCoefWords != 15 {
		t.Fatalf("cost consts change: %d slots / %d sections / %d coefficients", dynamicBassSlots, dynamicBassSections, dynamicBassCoefWords)
	}

	nodes, err := dynamicBassNodes(dynamicBassDefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatal(err)
	}

	seen := map[int]bool{}
	for s := 0; s < plan.Slots; s++ {
		for _, w := range plan.Words {
			if w.Addr == s*slotWordSize+swStb {
				if s == 0 {
					continue
				}
				seen[int(w.Value)] = true
			}
		}
	}
	if len(seen) != 3 {
		t.Errorf("three execute slot should each take one different state mark, got %v", seen)
	}
}

func TestDynamicBassRejectsInsteadOfTruncatingWhenSlotsRunOut(t *testing.T) {

	mk := func(slots int) []planNode {
		var nodes []planNode
		for i := 0; i < slots; i++ {
			nodes = append(nodes, planNode{Kind: planKindBiquad, Coefs: [5]int32{32768, 0, 0, 0, 0}})
		}
		return nodes
	}

	fill, err := buildSlotPlanNodes(mk(24))
	if err != nil {
		t.Fatalf("24 section (after packing 8 slot) should pass: %v", err)
	}
	if fill.Slots != 8 {
		t.Logf("24 section pack %d slots (secPerSlot=%d)", fill.Slots, secPerSlot)
	}

	limit := slotLimit()
	if limit%2 != 0 {
		t.Fatalf("slot cap %d is not even number, this test fill pattern need change", limit)
	}
	var nodes []planNode
	for i := 0; i < limit/2; i++ {
		nodes = append(nodes, planNode{
			Kind:  planKindViPERBass,
			Extra: [5]int32{32768, 0, 0, 0, 0},
			Coefs: [5]int32{32768, 0, 0, 0, 0},
			Mix:   [2]int32{32768, 0},
		})
	}
	full, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("%d ViPERBass nodes should exactly fill %d slots fully used: %v", limit/2, limit, err)
	}
	if full.Slots != limit {
		t.Fatalf("fill used %d slots, expect exactly %d", full.Slots, limit)
	}

	dbn, err := dynamicBassNodes(dynamicBassDefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	nodes = append(nodes, dbn...)
	_, err = buildSlotPlanNodes(nodes)
	if err == nil {
		t.Fatalf("slot already fully used (%d slots)+ DynamicBass %d slots must reject", limit, dynamicBassSlots)
	}
	if !strings.Contains(err.Error(), "slot insufficient") {
		t.Errorf("rejection reason must spell out is slot insufficient, got: %v", err)
	}

	if !strings.Contains(err.Error(), "4") {
		t.Logf("(hint) slot insufficient error string: %v", err)
	}
}

func TestDynamicBassCrossChannelLagIsPinned(t *testing.T) {

	want := 360.0 * 55.0 / 48000.0
	if got := dynBassSideLagDegrees(); math.Abs(got-want) > 1e-12 {
		t.Errorf("phase difference = %g deg, expect %g deg", got, want)
	}
	if math.Abs(want-0.4125) > 5e-5 {
		t.Errorf("55 Hz/48 kHz 1 sample should be 0.4125 deg, got %g", want)
	}

	if dynamicBassSideLagSmp != 1 {
		t.Errorf("side channel lag = %d sample, expect 1 (fpga/src/tb/tb_engine_xphase.v measured)", dynamicBassSideLagSmp)
	}

	if maxSections*2 <= maxSections {
		t.Error("per channel section count must >=1, state partitioning precondition")
	}

	if crossLoBus != 22 {
		t.Errorf("crossLoBus = %d, expect 22 (dsp_engine.v:622 BUS_XF_LO)", crossLoBus)
	}

	if s := dynamicBassFullBranchBlocker(); !strings.Contains(s, "slot") ||
		strings.Contains(s, "missing new hardware") || strings.Contains(s, "new opcode") {
		t.Errorf("blocker description must call out\"slot\"and must not contain\"missing new hardware/new opcode\" (2026-09-22 correction), got: %s", s)
	}
}

func TestDynamicBassViewAndPersistence(t *testing.T) {
	saved := currentDynamicBass
	t.Cleanup(func() { currentDynamicBass = saved })

	currentDynamicBass = nil
	if dynamicBassView() != nil {
		t.Error("off when view must is nil")
	}
	p := dynamicBassDefaultParams()
	currentDynamicBass = &p
	v, ok := dynamicBassView().(map[string]any)
	if !ok {
		t.Fatal("dynamicBassView should return map")
	}
	if v["branch"] != "simple" || v["x1"] != 100.0 {
		t.Errorf("view in branch route info not correct: branch=%v x1=%v", v["branch"], v["x1"])
	}
	if v["slots"] != dynamicBassSlots || v["sections"] != dynamicBassSections ||
		v["coef_words"] != dynamicBassCoefWords {
		t.Errorf("view must faithfully report cost: %v", v)
	}
	if v["side_channel_lag_samples"] != 1 {
		t.Error("view must faithfully report that 1 sample side channel lag")
	}
	if s, _ := v["source"].(string); !strings.Contains(s, "DynamicBass.cpp:11-28") {
		t.Errorf("view must with first-hand source, got %v", v["source"])
	}

	st := snapshotRuntimeState()
	if st.DynamicBass == nil || st.DynamicBass.Coeffs != p.Coeffs {
		t.Fatalf("DynamicBass missing from state snapshot: %+v", st.DynamicBass)
	}

	note := dynamicBassBranchNote()
	if note["needs_owner_decision"] != false {
		t.Error("this item already decided and implemented => needs_owner_decision must is false")
	}
	if note["api_field"] != "dynamic_bass" {
		t.Errorf("conclusion in must point new join mouth words section, got %v", note["api_field"])
	}
	if s, _ := note["decision"].(string); !strings.Contains(s, "done") {
		t.Errorf("conclusion must write' done (simple branch)', got %v", note["decision"])
	}
}

func TestRealChainPlusDynamicBassIsRejected(t *testing.T) {
	saveSlots := hwDelaySlots
	hwDelaySlots = 8
	defer func() { hwDelaySlots = saveSlots }()

	dbn, err := dynamicBassNodes(dynamicBassDefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		gear float64
		mode int
		name string
	}{
		{1.0, viperBassModeNatural, "NATURAL full"},
		{0.1, viperBassModeNatural, "NATURAL low"},
	} {
		nodes := realChainNodesEx(t, c.gear, c.mode, false)
		base, err := buildSlotPlanNodes(nodes)
		if err != nil {
			t.Fatalf("%s: real chain itself should fit: %v", c.name, err)
		}

		_, err = buildSlotPlanNodes(append(append([]planNode{}, nodes...), dbn...))
		if err == nil {
			t.Fatalf("%s: real chain %d slots + DynamicBass %d slots = %d slots > %d, must reject",
				c.name, base.Slots, dynamicBassSlots, base.Slots+dynamicBassSlots, slotLimit())
		}
		if !strings.Contains(err.Error(), "slot insufficient") {
			t.Errorf("%s: rejection reason must spell out is slot insufficient, got: %v", c.name, err)
		}
		t.Logf("%s: real chain %d slots + DynamicBass %d slots => reject (%v)",
			c.name, base.Slots, dynamicBassSlots, err)
	}

	small := []planNode{{Kind: planKindViPERBass,
		Extra: [5]int32{32768, 0, 0, 0, 0}, Coefs: [5]int32{32768, 0, 0, 0, 0},
		Mix: [2]int32{32768, 0}}}
	plan, err := buildSlotPlanNodes(append(small, dbn...))
	if err != nil {
		t.Fatalf("small chain (ViPERBass + DynamicBass) should fit: %v", err)
	}
	if plan.Slots != 2+dynamicBassSlots {
		t.Errorf("small chain slot count = %d, expect %d", plan.Slots, 2+dynamicBassSlots)
	}
	t.Logf("small chain (ViPERBass 2 slot + DynamicBass %d slots)= %d slots, compiles OK", dynamicBassSlots, plan.Slots)

	baseCost := chainFrameCost(37, true, true, false, false, 22)
	withCost := chainFrameCost(37+dynamicBassSections, true, true, false, false,
		22+dynamicBassSlots)
	t.Logf("frame budget delta: %d -> %d cycles (+%d cycles = per section %d cycles x%d sections + 2 cycles/slot x%d slots);"+
		"per channel cap %d cycles => if slots suffice, take than %.1f%% -> %.1f%%",
		baseCost, withCost, withCost-baseCost, costPerSectionRuntime(),
		dynamicBassSections, dynamicBassSlots, frameBudgetCycles,
		100*float64(baseCost)/float64(frameBudgetCycles),
		100*float64(withCost)/float64(frameBudgetCycles))
}

// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"strings"
	"testing"
)

func TestColorfulCoeffsMatchReferenceModel(t *testing.T) {
	p := colorfulParams{Depth: 1000, Widening: 0.5, MidImage: 0.8}
	jsd, js3, err := colorfulJointCoefs(p, sampleRate)
	if err != nil {
		t.Fatalf("colorfulJointCoefs：%v", err)
	}
	wantJSD := [9]int32{
		18427,
		-18427,
		960, 672,
		1023, -1804, 799, 61336, -28747,
	}
	wantJS3 := [2]int32{30147, -9175}
	if jsd != wantJSD {
		t.Errorf("JDST coefficients and reference model not symbol: \n got %v\n expect %v", jsd, wantJSD)
	}
	if js3 != wantJS3 {
		t.Errorf("J3DS coefficients and reference model not symbol: got %v, expect %v", js3, wantJS3)
	}

	if got := colorfulJointDelayWords(sampleRate); got != 960 {
		t.Errorf("delay budget should take longer of the two 960, got %d", got)
	}
}

func TestColorfulDepthBranchSign(t *testing.T) {
	for _, c := range []struct {
		depth   int
		wantNeg bool
	}{{1000, true}, {500, true}, {499, false}, {300, false}, {1, false}} {
		g, g1 := colorfulDepthGain(c.depth)
		if g <= 0 || g > 1.0 {
			t.Errorf("depth=%d: g=%.6f must fall in (0,1]", c.depth, g)
		}
		if c.wantNeg && g1 != -g {
			t.Errorf("depth=%d: prev1 gain should be -g (got %.6f, g=%.6f)", c.depth, g1, g)
		}
		if !c.wantNeg && g1 != g {
			t.Errorf("depth=%d: prev1 gain should be +g (got %.6f, g=%.6f)", c.depth, g1, g)
		}
	}

	if g, _ := colorfulDepthGain(0); g != 0 {
		t.Errorf("depth=0 should be off (gain 0), got %.6f", g)
	}
}

func TestColorfulMatrixMatchesStereo3D(t *testing.T) {

	ca, cb := colorfulMatrix(1.2, 1.5)

	if got, want := ca, 0.46875+0.6875; absf(got-want) > 1e-12 {
		t.Errorf("ca=%.9f, expect %.9f", got, want)
	}
	if got, want := cb, 0.46875-0.6875; absf(got-want) > 1e-12 {
		t.Errorf("cb=%.9f, expect %.9f", got, want)
	}

	ca2, cb2 := colorfulMatrix(-1.5, 1.0)
	if got, want := ca2, 1.0*0.5+(-0.5)*0.5; absf(got-want) > 1e-12 {
		t.Errorf("x<2 branch ca=%.9f, expect %.9f", got, want)
	}
	if got, want := cb2, 1.0*0.5-(-0.5)*0.5; absf(got-want) > 1e-12 {
		t.Errorf("x<2 branch cb=%.9f, expect %.9f", got, want)
	}
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func TestColorfulPlanShape(t *testing.T) {
	savedSections, savedCoefs, savedSlots := hwMaxSections, hwCoefWords, hwDelaySlots
	defer func() {
		hwMaxSections, hwCoefWords, hwDelaySlots = savedSections, savedCoefs, savedSlots
	}()
	hwMaxSections, hwCoefWords, hwDelaySlots = 48, 240, 8

	nodes := []planNode{
		{Kind: planKindBiquad, Coefs: [5]int32{32767, 0, 0, 0, 0}},
		{Kind: planKindBiquad, Coefs: [5]int32{32767, 0, 0, 0, 0}},
	}
	cn, err := colorfulNodes(colorfulDefaultParams())
	if err != nil {
		t.Fatalf("colorfulNodes：%v", err)
	}
	nodes = append(nodes, cn...)

	nodes = append(nodes, planNode{Kind: planKindBiquad, Coefs: [5]int32{32767, 0, 0, 0, 0}})

	p, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("buildSlotPlanNodes (4 node -> 3 slot): %v", err)
	}
	if !p.StereoFrame {
		t.Fatal("with joint section table must set StereoFrame (header bit8 = sf_en)")
	}

	if p.Slots != 4 {
		t.Fatalf("slot count should be 4 (previous stage 1 + JDST + J3DS + later stage 1), got %d", p.Slots)
	}
	get := func(slot, word int) uint32 {
		for _, w := range p.Words {
			if w.Addr == slot*slotWordSize+word {
				return w.Value
			}
		}
		t.Fatalf("slot %d %d words not in the plan", slot, word)
		return 0
	}
	if op := get(1, swCfg) & 0xFF; int(op) != opJDST {
		t.Errorf("slot 1 should be JDST(%d), got %d", opJDST, op)
	}
	if op := get(2, swCfg) & 0xFF; int(op) != opJ3DS {
		t.Errorf("slot 2 should be J3DS(%d), got %d", opJ3DS, op)
	}

	if got := get(1, swInA); got != 2 {
		t.Errorf("JDST in_a (L output bus) should be 2, got %d", got)
	}
	if got := get(1, swInB); got != 1 {
		t.Errorf("JDST in_b (R input bus = previous-stage output) should be 1, got %d", got)
	}
	if got := get(1, swOutB); got != 3 {
		t.Errorf("JDST out_b (R output bus) should be 3, got %d", got)
	}

	if got := get(2, swInA); got != 2 {
		t.Errorf("J3DS in_a should be 2, got %d", got)
	}
	if got := get(2, swInB); got != 3 {
		t.Errorf("J3DS in_b should be 3, got %d", got)
	}
	if got := get(2, swOutB); got != 0xFFFF {
		t.Errorf("J3DS out_b should be 0xFFFF (output only enters js_new_* register), got %#x", got)
	}

	if got := get(3, swInA); got != 4 {
		t.Errorf("later-stage slot input should be jbus=4, got %d", got)
	}
	if p.JBus != 4 {
		t.Errorf("JBus should be 4, got %d", p.JBus)
	}

	if got := get(1, swCfb); got != 10 {
		t.Errorf("JDST coefficient base should be 10 (previous stage 2 section = 10 words), got %d", got)
	}
	if got := get(2, swCfb); got != 20 {
		t.Errorf("J3DS coefficient base should be 20, got %d", got)
	}

	if len(p.Coefs) != 30 {
		t.Errorf("coefficient table should be 30 words (previous stage 10 + JDST 10 + J3DS 5 + later stage 5), got %d", len(p.Coefs))
	}
	jsd, js3, _ := colorfulJointCoefs(colorfulDefaultParams(), sampleRate)
	for i := 0; i < 9; i++ {
		if p.Coefs[10+i] != jsd[i] {
			t.Errorf("JDST %d coefficients (global 10+%d)= %d, expect %d", i, i, p.Coefs[10+i], jsd[i])
		}
	}
	if p.Coefs[19] != 0 {
		t.Errorf("JDST coefficient block fill bit (global 19) should be 0, got %d", p.Coefs[19])
	}
	if p.Coefs[20] != js3[0] || p.Coefs[21] != js3[1] {
		t.Errorf("J3DS two coefficients = %d,%d, expect %d,%d",
			p.Coefs[20], p.Coefs[21], js3[0], js3[1])
	}

	if got := get(1, swStb); got != 0 {
		t.Errorf("JDST STB (in-ring offset) should be 0, got %d", got)
	}

	if want := 3 + jointStateReserve; p.Sections != want {
		t.Errorf("section count should be %d, got %d", want, p.Sections)
	}
	t.Logf("joint section plan plan: %d slots / %d sections / %d coefficients, jbus=%d (StereoFrame=%v)",
		p.Slots, p.Sections, len(p.Coefs), p.JBus, p.StereoFrame)
}

func TestColorfulDelayOffsetDoesNotOverlap(t *testing.T) {
	savedSections, savedCoefs, savedSlots, savedWords := hwMaxSections, hwCoefWords, hwDelaySlots, hwDelayWords
	defer func() {
		hwMaxSections, hwCoefWords, hwDelaySlots, hwDelayWords = savedSections, savedCoefs, savedSlots, savedWords
	}()
	hwMaxSections, hwCoefWords, hwDelaySlots, hwDelayWords = 48, 240, 8, 8192

	cn, err := colorfulNodes(colorfulDefaultParams())
	if err != nil {
		t.Fatalf("colorfulNodes：%v", err)
	}
	nodes := append([]planNode{}, cn...)
	nodes = append(nodes, planNode{Kind: planKindDelay, Len: 100})
	p, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("buildSlotPlanNodes：%v", err)
	}
	stbOf := func(slot int) int {
		for _, w := range p.Words {
			if w.Addr == slot*slotWordSize+swStb {
				return int(w.Value)
			}
		}
		return -1
	}
	jointOff, otherOff := stbOf(0), stbOf(2)
	if jointOff != 0 {
		t.Errorf("first-allocated joint section offset should be 0, got %d", jointOff)
	}
	if otherOff != colorfulJointDelayWords(sampleRate) {
		t.Errorf("later delay slot offset should be %d (avoid joint section %d words), got %d",
			colorfulJointDelayWords(sampleRate), colorfulJointDelayWords(sampleRate), otherOff)
	}

	if otherOff < jointOff+colorfulJointDelayWords(sampleRate) {
		t.Errorf("delay interval overlap: joint [%d,%d) and another one slot [%d,%d)",
			jointOff, jointOff+colorfulJointDelayWords(sampleRate), otherOff, otherOff+100)
	}
}

func TestColorfulRejectedWithoutHardware(t *testing.T) {
	savedGen, savedCur := dspEngineGen, currentColorful
	defer func() { dspEngineGen, currentColorful = savedGen, savedCur }()
	dspEngineGen = 0
	currentColorful = nil
	p := colorfulDefaultParams()
	err := setColorfulMusic(&p)
	if err == nil {
		t.Fatal("missing CAP1 bit12 when must reject deploy ColorfulMusic (silent downgrade will let user for open)")
	}
	if !strings.Contains(err.Error(), "joint-stereo frame") {
		t.Errorf("rejection reason must state the missing one is' joint-stereo frame pass', got: %v", err)
	}
	if currentColorful != nil {
		t.Error("after rejection the state must not be left dirty (must not leave in memory 'on')")
	}

	var c dspEngineCaps
	if c.JointStereoAvailable() {
		t.Error("missing CAP0 magic when should not report available")
	}
	c = dspEngineCaps{Present: true, Opcodes: capOpcodeJointStereo}
	if !c.JointStereoAvailable() {
		t.Error("set CAP1 bit12 should report available")
	}
	c.Opcodes = 0x0FFB
	if c.JointStereoAvailable() {
		t.Error("legacy bitmap (0FFB) should not report has joint-stereo frame pass")
	}
}

func TestColorfulStateReserveIsEnforced(t *testing.T) {
	savedSections, savedCoefs, savedSlots := hwMaxSections, hwCoefWords, hwDelaySlots
	defer func() {
		hwMaxSections, hwCoefWords, hwDelaySlots = savedSections, savedCoefs, savedSlots
	}()

	hwMaxSections, hwCoefWords, hwDelaySlots = 48, 800, 8

	cn, err := colorfulNodes(colorfulDefaultParams())
	if err != nil {
		t.Fatalf("colorfulNodes：%v", err)
	}

	ok := make([]planNode, 0, 48)
	for i := 0; i < 48-jointStateReserve; i++ {
		ok = append(ok, planNode{Kind: planKindBiquad, Coefs: [5]int32{32767, 0, 0, 0, 0}})
	}
	ok = append(ok, cn...)
	if _, err := buildSlotPlanNodes(ok); err != nil {
		t.Fatalf("%d sections + joint section should fit (per channel leave %d sections), got: %v",
			48-jointStateReserve, jointStateReserve, err)
	}

	bad := append(append([]planNode{}, ok...), planNode{Kind: planKindBiquad, Coefs: [5]int32{32767, 0, 0, 0, 0}})
	if _, err := buildSlotPlanNodes(bad); err == nil {
		t.Fatalf("49 section (incl. joint-section reserve) must reject - exceeds NSEC=48 would make states clobber each other")
	} else if !strings.Contains(err.Error(), "section") {
		t.Errorf("rejection reason should spell out section count ask issue, got: %v", err)
	}
}

func TestColorfulFrameBudgetCountsJointCycles(t *testing.T) {
	base := chainFrameCost(20, false, false, false, false, 8)
	withJoint := chainFrameCost(20+jointStateReserve, false, false, false, true, 8)
	perSec := costPerSectionRuntime()
	if got, want := withJoint-base, colorfulJointCycles+jointStateReserve*perSec; got != want {
		t.Errorf("with joint section budget increase amount = %d, expect %d (joint %d cycles + %d sections x%d cycles)",
			got, want, colorfulJointCycles, jointStateReserve, perSec)
	}

	cn, _ := colorfulNodes(colorfulDefaultParams())
	p1, err := buildSlotPlanNodes([]planNode{{Kind: planKindBiquad, Coefs: [5]int32{32767, 0, 0, 0, 0}}})
	if err != nil {
		t.Fatal(err)
	}
	p2, err := buildSlotPlanNodes([]planNode{
		{Kind: planKindBiquad, Coefs: [5]int32{32767, 0, 0, 0, 0}}, cn[0]})
	if err != nil {
		t.Fatal(err)
	}
	if p2.Sections != p1.Sections+jointStateReserve {
		t.Errorf("with joint section after section count should +%d (got %d -> %d)",
			jointStateReserve, p1.Sections, p2.Sections)
	}
	if p2.Slots != p1.Slots+2 {
		t.Errorf("joint section take two slot (got %d -> %d)", p1.Slots, p2.Slots)
	}
}

func TestColorfulValidate(t *testing.T) {

	if _, err := colorfulValidate(colorfulDefaultParams()); err != nil {
		t.Errorf("client user end default params should valid: %v", err)
	}

	if _, err := colorfulValidate(colorfulParams{Depth: 1001, Widening: 1.2, MidImage: 1.5}); err == nil {
		t.Error("depth=1001 should reject (core core range 0..1000)")
	}

	if _, err := colorfulValidate(colorfulParams{Depth: 200, Widening: 5, MidImage: 1.5}); err == nil {
		t.Error("widening=5 should reject")
	}

	got, err := colorfulValidate(colorfulParams{Depth: 200, Widening: -3, MidImage: -1})
	if err != nil {
		t.Fatalf("negative value should clamped is not error: %v", err)
	}
	if got.Widening != 0 || got.MidImage != 0 {
		t.Errorf("negative value should clamp to 0, got widening=%v mid_image=%v", got.Widening, got.MidImage)
	}

	client := colorfulParams{Depth: 200, Widening: 120.0 / 100.0, MidImage: 150.0 / 100.0}
	if _, err := colorfulValidate(client); err != nil {
		t.Errorf("client user end default preset (120;200 / 150) converted should valid: %v", err)
	}
}

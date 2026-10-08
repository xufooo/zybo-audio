// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"strings"
	"testing"
)

func mkSections(n int) [][5]int32 {
	out := make([][5]int32, n)
	for i := range out {
		for k := 0; k < 5; k++ {
			out[i][k] = int32((i+1)*100 + k)
		}
	}
	return out
}

func wordAt(t *testing.T, p slotPlan, addr int) uint32 {
	t.Helper()
	for _, w := range p.Words {
		if w.Addr == addr {
			return w.Value
		}
	}
	t.Fatalf("slot word address %d missing", addr)
	return 0
}

func TestBuildSlotPlanSixBands(t *testing.T) {
	p, err := buildSlotPlan(mkSections(6))
	if err != nil {
		t.Fatalf("should not fail: %v", err)
	}
	if p.Slots != 2 || p.Sections != 6 {
		t.Fatalf("slots/sections = %d/%d, want 2/6", p.Slots, p.Sections)
	}
	if len(p.Words) != 16 {
		t.Fatalf("descriptor word count = %d, want 16", len(p.Words))
	}
	if len(p.Coefs) != 30 {
		t.Fatalf("coefficient count = %d, want 30", len(p.Coefs))
	}
	for s := 0; s < 2; s++ {
		base := s * slotWordSize
		cfg := wordAt(t, p, base+swCfg)
		if cfg&0xFF != opBiquad {
			t.Errorf("slot %d opcode = %d, want BIQUAD(%d)", s, cfg&0xFF, opBiquad)
		}
		if n := int(cfg >> 16 & 0xFF); n != 3 {
			t.Errorf("slot %d section count = %d, want 3", s, n)
		}
		if fl := cfg >> 8 & 0xFF; fl != 0 {
			t.Errorf("slot %d flags = %#x, want 0 (must not carry bypass)", s, fl)
		}
		if got, want := wordAt(t, p, base+swCfb), uint32(s*15); got != want {
			t.Errorf("slot %d coef_base = %d, want %d", s, got, want)
		}
		if got, want := wordAt(t, p, base+swStb), uint32(s*3); got != want {
			t.Errorf("slot %d state_base = %d, want %d", s, got, want)
		}
		if got, want := wordAt(t, p, base+swInA), uint32(s); got != want {
			t.Errorf("slot %d in_a = %d, want %d", s, got, want)
		}
		if got, want := wordAt(t, p, base+swOutB), uint32(s+1); got != want {
			t.Errorf("slot %d out_bus = %d, want %d", s, got, want)
		}
	}

	for i := 0; i < 6; i++ {
		for k := 0; k < 5; k++ {
			if got, want := p.Coefs[i*5+k], int32((i+1)*100+k); got != want {
				t.Fatalf("section %d coefficient #%d = %d, want %d", i, k, got, want)
			}
		}
	}
}

func TestBuildSlotPlanEmpty(t *testing.T) {
	p, err := buildSlotPlan(nil)
	if err != nil {
		t.Fatalf("should not fail: %v", err)
	}
	if p.Slots != 0 || len(p.Words) != 0 || len(p.Coefs) != 0 || p.Sections != 0 {
		t.Fatalf("empty chain should write nothing, got %+v", p)
	}
}

func TestBuildSlotPlanPacking(t *testing.T) {
	p, err := buildSlotPlan(mkSections(7))
	if err != nil {
		t.Fatalf("should not fail: %v", err)
	}
	if p.Slots != 3 || p.Sections != 7 {
		t.Fatalf("slots/sections = %d/%d, want 3/7", p.Slots, p.Sections)
	}
	base := 2 * slotWordSize
	if n := int(wordAt(t, p, base+swCfg) >> 16 & 0xFF); n != 1 {
		t.Errorf("last slot section count = %d, want 1", n)
	}
	if got, want := wordAt(t, p, base+swCfb), uint32(30); got != want {
		t.Errorf("last slot coef_base = %d, want %d", got, want)
	}
	if got, want := wordAt(t, p, base+swStb), uint32(6); got != want {
		t.Errorf("last slot state_base = %d, want %d", got, want)
	}
}

func TestBuildSlotPlanLimit(t *testing.T) {
	p, err := buildSlotPlan(mkSections(maxSections))
	if err != nil {
		t.Fatalf("%d sections should fit: %v", maxSections, err)
	}
	if p.Sections != maxSections || len(p.Coefs) != maxSections*5 {
		t.Fatalf("sections/coefficients = %d/%d, want %d/%d",
			p.Sections, len(p.Coefs), maxSections, maxSections*5)
	}
	if p.Slots > dspSlotMax {
		t.Fatalf("slot count %d exceeds engine limit %d", p.Slots, dspSlotMax)
	}

	_, err = buildSlotPlan(mkSections(maxSections + 1))
	if err == nil {
		t.Fatal("over the limit must fail (rule: never truncate silently)")
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error should mention the limit, got: %v", err)
	}
}

func TestTruepeakAvailableFollowsHardwareBitmap(t *testing.T) {
	cases := []struct {
		name string
		caps dspEngineCaps
		want bool
		why  string
	}{
		{"board-measured bitmap 0x0303", dspEngineCaps{Present: true, Opcodes: 0x0303}, true, "bit9 set => has limiter"},
		{"same generation without limiter", dspEngineCaps{Present: true, Opcodes: 0x0103}, false, "bit9 clear => must report false"},
		{"only NOP", dspEngineCaps{Present: true, Opcodes: 0x0001}, false, "no opcodes at all"},
		{"old bitstream (no magic)", dspEngineCaps{Present: false, Opcodes: 0x0303}, false, "wrong magic means this bitmap is not recognized"},
	}
	for _, c := range cases {
		if got := c.caps.TruepeakAvailable(); got != c.want {
			t.Errorf("%s: TruepeakAvailable() = %v, want %v (%s)", c.name, got, c.want, c.why)
		}
	}
}

func TestSevenBandsFitsOnSlotEngine(t *testing.T) {
	saved := dspEngineGen
	dspEngineGen = 1
	defer func() { dspEngineGen = saved }()

	for _, n := range []int{7, 10, maxSections} {
		slots, active, err := chainToSlots(mkBands(n))
		if err != nil {
			t.Fatalf("%d sections must fit on the 0.2 engine: %v", n, err)
		}
		if active != n {
			t.Fatalf("%d sections: active sections = %d", n, active)
		}

		savedSlots, savedPre := currentSlots, currentPreampDB
		currentSlots, currentPreampDB = slots, 0
		coefs, _ := dspFinalCoeffs()
		plan, err := buildSlotPlan(coefs[:dspActiveBands()])
		currentSlots, currentPreampDB = savedSlots, savedPre
		if err != nil {
			t.Fatalf("%d sections failed to compile slot table: %v", n, err)
		}
		if plan.Sections != n {
			t.Errorf("%d sections: plan only has %d sections (disabled sections must not be counted, no active section may be missing)",
				n, plan.Sections)
		}
		if plan.Slots > dspSlotMax {
			t.Fatalf("%d sections need %d slots > NSLOT=%d", n, plan.Slots, dspSlotMax)
		}
		if len(plan.Coefs) != n*coefPerBand {
			t.Errorf("%d sections: coefficient count = %d, want %d", n, len(plan.Coefs), n*coefPerBand)
		}

		if len(plan.Coefs) > coefWordsPerBank {
			t.Errorf("%d sections: %d coefficients exceed coefficient RAM (%d words/channel)", n, len(plan.Coefs), coefWordsPerBank)
		}
	}

	if _, _, err := chainToSlots(mkBands(maxSections + 1)); err == nil {
		t.Fatalf("%d sections over the engine limit, must fail", maxSections+1)
	}
}

func TestBandsReportsWhatTheChainActuallyAccepts(t *testing.T) {
	cases := []struct {
		gen   int
		limit int
		why   string
	}{
		{0, legacyBands, "0.1 bitstream: hardware is a 6-section cascade"},
		{1, maxSections, "0.2 slot-table engine: fills up to the RAM limit"},
	}
	for _, c := range cases {
		saved := dspEngineGen
		dspEngineGen = c.gen
		limit := bandLimit()
		if limit != c.limit {
			dspEngineGen = saved
			t.Fatalf("gen=%d: bandLimit() = %d, want %d (%s)", c.gen, limit, c.limit, c.why)
		}

		slots, n, err := chainToSlots(mkBands(limit))
		if err != nil {
			dspEngineGen = saved
			t.Fatalf("gen=%d: %d sections should fit: %v", c.gen, limit, err)
		}
		if n != limit {
			dspEngineGen = saved
			t.Fatalf("gen=%d: active sections = %d, want %d", c.gen, n, limit)
		}

		if _, _, err := chainToSlots(mkBands(limit + 1)); err == nil {
			dspEngineGen = saved
			t.Fatalf("gen=%d: chain can hold %d sections? then bands reporting %d is lying -- but it actually fails",
				c.gen, limit+1, limit)
		}
		_ = slots
		dspEngineGen = saved
	}

	if !(maxSections > legacyBands) {
		t.Fatalf("premise broken: engine limit %d is not greater than 0.1's %d sections", maxSections, legacyBands)
	}
}

func mkBands(n int) []ChainItem {
	out := make([]ChainItem, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, ChainItem{
			Type:    "peq",
			Enabled: true,
			Freq:    100 * float64(i+1),
			Q:       1.0,
			GainDB:  1.0,
		})
	}
	return out
}

func TestBuildSlotPlanDynamicBass(t *testing.T) {
	dynC := [5]int32{2 * qOne, qOne, qOne / 4, qOne * 3 / 4, (500 << 9) | 300}
	p, err := buildSlotPlanNodes([]planNode{{Kind: planKindDyn, Coefs: dynC, Side: mkSections(1)[0]}})
	if err != nil {
		t.Fatalf("DYN chain should not fail: %v", err)
	}
	if p.Slots != 2 {
		t.Fatalf("slot count = %d, want 2 (detection bandpass + DYN)", p.Slots)
	}
	if p.Sections != 2 {
		t.Fatalf("section count = %d, want 2 (DYN occupies two sections of state RAM)", p.Sections)
	}
	if len(p.Coefs) != 10 {
		t.Fatalf("coefficient count = %d, want 10 (5 words each)", len(p.Coefs))
	}

	if op := wordAt(t, p, swCfg) & 0xFF; op != opBiquad {
		t.Errorf("slot 0 opcode = %d, want BIQUAD", op)
	}
	if got := wordAt(t, p, swOutB); got != dynScratchBus {
		t.Errorf("slot 0 out_b = %d, want %d (sidechain private bus)", got, dynScratchBus)
	}

	base := slotWordSize
	if op := wordAt(t, p, base+swCfg) & 0xFF; op != opDyn {
		t.Errorf("slot 1 opcode = %d, want DYN(%d)", op, opDyn)
	}
	if got := wordAt(t, p, base+swInA); got != 0 {
		t.Errorf("DYN in_a = %d, want 0 (signal is on the chain)", got)
	}
	if got := wordAt(t, p, base+swInB); got != dynScratchBus {
		t.Errorf("DYN in_b = %d, want %d (detection sidechain)", got, dynScratchBus)
	}
	if got := wordAt(t, p, base+swOutB); got != 1 {
		t.Errorf("DYN out_b = %d, want 1 (back to chain)", got)
	}
	if got := wordAt(t, p, base+swCfb); got != 5 {
		t.Errorf("DYN param base = %d, want 5 (detection bandpass occupies 0..4)", got)
	}

	nodes := []planNode{
		{Kind: planKindBiquad, Coefs: mkSections(1)[0]},
		{Kind: planKindBiquad, Coefs: mkSections(1)[0]},
		{Kind: planKindDyn, Coefs: dynC, Side: mkSections(1)[0]},
	}
	p2, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("mixed chain should not fail: %v", err)
	}
	if p2.Slots != 3 {
		t.Fatalf("mixed chain slot count = %d, want 3 (1 packed biquad slot + 2 DYN slots)", p2.Slots)
	}
	if n := int(wordAt(t, p2, swCfg) >> 16 & 0xFF); n != 2 {
		t.Errorf("slot 0 section count = %d, want 2 (two biquad sections packed)", n)
	}
}

func TestBuildChainNodesDynFirst(t *testing.T) {
	dyn := dynDefaultParams()
	secs := [][5]int32{mkSections(1)[0], mkSections(1)[0]}
	nodes, err := buildChainNodes(secs, &dyn, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 || nodes[0].Kind != planKindDyn {
		t.Fatalf("node sequence should be [DYN, biquad, biquad], got %v", nodes)
	}
	p, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if p.Slots != 3 {
		t.Fatalf("slot count = %d, want 3 (detection bandpass + DYN + packed biquad pair)", p.Slots)
	}

	base := slotWordSize
	if op := wordAt(t, p, base+swCfg) & 0xFF; op != opDyn {
		t.Errorf("slot 1 opcode = %d, want DYN", op)
	}
	if got := wordAt(t, p, base+swInA); got != 0 {
		t.Errorf("DYN in_a = %d, want 0", got)
	}
	if got := wordAt(t, p, base+swOutB); got != 1 {
		t.Errorf("DYN out_b = %d, want 1 (after chain head)", got)
	}

	base2 := 2 * slotWordSize
	if n := int(wordAt(t, p, base2+swCfg) >> 16 & 0xFF); n != 2 {
		t.Errorf("slot 2 section count = %d, want 2", n)
	}
	if got := wordAt(t, p, base2+swInA); got != 1 {
		t.Errorf("EQ in_a = %d, want 1 (right after DYN)", got)
	}

	if n2, _ := buildChainNodes(secs, nil, nil, nil); len(n2) != 2 {
		t.Errorf("node count after dynamic bass is off = %d, want 2", len(n2))
	}
}

func TestDynParamsToCoefs(t *testing.T) {
	p := dynDefaultParams()
	c, err := dynParamsToCoefs(p)
	if err != nil {
		t.Fatal(err)
	}

	if c[0] < 64000 || c[0] > 66500 {
		t.Errorf("gmax(%+.1f dB) = %d, want about 65383 (2.0x32768)", p.GainDB, c[0])
	}
	if c[1] != qOne {
		t.Errorf("gmin = %d, want %d (cut_db=0)", c[1], qOne)
	}

	if c[2] < 1600 || c[2] > 2100 {
		t.Errorf("ref(%+.0f dB) = %d, want about 0.056x32768", p.RefDB, c[2])
	}
	att_s, rel_s := c[4]&0x1F, (c[4]>>5)&0x1F

	if att_s < 7 || att_s > 9 {
		t.Errorf("5 ms attack should encode as s~=8, got %d", att_s)
	}
	if rel_s < 12 || rel_s > 14 {
		t.Errorf("200 ms release should encode as s~=13, got %d", rel_s)
	}
	if rel_s <= att_s {
		t.Errorf("release (s=%d) time constant must be longer than attack (s=%d)", rel_s, att_s)
	}

	c2, err := dynParamsToCoefs(dynParams{GainDB: 99, CutDB: -5, RefDB: 10, KS: 1, AttMs: 0, RelMs: 0})
	if err != nil {
		t.Fatalf("out-of-range params should be clamped: %v", err)
	}
	if c2[0] > coefMax {
		t.Errorf("out-of-range gain should be clamped to what the coefficient format holds, got %d > %d", c2[0], coefMax)
	}
	if c2[1] != qOne {
		t.Errorf("cut_db=-5 should be clamped to 0 (gmin=1.0=%d), got %d", qOne, c2[1])
	}
}

func TestSelfCheckComparesWrittenPlan(t *testing.T) {
	savedGen, savedPlan := dspEngineGen, lastPlanCoefs
	defer func() { dspEngineGen, lastPlanCoefs = savedGen, savedPlan }()

	dspEngineGen = 1

	lastPlanCoefs = []int32{32849, 0, -32849, -65346, 32499, 32768, 0, 8192, 24576, 24448}

	if got := coefWritten(); got != len(lastPlanCoefs) {
		t.Fatalf("coefWritten() = %d, want %d (must be computed from what was actually written)", got, len(lastPlanCoefs))
	}
	want := dspExpectedCoeffs()
	if len(want) != len(lastPlanCoefs) {
		t.Fatalf("expected-value length %d != written %d", len(want), len(lastPlanCoefs))
	}
	for i := range want {
		if want[i] != lastPlanCoefs[i] {
			t.Fatalf("expected value #%d = %d vs %d written into hardware (self-check would fail spuriously)",
				i, want[i], lastPlanCoefs[i])
		}
	}
}

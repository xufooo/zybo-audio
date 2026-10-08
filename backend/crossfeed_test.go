// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"math/cmplx"
	"testing"
)

func biquadResponse(c [5]int32, freq, fs float64) complex128 {
	b0 := float64(c[0]) / 32768
	b1 := float64(c[1]) / 32768
	b2 := float64(c[2]) / 32768
	a1 := float64(c[3]) / 32768
	a2 := float64(c[4]) / 32768
	z := cmplx.Rect(1, -2*math.Pi*freq/fs)
	num := complex(b0, 0) + complex(b1, 0)*z + complex(b2, 0)*z*z

	den := complex(1, 0) + complex(a1, 0)*z + complex(a2, 0)*z*z
	return num / den
}

func TestCrossfeedBs2bProperties(t *testing.T) {
	p := crossfeedDefaultParams()
	if p.FcutHz != 700 || p.Feed != 60 {

		t.Fatalf("Default params should be the Moderate tier 700/60 (ViPER.cpp:381-389 / default.conf:16-17), got %+v", p)
	}
	lo, hi, mix, err := crossfeedCoefs(p)
	if err != nil {
		t.Fatalf("Coefficient computation failed: %v", err)
	}
	g := float64(mix[0]) / 32768
	if mix[0] != mix[1] {
		t.Errorf("Both MIX2 weights should match (both equal the bs2b gain), got %d/%d", mix[0], mix[1])
	}

	for _, f := range []float64{50, 100, 300} {
		h := complex(g, 0) * (biquadResponse(hi, f, sampleRate) + biquadResponse(lo, f, sampleRate))
		if db := 20 * math.Log10(cmplx.Abs(h)); math.Abs(db) > 0.5 {
			t.Errorf("Mono low-end %.0f Hz level %.2f dB (should be ~= 0) -- gain normalization or a1 sign is wrong", f, db)
		}
	}
	prev := 0.0
	for _, f := range []float64{300, 1000, 3000, 10000} {
		h := complex(g, 0) * (biquadResponse(hi, f, sampleRate) + biquadResponse(lo, f, sampleRate))
		db := 20 * math.Log10(cmplx.Abs(h))
		if db > 0.2 || db < -3.0 {
			t.Errorf("Mono %.0f Hz tilts to %.2f dB, outside bs2b's inherent range (0 to -3 dB)", f, db)
		}
		if db > prev+0.05 {
			t.Errorf("Mono response should slope down monotonically at high end: %.0f Hz higher than previous point (%.2f -> %.2f)", f, prev, db)
		}
		prev = db
	}

	c100 := cmplx.Abs(biquadResponse(lo, 100, sampleRate))
	c10k := cmplx.Abs(biquadResponse(lo, 10000, sampleRate))
	if c100 <= c10k*4 {
		t.Errorf("Cross branch is not lowpass: 100 Hz %.4f vs 10 kHz %.4f", c100, c10k)
	}

	if c100 < 0.30 || c100 > 0.50 {
		t.Errorf("Crossover amount at 100 Hz %.3f, want ~= 0.398 (G_lo)", c100)
	}

	for _, c := range append(append([]int32{}, lo[:]...), hi[:]...) {
		if c > 131071 || c < -131072 {
			t.Errorf("Coefficient %d exceeds Q3.15 18-bit range", c)
		}
	}
}

func TestCrossfeedCoefsPin(t *testing.T) {
	lo, hi, mix, err := crossfeedCoefs(crossfeedDefaultParams())
	if err != nil {
		t.Fatal(err)
	}

	if lo[0] < 1130 || lo[0] > 1155 {
		t.Errorf("lo.b0 = %d, want ~=1142 (a0_lo = G_lo*(1-b1_lo))", lo[0])
	}
	if lo[3] < -30000 || lo[3] > -29800 {
		t.Errorf("lo.a1 = %d, want ~=-29899 (= -b1_lo)", lo[3])
	}
	if lo[1] != 0 || lo[2] != 0 || lo[4] != 0 {
		t.Errorf("lo should be first-order (b1=b2=a2=0), got %v", lo)
	}

	if hi[0] < 31900 || hi[0] > 32050 {
		t.Errorf("hi.b0 = %d, want ~=31964 (a0_hi, Fc_hi~=975 Hz)", hi[0])
	}
	if hi[1] != hi[3] {
		t.Errorf("hi b1 and a1 must match (same b1_hi with opposite signs): %d vs %d", hi[1], hi[3])
	}

	if mix[0] < 27400 || mix[0] > 27600 {
		t.Errorf("MIX2 weight = %d, want ~=27481 (bs2b gain)", mix[0])
	}
}

func TestCrossfeedPlanLayout(t *testing.T) {
	lo, hi, mix, err := crossfeedCoefs(crossfeedDefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	nodes := []planNode{
		{Kind: planKindBiquad, Coefs: [5]int32{32768, 0, 0, 0, 0}},
		{Kind: planKindCross, Coefs: lo, Hi: hi, Mix: mix},
	}
	p, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if p.Slots != 4 {
		t.Fatalf("Slot count = %d, want 4 (1 EQ section + cross lo/hi/MIX2)", p.Slots)
	}
	word := func(slot, off int) int { return int(p.Words[slot*slotWordSize+off].Value) }
	opOf := func(slot int) int { return word(slot, swCfg) & 0xFF }
	if opOf(0) != opBiquad {
		t.Errorf("Slot 0 should be biquad, got op=%d", opOf(0))
	}
	if opOf(1) != opBiquad || word(1, swOutB) != crossLoBus {
		t.Errorf("Slot 1 should be the lo section writing cross-channel bus %d, got op=%d out=%d",
			crossLoBus, opOf(1), word(1, swOutB))
	}
	if opOf(2) != opBiquad || word(2, swOutB) != 2 {
		t.Errorf("Slot 2 should be the hi section writing bus 2, got op=%d out=%d", opOf(2), word(2, swOutB))
	}
	if opOf(3) != opMix2 {
		t.Fatalf("Slot 3 should be MIX2, got op=%d", opOf(3))
	}
	if word(3, swInA) != 2 || word(3, swInB) != crossLoBus || word(3, swOutB) != 3 {
		t.Errorf("MIX2 in_a/in_b/out should be 2/%d/3, got %d/%d/%d",
			crossLoBus, word(3, swInA), word(3, swInB), word(3, swOutB))
	}

	if len(p.Coefs) != 20 {
		t.Fatalf("Coefficient count = %d, want 20", len(p.Coefs))
	}

	if p.Coefs[15] != mix[0] || p.Coefs[16] != mix[1] {
		t.Errorf("MIX2 weights missed coefficient table positions 15/16: %d %d", p.Coefs[15], p.Coefs[16])
	}

	if p.Sections != 3 {
		t.Errorf("Section count = %d, want 3", p.Sections)
	}
}

func TestCrossfeedChainOrder(t *testing.T) {
	secs := [][5]int32{{32768, 0, 0, 0, 0}, {32768, 0, 0, 0, 0}}
	d := dynDefaultParams()
	c := crossfeedDefaultParams()
	nodes, err := buildChainNodes(secs, &d, &c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 4 {
		t.Fatalf("Node count = %d, want 4 (DYN + 2 EQ sections + cross)", len(nodes))
	}
	if nodes[0].Kind != planKindDyn {
		t.Errorf("Node 0 should be DYN (dynamic bass before EQ), got %s", nodes[0].Kind)
	}
	if nodes[1].Kind != planKindBiquad || nodes[2].Kind != planKindBiquad {
		t.Errorf("Middle two should be EQ sections, got %s/%s", nodes[1].Kind, nodes[2].Kind)
	}
	if nodes[3].Kind != planKindCross {
		t.Errorf("Last node should be cross (after EQ), got %s", nodes[3].Kind)
	}

	if n, _ := buildChainNodes(secs, nil, nil, nil); len(n) != 2 {
		t.Errorf("With both off want only 2 sections, got %d", len(n))
	}
}

func TestDelayPlanLayout(t *testing.T) {
	nodes := []planNode{
		{Kind: planKindDelay, Len: 8},
		{Kind: planKindBiquad, Coefs: [5]int32{32768, 0, 0, 0, 0}},
	}
	p, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	if p.Slots != 2 {
		t.Fatalf("Slot count = %d, want 2", p.Slots)
	}
	word := func(slot, off int) int { return int(p.Words[slot*slotWordSize+off].Value) }
	if op := word(0, swCfg) & 0xFF; op != opDelay {
		t.Errorf("Slot 0 should be DELAY (op=%d), got op=%d", opDelay, op)
	}
	if p.Coefs[0] != 8 {
		t.Errorf("DELAY c0 should be length 8, got %d", p.Coefs[0])
	}

	if word(1, swStb) != 0 {
		t.Errorf("Trailing section state base should be 0 (ring takes no state words), got %d", word(1, swStb))
	}
	if p.Sections != 1 {
		t.Errorf("Section count = %d, want 1 (only that EQ section; ring lives in its own array)", p.Sections)
	}

	if _, err := buildSlotPlanNodes([]planNode{{Kind: planKindDelay, Len: 6}}); err != nil {
		t.Errorf("Delay length 6 should be legal (pointer wraps modulo L, no power-of-two needed): %v", err)
	}

	pf, err := buildSlotPlanNodes([]planNode{{Kind: planKindDelay, Len: 96, Flags: 0x04}})
	if err != nil {
		t.Fatal(err)
	}

	if got, want := int(pf.Words[swCfg].Value), (1<<16)|(0x04<<8)|opDelay; got != want {
		t.Errorf("cfg = %#x, want %#x (n=1, flags=0x04, op=%d)", got, want, opDelay)
	}

	if _, err := buildSlotPlanNodes([]planNode{{Kind: planKindDelay, Len: hwDelayWords + 1}}); err == nil {
		t.Errorf("Delay %d exceeds the %d-word-per-channel ring and should fail", hwDelayWords+1, hwDelayWords)
	}

	mix := []planNode{{Kind: planKindDelay, Len: 256}}
	for i := 0; i < maxSections-1; i++ {
		mix = append(mix, planNode{Kind: planKindBiquad, Coefs: [5]int32{32768, 0, 0, 0, 0}})
	}
	if _, err := buildSlotPlanNodes(mix); err != nil {
		t.Errorf("%d EQ sections + 256-word delay ring should fit: %v", maxSections-1, err)
	}

	mix = append(mix, planNode{Kind: planKindBiquad, Coefs: [5]int32{32768, 0, 0, 0, 0}})
	if _, err := buildSlotPlanNodes(mix); err == nil {
		t.Errorf("%d EQ sections + delay slot need %d words > coefficient RAM %d words and should fail",
			maxSections, (maxSections+1)*coefPerBand, coefWordsPerBank)
	}
}

func TestSurroundPlanAndUnits(t *testing.T) {

	cases := []struct {
		ms   float64
		want int
	}{
		{0, 0},
		{1, 48},
		{2.5, 120},
		{5, 240},
		{100, 4800},

		{500, hwDelayWords},
	}
	for _, c := range cases {
		if got := surroundDelaySamples(surroundParams{DelayMs: c.ms}); got != c.want {
			t.Errorf("%.1f ms -> %d samples, want %d", c.ms, got, c.want)
		}
	}

	sp := surroundParams{DelayMs: 2.5}
	nodes, err := buildChainNodes(nil, nil, nil, &sp)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Kind != planKindDelay {
		t.Fatalf("Want exactly one delay node, got %+v", nodes)
	}
	if nodes[0].Len != 120 || nodes[0].Flags != flDlyRight {
		t.Errorf("Delay node = %+v, want Len=120 Flags=%#x", nodes[0], flDlyRight)
	}
	p, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if op := int(p.Words[swCfg].Value) & 0xFF; op != opDelay {
		t.Errorf("op = %d, want %d (DELAY)", op, opDelay)
	}
	if fl := (int(p.Words[swCfg].Value) >> 8) & 0xFF; fl != flDlyRight {
		t.Errorf("flags = %#x, want %#x (delay second channel only)", fl, flDlyRight)
	}
	if p.Coefs[0] != 120 {
		t.Errorf("c0 = %d, want 120", p.Coefs[0])
	}

	d := dynDefaultParams()
	c := crossfeedDefaultParams()
	secs := [][5]int32{{32768, 0, 0, 0, 0}}
	all, err := buildChainNodes(secs, &d, &c, &sp)
	if err != nil {
		t.Fatal(err)
	}
	last := all[len(all)-1]
	if last.Kind != planKindDelay {
		t.Errorf("Last node should be surround (delay), got %s", last.Kind)
	}
	if all[len(all)-2].Kind != planKindCross {
		t.Errorf("Second-to-last should be crossfeed, got %s", all[len(all)-2].Kind)
	}
}

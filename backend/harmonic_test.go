// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
)

func sat24(v int64) int32 {
	if v > 8388607 {
		return 8388607
	}
	if v < -8388608 {
		return -8388608
	}
	return int32(v)
}

func dftHarmonics(c [11]float64, N int) [6]float64 {
	var h [6]float64
	for n := 0; n < N; n++ {
		th := 2 * math.Pi * float64(n) / float64(N)
		x := math.Cos(th)
		p := 0.0
		for i := 10; i >= 0; i-- {
			p = p*x + c[i]
		}
		for k := 1; k <= 5; k++ {
			h[k] += p * math.Cos(float64(k)*th)
		}
	}
	for k := 1; k <= 5; k++ {
		h[k] *= 2.0 / float64(N)
	}
	return h
}

func TestHarmonicChebyshevToMonomial(t *testing.T) {
	cases := []struct {
		name string
		amps [10]float64
		want [6]float64
	}{
		{

			name: "VSE emits odd orders only 0.02",
			amps: [10]float64{0.02, 0, 0.02, 0, 0.02, 0, 0.02, 0, 0.02, 0},
			want: [6]float64{0, 0.02, 0, 0.02, 0, 0.02},
		},
		{

			name: "AnalogX first four",
			amps: [10]float64{0.01, 0.02, 0.0001, 0.001, 0, 0, 0, 0, 0, 0},
			want: [6]float64{0, 0.01, 0.02, 0.0001, 0.001, 0},
		},
		{
			name: "fundamental only = identity",
			amps: [10]float64{1, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			want: [6]float64{0, 1, 0, 0, 0, 0},
		},
	}
	for _, tc := range cases {
		c := harmonicMonomialCoeffs(tc.amps)
		h := dftHarmonics(c, 3600)
		for k := 1; k <= 5; k++ {
			if math.Abs(h[k]-tc.want[k]) > 1e-9 {
				t.Errorf("%s: %d -order harmonic amplitude = %.9f, expect %.9f (expansion algorithm not correct)",
					tc.name, k, h[k], tc.want[k])
			}
		}
	}
}

func TestHarmonicNormalisationBounded(t *testing.T) {
	amps := [10]float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	c := harmonicMonomialCoeffs(amps)
	worst := 0.0
	for i := 0; i <= 2000; i++ {
		x := -1.0 + 2.0*float64(i)/2000.0
		p := 0.0
		for k := 10; k >= 0; k-- {
			p = p*x + c[k]
		}
		if a := math.Abs(p); a > worst {
			worst = a
		}
	}
	if worst > 1.0+1e-9 {
		t.Errorf("|p| most large value %.6f > 1: normalization not applied (hard item per |p|<=1 scaling)", worst)
	}
	t.Logf("Σ|a|=10 when |p|max = %.6f (normalization should <=1)", worst)
}

func TestHarmonicQ315FitsQ315Range(t *testing.T) {
	p := exciterDefaultParams()
	c := harmonicMonomialCoeffs(p.Harmonics)
	if mx := polyMaxCoef(c); mx <= 3.9 {
		t.Fatalf("this test group max coefficient %.3f not exceeding Q3.15 cap, measure not to scaling path", mx)
	}
	nodes, err := exciterNodes(p)
	if err != nil {
		t.Fatalf("exciterNodes error: %v", err)
	}
	for i := 0; i < 11; i++ {
		if v := nodes[0].Poly[i]; v > 131071 || v < -131072 {
			t.Errorf("%d single-wing coefficients %d exceeds Q3.15 (18 bit has symbol number) range", i, v)
		}
	}

	scale := 3.9 / polyMaxCoef(c)
	wantWet := p.Mix / scale
	gainStage := 1.0
	if nodes[0].MixB[0] != 0 {
		gainStage = float64(nodes[0].MixB[0]) / 32768.0
	}
	got := float64(nodes[0].Mix[1]) / 32768.0 * gainStage
	if math.Abs(got-wantWet) > 0.03 {
		t.Errorf("wet weight xgain stage = %.4f, and Mix/scale=%.4f mismatch (scaling not per side folded into MIX2)",
			got, wantWet)
	}
	if nodes[0].Mix[0] != 32767 {
		t.Errorf("dry gain should be 1.0 (32767), actual %d", nodes[0].Mix[0])
	}
	if got := nodes[0].Poly[11]; got != 200 {
		t.Errorf("anti-pop length = %d, expect 200 (|a|max*10000, and Harmonic.cpp same convention)", got)
	}
}

func TestHarmonicFixedPointMatchesFloat(t *testing.T) {
	p := exciterDefaultParams()
	c := harmonicMonomialCoeffs(p.Harmonics)
	scale := 3.9 / polyMaxCoef(c)
	var q [11]int32
	var cf [11]float64
	for i := 0; i < 11; i++ {
		q[i] = q315Round(c[i] * scale)
		cf[i] = float64(q[i]) / 32768.0
	}
	const N = 512
	var fpp, fpy int32
	var rpp, rpy float64
	worst := 0.0
	for n := 0; n < N; n++ {

		xr := 0.4*math.Cos(2*math.Pi*17*float64(n)/N) +
			0.2*math.Cos(2*math.Pi*61*float64(n)/N)
		if n == 0 {
			xr += 0.3
		}
		x24 := sat24(int64(math.Round(xr * (1 << 23))))

		x18 := int64(x24 >> 6)
		acc := int64(q[10]) << 23
		for k := 9; k >= 0; k-- {
			acc = (acc>>17)*x18 + int64(q[k])<<23
		}
		pi := sat24(acc >> 15)
		yi := sat24(((acc - (int64(fpp) << 15)) + int64(fpy)*32735) >> 15)
		fpp, fpy = pi, yi

		pf := 0.0
		for k := 10; k >= 0; k-- {
			pf = pf*xr + cf[k]
		}
		yf := (pf + (32735.0/32768.0)*rpy) - rpp
		rpp, rpy = pf, yf
		d := math.Abs(float64(yi) - yf*float64(int64(1)<<23))
		if d > worst {
			worst = d
		}
	}

	if worst > 64 {
		t.Errorf("fixed-point vs floating-point most large deviation %.1f LSB (24 bit)> 64 LSB, fixed-point path broken", worst)
	}
	t.Logf("fixed-point vs floating-point most large deviation = %.1f LSB (24 bit, ~= %.1f dBFS)",
		worst, 20*math.Log10(worst/float64(int64(1)<<23)))
}

func TestExciterCompilesToFourSlots(t *testing.T) {
	p := exciterDefaultParams()
	nodes, err := exciterNodes(p)
	if err != nil {
		t.Fatalf("exciterNodes：%v", err)
	}
	if len(nodes) != 1 || nodes[0].Kind != planKindExciter {
		t.Fatalf("exciter should compile 1 exciter node, actual %d", len(nodes))
	}
	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("buildSlotPlanNodes：%v", err)
	}
	if plan.Slots != 4 {
		t.Errorf("should compile 4 slot (HPF/POLY/LPF/MIX2), actual %d", plan.Slots)
	}
	if plan.Sections != 3 {
		t.Errorf("should take 3 state section (HPF/POLY/LPF), actual %d", plan.Sections)
	}
	word := func(slot, off int) uint32 {
		for _, w := range plan.Words {
			if w.Addr == slot*slotWordSize+off {
				return w.Value
			}
		}
		return 0
	}
	ops := []uint32{word(0, swCfg) & 0xFF, word(1, swCfg) & 0xFF,
		word(2, swCfg) & 0xFF, word(3, swCfg) & 0xFF}
	want := []uint32{opBiquad, opPoly, opBiquad, opMix2}
	for i := range want {
		if ops[i] != want[i] {
			t.Errorf("%d slots opcode = %d, expect %d", i, ops[i], want[i])
		}
	}

	if got := (word(1, swCfg) >> 16) & 0xFF; got != 11 {
		t.Errorf("POLY slot n = %d, expect 11 (n=0 would be treated as bypass by the engine)", got)
	}

	if got := word(0, swCfb); got != 0 {
		t.Errorf("HPF coefficient base = %d, expect 0", got)
	}
	if got := word(1, swCfb); got != 5 {
		t.Errorf("POLY coefficient base = %d, expect 5", got)
	}
	if got := word(2, swCfb); got != 17 {
		t.Errorf("LPF coefficient base = %d, expect 17", got)
	}

	if st := word(1, swStb); st != 1 {
		t.Errorf("POLY state section = %d, expect 1", st)
	}

	if inB := word(3, swInB); inB == crossLoBus || inB >= crossLoBus {
		t.Errorf("MIX2 in_b = %d collide with protect leave bus number", inB)
	}

	if len(plan.Coefs) < 27 {
		t.Errorf("total coefficients %d < 27 (HPF5+POLY12+LPF5+MIX5)", len(plan.Coefs))
	}
	if plan.Coefs[5+11] != int32(exciterMuteSamples(p)) {
		t.Errorf("POLY most one coefficients (anti-pop length)= %d, expect %d",
			plan.Coefs[5+11], exciterMuteSamples(p))
	}
}

func TestChainOverflowIsReported(t *testing.T) {
	p := exciterDefaultParams()
	ex, err := exciterNodes(p)
	if err != nil {
		t.Fatalf("exciterNodes：%v", err)
	}

	xhifiWithDelaySlots(t, 8, func() {
		one := mustXHIFINode(t, 50)
		fir := func(n int) []planNode {
			out := make([]planNode, n)
			for i := range out {
				out[i] = planNode{Kind: planKindFIR, Blocks: firTaps / firMACS}
			}
			return out
		}
		full := append([]planNode{one, one}, fir(8)...)
		plan, err := buildSlotPlanNodes(full)
		if err != nil {
			t.Fatalf("2 XHIFI + 8 convolution (24 slot = NSLOT) should fit, yet reject: %v", err)
		}
		if plan.Slots != dspSlotMax {
			t.Fatalf("slot count = %d, expect %d (= dspSlotMax)", plan.Slots, dspSlotMax)
		}
		over := append([]planNode{one, one}, fir(9)...)
		if _, err := buildSlotPlanNodes(over); err == nil {
			t.Fatalf("%d slots > NSLOT=%d compiled when it should not have - must error", 16+9, dspSlotMax)
		} else {
			t.Logf("slot count errors as expected: %v", err)
		}
	})

	ok := append([]planNode{{Kind: planKindFIR, Blocks: firTaps / firMACS}}, ex...)
	if _, err := buildSlotPlanNodes(ok); err != nil {
		t.Fatalf("convolution + exciter (5 slot) should not error: %v", err)
	}

	busHeavy := []planNode{ex[0], ex[0], ex[0], ex[0], ex[0], ex[0]}
	if _, err := buildSlotPlanNodes(busHeavy); err == nil {
		t.Fatalf("bus peak %d compiled when it should not have - must error (%d/%d is protect leave number)",
			4*len(busHeavy)+4, crossLoBus, dynScratchBus)
	} else {
		t.Logf("errors as expected: %v", err)
	}
}

func TestFullChainFitsAfterCapacityExpansion(t *testing.T) {
	ddc := make([][5]int32, 16)
	for i := range ddc {
		ddc[i] = [5]int32{32767, 0, 0, 0, 0}
	}
	p := exciterDefaultParams()
	ex, err := exciterNodes(p)
	if err != nil {
		t.Fatalf("exciterNodes：%v", err)
	}
	nodes := append([]planNode{}, ex...)
	for _, s := range ddc {
		nodes = append(nodes, planNode{Kind: planKindBiquad, Coefs: s})
	}
	nodes = append(nodes, planNode{Kind: planKindFIR, Blocks: firTaps / firMACS})
	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("chain should fit after expansion, yet reject: %v", err)
	}
	if plan.Slots != 11 {
		t.Errorf("slot count = %d, expect 11 (exciter 4 + DDC 6 + convolution 1)", plan.Slots)
	}
	if plan.Sections != 19 {
		t.Errorf("section count = %d, expect 19 (DDC 16 + exciter HPF/LPF 2 + POLY state 1)", plan.Sections)
	}
	if plan.Slots > dspSlotMax || plan.Sections > maxSections {
		t.Errorf("plan plan %d slots / %d sections exceeds dspSlotMax=%d / maxSections=%d",
			plan.Slots, plan.Sections, dspSlotMax, maxSections)
	}
}

func TestExciterMixRangeIsEnforced(t *testing.T) {
	orig := currentExciter
	defer func() { currentExciter = orig }()

	p := exciterDefaultParams()
	if p.Mix != 0.56 {
		t.Fatalf("default mix should be 0.56 (V4A UI 0.1 x5.6 x100/100), actual %.3f", p.Mix)
	}
	if _, err := harmonicQ315(p); err != nil {
		t.Fatalf("default params should not error: %v", err)
	}

	scale := exciterWetScale(p)
	if scale >= 1.0 {
		t.Fatalf("VSE factory shape single-wing coefficients most large 10.24 > 3.9 => must overall scale small, got scale=%.4f", scale)
	}
	for _, mix := range []float64{maxMixQ315, 4.48, 5.04, 5.6} {
		q := p
		q.Mix = mix
		if err := validateExciterMix(q); err != nil {
			t.Errorf("VSE shape mix=%.3f should can deploy (product %.3f <= %.3f): %v",
				mix, mix*scale, maxMixQ315, err)
		}
	}

	q := p
	q.Harmonics = [10]float64{}
	if exciterWetScale(q) != 1.0 {
		t.Fatalf("all-zero harmonics should not trigger scaling")
	}

	q.Mix = 15.9
	if err := validateExciterMix(q); err != nil {
		t.Errorf("mix=15.9 in' MIX2 4.0 x gain stage 4.0' within, should not reject: %v", err)
	}
	q.Mix = 16.1
	if err := validateExciterMix(q); err == nil {
		t.Error("not scaling when mix=16.1 exceeds' MIX2 4.0 x gain stage 4.0' expressible range, must error is not saturate")
	} else {
		t.Logf("rejected as expected: %v", err)
	}

	q.Mix = -0.1
	if err := validateExciterMix(q); err == nil {
		t.Error("negative mix must reject")
	}
}

func TestChainOrderMatchesJamesDSP(t *testing.T) {

	firSave := currentFIR
	ddcMu.Lock()
	ddcSave := ddcSections
	ddcSections = [][5]int32{{32767, 0, 0, 0, 0}, {32767, 0, 0, 0, 0}}
	ddcMu.Unlock()
	defer func() {
		currentFIR = firSave
		ddcMu.Lock()
		ddcSections = ddcSave
		ddcMu.Unlock()
	}()
	currentFIR = &firParams{Taps: 8192, Name: "order-test"}

	sections := [][5]int32{{32767, 0, 0, 0, 0}}
	dyn := &dynParams{GainDB: 6, CutDB: 0, RefDB: -25, KS: 0.75, AttMs: 5, RelMs: 200}
	cross := &crossfeedParams{FcutHz: 700, Feed: 60}
	sur := &surroundParams{DelayMs: 5}

	nodes, err := buildChainNodes(sections, dyn, cross, sur)
	if err != nil {
		t.Fatalf("buildChainNodes：%v", err)
	}

	var seq []string
	for _, n := range nodes {
		if len(seq) == 0 || seq[len(seq)-1] != n.Kind {
			seq = append(seq, n.Kind)
		}
	}
	want := []string{planKindDyn, planKindBiquad, planKindFIR, planKindBiquad, planKindCross, planKindDelay}
	if len(seq) != len(want) {
		t.Fatalf("chain order = %v, expect %v (dyn-> EQ-> convolution-> DDC-> cross-> surround)", seq, want)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("chain order %d stage = %q, expect %q (complete whole: %v)", i, seq[i], want[i], seq)
		}
	}

	firIdx, ddcIdx := -1, -1
	for i, n := range nodes {
		if n.Kind == planKindFIR && firIdx < 0 {
			firIdx = i
		}
	}

	if firIdx < 0 {
		t.Fatal("chain in missing convolution stage")
	}
	ddcIdx = firIdx + 1
	if nodes[ddcIdx].Kind != planKindBiquad {
		t.Errorf("convolution after should be DDC (biquad section), actual %q", nodes[ddcIdx].Kind)
	}
}

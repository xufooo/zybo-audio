// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"math/cmplx"
	"testing"
)

type bqSim struct {
	c      [5]int32
	x1, x2 float64
	y1, y2 float64
}

func (s *bqSim) step(x float64) float64 {
	b0 := float64(s.c[0]) / 32768
	b1 := float64(s.c[1]) / 32768
	b2 := float64(s.c[2]) / 32768
	a1 := float64(s.c[3]) / 32768
	a2 := float64(s.c[4]) / 32768
	y := b0*x + b1*s.x1 + b2*s.x2 - a1*s.y1 - a2*s.y2
	s.x2, s.x1 = s.x1, x
	s.y2, s.y1 = s.y1, y
	return y
}

type mbc struct{ b0, b1, b2, a1, a2 float64 }
type mbSim struct {
	c      mbc
	x1, x2 float64
	y1, y2 float64
}

func rbjRaw(kind string, freq, q, fs float64) mbc {
	omega := 2 * math.Pi * freq / fs
	sinO, cosO := math.Sin(omega), math.Cos(omega)
	alpha := sinO / (2 * q)
	a0 := 1 + alpha
	a1r := -2 * cosO
	a2r := 1 - alpha
	var b0, b1, b2 float64
	switch kind {
	case "LP":
		b0, b1, b2 = (1-cosO)/2, 1-cosO, (1-cosO)/2
	case "HP":
		b0, b1, b2 = (1+cosO)/2, -(1 + cosO), (1+cosO)/2
	case "BP":
		b0, b1, b2 = alpha, 0, -alpha
	}
	return mbc{b0 / a0, b1 / a0, b2 / a0, -(a1r / a0), -(a2r / a0)}
}

func (s *mbSim) step(x float64) float64 {
	y := s.c.b0*x + s.c.b1*s.x1 + s.c.b2*s.x2 + s.c.a1*s.y1 + s.c.a2*s.y2
	s.x2, s.x1 = s.x1, x
	s.y2, s.y1 = s.y1, y
	return y
}

func testSignal(n int) []float64 {
	sig := make([]float64, n)
	sig[0] = 1
	seed := uint32(12345)
	for i := 1; i < n; i++ {
		seed = seed*1664525 + 1013904223
		sig[i] = float64(int32(seed>>8)) / float64(1<<23) * 0.5
	}
	return sig
}

func TestSpeakerCorrectionMergeMatchesDirectForm(t *testing.T) {
	const fs = 48000.0
	mine := speakerCorrectionBiquads(fs)
	var s0, s1, s2 bqSim
	s0.c, s1.c, s2.c = mine[0].i32(), mine[1].i32(), mine[2].i32()

	var dlp, dhp, dbp bqSim
	dlp.c = rbj("LP", 13500, 1.0, fs).i32()
	dhp.c = rbj("HP", 80, 1.0, fs).i32()
	dbp.c = rbj("BP", 420, 3.88, fs).i32()

	sig := testSignal(2048)
	var maxErr, maxAbs float64
	for _, x := range sig {
		got := s2.step(s1.step(s0.step(x)))
		z := dhp.step(dlp.step(x))
		want := z/2 + dbp.step(z/2)
		if e := math.Abs(got - want); e > maxErr {
			maxErr = e
		}
		if a := math.Abs(want); a > maxAbs {
			maxAbs = a
		}
	}
	if maxErr > 2e-3 {
		t.Errorf("merged form and V4A direct form mismatch:max error %g(signal peak %g)-- "+
			"the derivation folding 0.5*(1+BP) into the numerator is wrong", maxErr, maxAbs)
	}
	t.Logf("same setup sample-by-sample max error %g(peak %g)", maxErr, maxAbs)

	for _, f := range []float64{20, 100, 420, 1000, 5000, 13500, 18000} {
		got := cmplx.Abs(biquadResponse(mine[0].i32(), f, fs)) *
			cmplx.Abs(biquadResponse(mine[1].i32(), f, fs)) *
			cmplx.Abs(biquadResponse(mine[2].i32(), f, fs))
		hbp := biquadResponse(dbp.c, f, fs)
		want := cmplx.Abs(biquadResponse(dlp.c, f, fs)) *
			cmplx.Abs(biquadResponse(dhp.c, f, fs)) * cmplx.Abs(0.5+0.5*hbp)
		gotDB, wantDB := 20*math.Log10(got), 20*math.Log10(want)
		if d := math.Abs(gotDB - wantDB); d > 0.15 {
			t.Errorf("%g Hz:merged %+.3f dB vs direct %+.3f dB(diff %.3f dB)", f, gotDB, wantDB, d)
		} else {
			t.Logf("%6.0f Hz:merged %+.3f dB / direct %+.3f dB", f, gotDB, wantDB)
		}
	}
}

func TestClarityNaturalMergeMatchesTwoStages(t *testing.T) {
	const fs = 48000.0
	for _, level := range []float64{10, 50, 100} {
		g := level / 100.0
		var merged bqSim
		merged.c = noiseSharpeningBiquad(fs, g)

		l0, _, la := iir1LPF_BW(fs/2-1000, fs)
		var prevX float64

		var x1, y1 float64

		sig := testSignal(4096)
		var maxErr float64
		for _, x := range sig {
			got := merged.step(x)

			pre := x + g*(x-prevX)
			prevX = x

			want := l0*pre + l0*x1 - la*y1
			x1, y1 = pre, want
			if e := math.Abs(got - want); e > maxErr {
				maxErr = e
			}
		}
		if maxErr > 2e-4 {
			t.Errorf("level=%g:merged single section and two cascaded stages mismatch(max error %g)", level, maxErr)
		}
		t.Logf("level=%g(g=%.2f)sample-by-sample max error %g", level, g, maxErr)
	}
}

func TestClarityOzoneIsHighShelfAt8250(t *testing.T) {
	const fs = 48000.0
	for _, level := range []float64{0, 50, 100} {
		nodes, err := clarityNodes(clarityParams{Mode: clarityModeOzone, Level: level}, fs)
		if err != nil {
			t.Fatalf("level=%g：%v", level, err)
		}
		if len(nodes) != 1 {
			t.Fatalf("OZONE should be 1 sections,got %d", len(nodes))
		}
		resp := func(f float64) float64 {
			return 20 * math.Log10(cmplx.Abs(biquadResponse(nodes[0].Coefs, f, fs)))
		}

		wantHigh := 20 * math.Log10(level/100+1)
		if math.Abs(resp(100)) > 1.0 {
			t.Errorf("level=%g:100 Hz at %+.2f dB,shelving must not touch lows", level, resp(100))
		}
		if math.Abs(resp(16000)-wantHigh) > 1.5 {
			t.Errorf("level=%g:16 kHz at %+.2f dB,expected ≈ %+.2f dB(20*log10(g+1))"+
				" -- frequency or gain convention and ViPERClarity.cpp mismatch", level, resp(16000), wantHigh)
		}
		t.Logf("level=%g:100 Hz %+.2f dB,16 kHz %+.2f dB(expected %+.2f)",
			level, resp(100), resp(16000), wantHigh)
	}
}

func TestA4SectionCost(t *testing.T) {
	cn, err := clarityNodes(clarityDefaultParams(), 48000)
	if err != nil {
		t.Fatal(err)
	}
	if len(cn) != 1 {
		t.Errorf("Clarity should 1 sections,got %d", len(cn))
	}
	sn := speakerCorrectionNodes(48000)
	if len(sn) != 3 {
		t.Errorf("SpeakerCorrection should 3 sections,got %d", len(sn))
	}

	for _, n := range append(append([]planNode{}, cn...), sn...) {
		if n.Kind != planKindBiquad {
			t.Errorf("A4 sections should all planKindBiquad,got %q", n.Kind)
		}
	}
}

func TestClarityXHIFIIsImplemented(t *testing.T) {
	clr := clarityParams{Mode: clarityModeXHIFI, Level: 50}
	nodes, err := clarityNodes(clr, 48000)
	if err != nil {
		t.Fatalf("XHIFI already implemented,clarityNodes should not fail:%v", err)
	}
	if len(nodes) != 1 || nodes[0].Kind != planKindXHIFI {
		t.Fatalf("XHIFI must be planned as planKindXHIFI, got %+v (silently degrading to another gear = user thinks XHIFI is on)", nodes)
	}
	if err := setClarity(&clr); err != nil {
		t.Fatalf("setClarity(XHIFI) should not fail:%v", err)
	}

	for _, m := range []int{clarityModeNatural, clarityModeOzone, clarityModeXHIFI} {
		if err := setClarity(&clarityParams{Mode: m, Level: 50}); err != nil {
			t.Errorf("setClarity(mode=%d)：%v", m, err)
		}
	}

	if err := setClarity(&clarityParams{Mode: clarityModeOzone, Level: 101}); err == nil {
		t.Error("level over 100 must fail")
	}
	currentClarity = nil
}

func TestA4ChainOrderAndSectionBudget(t *testing.T) {

	firSave, tubeSave := currentFIR, currentTube
	clrSave, spkSave := currentClarity, currentSpeakerCorrection
	ddcMu.Lock()
	ddcSave := ddcSections
	ddcSections = [][5]int32{{32767, 0, 0, 0, 0}, {32767, 0, 0, 0, 0}}
	ddcMu.Unlock()
	defer func() {
		currentFIR, currentTube = firSave, tubeSave
		currentClarity, currentSpeakerCorrection = clrSave, spkSave
		ddcMu.Lock()
		ddcSections = ddcSave
		ddcMu.Unlock()
	}()

	currentFIR = &firParams{Taps: 8192, Name: "a4-order-test"}
	clr := clarityParams{Mode: clarityModeOzone, Level: 50}
	currentClarity = &clr
	currentSpeakerCorrection = &speakerCorrectionParams{}
	currentTube = &tubeParams{}

	sections := [][5]int32{{32767, 0, 0, 0, 0}}
	nodes, err := buildChainNodes(sections, nil, &crossfeedParams{FcutHz: 700, Feed: 60},
		&surroundParams{DelayMs: 5})
	if err != nil {
		t.Fatalf("buildChainNodes：%v", err)
	}

	clrNodes, err := clarityNodes(clr, sampleRate)
	if err != nil {
		t.Fatalf("clarityNodes：%v", err)
	}
	clrCoef := clrNodes[0].Coefs
	spkCoefs := make(map[[5]int32]int)
	for i, n := range speakerCorrectionNodes(sampleRate) {
		spkCoefs[n.Coefs] = i
	}
	clrIdx, crossIdx, delayIdx, tubeIdx := -1, -1, -1, -1
	spkIdx := []int{}
	tubeCoef := tubeNodes()[0].Coefs

	for i, n := range nodes {
		switch n.Kind {
		case planKindCross:
			if crossIdx < 0 {
				crossIdx = i
			}
		case planKindDelay:
			if delayIdx < 0 {
				delayIdx = i
			}
		case planKindBiquad:
			switch {
			case n.Coefs == clrCoef && clrIdx < 0:
				clrIdx = i
			case n.Coefs == tubeCoef:
				tubeIdx = i
			default:
				if _, ok := spkCoefs[n.Coefs]; ok {
					spkIdx = append(spkIdx, i)
				}
			}
		}
	}
	if clrIdx < 0 {
		t.Fatal("in chain not found Clarity that section(coefficient mismatch)")
	}
	if len(spkIdx) != 3 {
		t.Fatalf("in chain should have 3 sections SpeakerCorrection,got %d sections(position %v)", len(spkIdx), spkIdx)
	}
	if crossIdx < 0 || delayIdx < 0 || tubeIdx < 0 {
		t.Fatalf("cross/surround/tube not found: cross=%d delay=%d tube=%d", crossIdx, delayIdx, tubeIdx)
	}
	if !(clrIdx < crossIdx) {
		t.Errorf("Clarity must come before crossfeed (V4A: Clarity(12) -> Cure(13)), got %d vs %d",
			clrIdx, crossIdx)
	}
	if !(spkIdx[0] > delayIdx) {
		t.Errorf("SpeakerCorrection must come after surround (V4A: DiffSurround(5) -> reverb(6) -> SpeakerCorrection(7)), "+
			"got %d vs surround %d", spkIdx[0], delayIdx)
	}
	if !(spkIdx[2] < tubeIdx) {
		t.Errorf("SpeakerCorrection must come before tube (V4A: SpeakerCorrection(7) -> ... -> tube(14)), "+
			"got %d vs tube %d", spkIdx[2], tubeIdx)
	}

	nSec := 0
	for _, n := range nodes {
		if n.Kind == planKindBiquad {
			nSec++
		}
	}
	if nSec > maxSections {
		t.Errorf("this chain biquad section count %d exceeds engine cap %d", nSec, maxSections)
	}
	t.Logf("chain order:clarity@%d < cross@%d,surround@%d < speaker@%v < tube@%d;biquad sections of %d/%d",
		clrIdx, crossIdx, delayIdx, spkIdx, tubeIdx, nSec, maxSections)
}

func TestAdoptHardwareCaps(t *testing.T) {

	saveSec, saveWords, saveBase, saveDly := hwMaxSections, hwCoefWords, hwCoefFIRBase, hwDelayWords
	saveSlots, saveSFIRTap, saveSFIROvh, saveSFIRBase := hwDelaySlots, hwSFIRMaxTaps, hwSFIROvh, hwCoefSFIRBase
	defer func() {
		hwMaxSections, hwCoefWords, hwCoefFIRBase, hwDelayWords = saveSec, saveWords, saveBase, saveDly
		hwDelaySlots, hwSFIRMaxTaps, hwSFIROvh, hwCoefSFIRBase = saveSlots, saveSFIRTap, saveSFIROvh, saveSFIRBase
	}()

	hwMaxSections, hwCoefWords, hwCoefFIRBase, hwDelayWords = maxSections, coefWordsPerBank, coefFIRBase, maxDelayWords
	adoptCapsFrom(dspEngineCaps{})
	if hwMaxSections != maxSections || hwCoefWords != coefWordsPerBank ||
		hwCoefFIRBase != coefFIRBase || hwDelayWords != maxDelayWords {
		t.Errorf("old bitstream(field is 0)must not change any capacity:sections=%d coefficient=%d base=%d delay=%d",
			hwMaxSections, hwCoefWords, hwCoefFIRBase, hwDelayWords)
	}

	adoptCapsFrom(dspEngineCaps{NumSections: 24, NumCoefWords: 120, DelayLog2: 8})
	if hwMaxSections != 24 {
		t.Errorf("sections cap should tighten to 24,got %d", hwMaxSections)
	}
	if hwCoefWords != 120 {
		t.Errorf("coefficient word count should be 120,got %d", hwCoefWords)
	}
	if hwCoefFIRBase != 240 {
		t.Errorf("FIR base should be 2*120=240,got %d -- this is wrong convolver coefficients written into biquad space", hwCoefFIRBase)
	}
	if hwDelayWords != 256 {
		t.Errorf("delay ring should be 1<<8=256,got %d", hwDelayWords)
	}
	if bandLimit() > hwMaxSections {
		t.Errorf("bandLimit()=%d exceeds hardware-advertised section count %d", bandLimit(), hwMaxSections)
	}

	hwSFIRMaxTaps, hwSFIROvh, hwCoefSFIRBase = sfirTaps, sfirOvh, coefSFIRBase
	adoptCapsFrom(dspEngineCaps{
		Present: true, Opcodes: capOpcodeSFIR, NumCoefWords: coefWordsPerBank,
		SFIRMaxLog2: 3, SFIRMACS: 8, SFIROverhead: 11,
	})
	if hwSFIRMaxTaps != sfirTaps || hwSFIROvh != sfirOvh {
		t.Errorf("small FIR capper CAP4 take %d taps / overhead %d,got %d / %d",
			sfirTaps, sfirOvh, hwSFIRMaxTaps, hwSFIROvh)
	}
	if hwCoefSFIRBase != coefFIRBase+2*firTaps {
		t.Errorf("small FIR base should be 2*NCOEF + 2*FIR_TAPS = %d,got %d",
			coefFIRBase+2*firTaps, hwCoefSFIRBase)
	}

	adoptCapsFrom(dspEngineCaps{Present: true, Opcodes: 0x0303, NumCoefWords: coefWordsPerBank})
	if hwSFIRMaxTaps != 0 {
		t.Errorf("when bitstream does not advertise small FIR, cap must be forced to 0 (else it would emit OP_SFIR slots the hardware does not recognize), got %d",
			hwSFIRMaxTaps)
	}
}

func TestAnalogXIsLastEffect(t *testing.T) {
	axSave, tubeSave := currentAnalogX, currentTube
	defer func() { currentAnalogX, currentTube = axSave, tubeSave }()
	ax := analogxDefaultParams()
	currentAnalogX = &ax
	currentTube = &tubeParams{}

	nodes, err := buildChainNodes([][5]int32{{32767, 0, 0, 0, 0}}, nil, nil, nil)
	if err != nil {
		t.Fatalf("buildChainNodes：%v", err)
	}

	an, err := analogxNodes(ax, sampleRate)
	if err != nil {
		t.Fatal(err)
	}
	axIdx, tubeIdx := -1, -1
	tubeCoef := tubeNodes()[0].Coefs
	for i, n := range nodes {
		if n.Kind == planKindAnalogX {
			axIdx = i
		}
		if n.Kind == planKindBiquad && n.Coefs == tubeCoef {
			tubeIdx = i
		}
	}
	if axIdx < 0 {
		t.Fatal("in chain not found AnalogX node")
	}
	if tubeIdx < 0 {
		t.Fatal("in chain not found tube sections(confirm first currentTube live)")
	}
	if axIdx < tubeIdx {
		t.Errorf("AnalogX must come after tube (V4A: tube(14) -> AnalogX(15)), got %d vs %d",
			axIdx, tubeIdx)
	}
	if axIdx != len(nodes)-1 {
		t.Errorf("AnalogX should be the last stage (only the limiter comes after it, not in nodes), got position %d of %d",
			axIdx, len(nodes))
	}

	if len(an) != 1 {
		t.Fatalf("AnalogX should only emit one node,got %d", len(an))
	}
}

func TestAnalogXNumbers(t *testing.T) {
	const fs = 48000.0

	pk := analogxPeak(fs)
	db := func(c [5]int32, f float64) float64 {
		return 20 * math.Log10(cmplx.Abs(biquadResponse(c, f, fs)))
	}

	rel := db(pk, 633) - 20*math.Log10(0.8)
	if math.Abs(rel-0.58) > 0.15 {
		t.Errorf("relative boost at 633 Hz with x0.8 should be A² = +0.58 dB (the param_7 branch), got %+.2f dB", rel)
	}

	abs := db(pk, 633)
	if math.Abs(abs-(-1.36)) > 0.4 {
		t.Errorf("absolute gain at 633 Hz should be about -1.36 dB (including the x0.8 from sources), got %+.2f dB", abs)
	}

	var last float64 = 100
	for i, tr := range analogxTiers {
		hp, err := multiBiquad("HP", 0, 240.0, 0.717, fs, false)
		if err != nil {
			t.Fatal(err)
		}
		lp, err := multiBiquad("LP", 0, tr.LPHz, 0.717, fs, false)
		if err != nil {
			t.Fatal(err)
		}
		got := db(lp, 20000) + db(hp, 20000)
		if got >= last {
			t.Errorf("gear %d should be darker than the previous gear at 20 kHz: %+.2f vs %+.2f", i, got, last)
		}
		last = got
	}

	want := [3]struct {
		g, f float64
	}{{0.6, 19650}, {1.2, 18233}, {2.4, 16307}}
	for i, w := range want {
		if analogxTiers[i].Gain != w.g || analogxTiers[i].LPHz != w.f {
			t.Errorf("gear %dconstants should be gain=%g / LP=%g,got %g / %g",
				i, w.g, w.f, analogxTiers[i].Gain, analogxTiers[i].LPHz)
		}
	}
}

func TestAnalogXPlanEmitsFiveSlots(t *testing.T) {
	p := analogxDefaultParams()
	nodes, err := analogxNodes(p, 48000)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("planning failed:%v", err)
	}

	if plan.Slots != 5 {
		t.Errorf("AnalogX should emit 5 slots,got %d", plan.Slots)
	}
	if len(plan.Coefs) != 32 {
		t.Errorf("AnalogX should emit 32 coefficients(5+12+5+5+5),got %d", len(plan.Coefs))
	}
	t.Logf("slots=%d sections=%d coefficient=%d", plan.Slots, plan.Sections, len(plan.Coefs))
}

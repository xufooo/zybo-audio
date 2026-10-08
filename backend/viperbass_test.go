// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
	"math/cmplx"
	"testing"
)

func viperBassPlan(t *testing.T, p viperBassParams) slotPlan {
	t.Helper()
	nodes, err := viperBassNodes(p)
	if err != nil {
		t.Fatalf("compile ViPERBass(%+v) failed:%v", p, err)
	}
	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("plan ViPERBass(%+v) failed:%v", p, err)
	}
	return plan
}

func TestViPERBassModeGating(t *testing.T) {
	p := viperBassDefaultParams()
	if p.Mode != viperBassModeNatural || p.CutoffHz != 40 || p.Gain != 50 {
		t.Errorf("default gear should be Natural Bass / 40 Hz / 50 (V4A client getString default string), got %+v", p)
	}
	if err := viperBassValidate(p); err != nil {
		t.Errorf("default parametersshould validate:%v", err)
	}

	p.Mode = viperBassModePBP
	if err := viperBassValidate(p); err != nil {
		t.Errorf("PBP parameter(40 Hz / gain 50)should validate:%v", err)
	}

	if err := setViPERBass(&viperBassParams{Mode: viperBassModePBP, CutoffHz: 40, Gain: 50}); err == nil {
		t.Error("PBP must be rejected on hardware without small FIR (else the dry path cannot dispatch and only wet remains)")
	}

	p.Mode = viperBassModeSubwoofer
	if err := viperBassValidate(p); err == nil {
		t.Error("Subwoofer gearnot implemented,must fail")
	}

	p.Mode = 3
	if err := viperBassValidate(p); err == nil {
		t.Error("mode 3 missing,must fail")
	}

	p = viperBassDefaultParams()
	p.CutoffHz = 20
	if err := viperBassValidate(p); err == nil {
		t.Error("cutoff 20 Hz not inpanel gears (30...100),must fail")
	}
	p = viperBassDefaultParams()
	p.Gain = 700
	if err := viperBassValidate(p); err == nil {
		t.Error("gain 700 exceeds panel cap 600,must fail")
	}
}

func TestViPERBassSlotPlan(t *testing.T) {
	plan := viperBassPlan(t, viperBassDefaultParams())
	if plan.Slots != 2 {
		t.Fatalf("slot count = %d,expected 2(packed biquad pair + MIX2)", plan.Slots)
	}
	if plan.Sections != 3 {
		t.Errorf("section count = %d,expected 3(scaling 1 + low-pass 1 + MIX2 1)", plan.Sections)
	}
	slots := decodeSlots(t, plan)
	if len(slots) != 2 {
		t.Fatalf("decoded %d slotsdescriptor,expected 2", len(slots))
	}
	if slots[0].Op != opBiquad || slots[0].N != 2 {
		t.Errorf("slot 0should be 2 sectionspacked BIQUAD,got op=%d n=%d", slots[0].Op, slots[0].N)
	}
	if slots[0].In != 0 || slots[0].Out != 1 {
		t.Errorf("slot 0 should be bus0 -> bus1, got %d -> %d", slots[0].In, slots[0].Out)
	}
	if slots[1].Op != opMix2 {
		t.Fatalf("slot 1should be MIX2,got op=%d", slots[1].Op)
	}

	if slots[1].In != 0 || slots[1].InB != 1 || slots[1].Out != 1 {
		t.Errorf("MIX2 should be dry(bus0) + wet(bus1) -> bus1 (in place), got dry=%d wet=%d out=%d",
			slots[1].In, slots[1].InB, slots[1].Out)
	}

	if len(plan.Coefs) != 15 {
		t.Errorf("coefficientword count = %d,expected 15(2 sections x5 + MIX2 weights 2valid words)", len(plan.Coefs))
	}
}

func TestViPERBassPreScaleIsRequired(t *testing.T) {
	b0, b1, b2, _, _ := rbjLowPassFloat(40, viperBassQ, sampleRate)
	for i, v := range []float64{b0, b1, b2} {
		if floatToQ315(v) != 0 {
			t.Fatalf("prerequisite changed: 40 Hz low-pass b%d=%.3e is no longer 0 in Q3.15 (%d)--"+
				"this note and the scaling section need re-review", i, v, floatToQ315(v))
		}
	}

	q := viperBassPlan(t, viperBassDefaultParams())
	pre := [5]int32{q.Coefs[0], q.Coefs[1], q.Coefs[2], q.Coefs[3], q.Coefs[4]}
	if pre[0] != 1024 {
		t.Errorf("scaling sectioncoefficientshould be 1/32 = 1024,got %d", pre[0])
	}
	lp := [5]int32{q.Coefs[5], q.Coefs[6], q.Coefs[7], q.Coefs[8], q.Coefs[9]}
	if lp[0] == 0 || lp[1] == 0 || lp[2] == 0 {
		t.Errorf("after scalinglow-pass numeratorstillis 0(%v)-- effect would vanish entirely", lp)
	}
}

func TestViPERBassGainConvention(t *testing.T) {

	p0 := viperBassDefaultParams()
	p0.Gain = 0
	z := viperBassPlan(t, p0)
	if z.Coefs[11] != 0 {
		t.Errorf("gain=0 wet weightshould be 0(bassFactor=gain/100),got %d", z.Coefs[11])
	}

	for _, c := range []struct {
		gain float64
		want float64
	}{{50, 1.5}, {100, 2.0}, {200, 3.0}, {400, 5.0}, {600, 7.0}} {
		p := viperBassDefaultParams()
		p.Gain = c.gain
		plan := viperBassPlan(t, p)
		pre := [5]int32{plan.Coefs[0], plan.Coefs[1], plan.Coefs[2], plan.Coefs[3], plan.Coefs[4]}
		lp := [5]int32{plan.Coefs[5], plan.Coefs[6], plan.Coefs[7], plan.Coefs[8], plan.Coefs[9]}
		dry := complex(float64(plan.Coefs[10])/32768.0, 0)
		wet := complex(float64(plan.Coefs[11])/32768.0, 0)
		got := cmplx.Abs(dry + wet*biquadResponse(pre, 1e-6, sampleRate)*
			biquadResponse(lp, 1e-6, sampleRate))
		if d := 20 * math.Log10(got/c.want); math.Abs(d) > 1.2 {
			t.Errorf("gain=%g DC gain = %.4f(%+.2f dB),expected %.4f(= 1 + gain/100),tolerance 1.2 dB",
				c.gain, got, d, c.want)
		}
	}
}

func TestViPERBassMaxGainPlanBytes(t *testing.T) {
	p := viperBassParams{Mode: viperBassModeNatural, CutoffHz: 60, Gain: 600}
	plan := viperBassPlan(t, p)
	if len(plan.Coefs) != 15 {
		t.Fatalf("coefficientword count = %d,expected 15", len(plan.Coefs))
	}
	wantPre := []int32{1024, 0, 0, 0, 0}
	wantLP := []int32{24, 48, 24, -65052, 32286}
	wantMix := []int32{32769, 131071}
	for i, w := range wantPre {
		if plan.Coefs[i] != w {
			t.Errorf("scaling section c%d = %d,expected %d", i, plan.Coefs[i], w)
		}
	}
	for i, w := range wantLP {
		if plan.Coefs[5+i] != w {
			t.Errorf("low-pass section c%d = %d,expected %d", i, plan.Coefs[5+i], w)
		}
	}
	for i, w := range wantMix {
		if plan.Coefs[10+i] != w {
			t.Errorf("MIX2 weights c%d = %d,expected %d", i, plan.Coefs[10+i], w)
		}
	}
	if plan.Coefs[5] == 0 || plan.Coefs[6] == 0 || plan.Coefs[7] == 0 {
		t.Errorf("low-pass numeratorquantized to 0 => wet stays 0('no effect' classic cause):%v", plan.Coefs[5:8])
	}
	if plan.Coefs[11] <= 0 {
		t.Errorf("MIX2 wet weight = %d,mustbe positive", plan.Coefs[11])
	}

	wetDC := float64(plan.Coefs[11]) / 32768.0 *
		float64(plan.Coefs[5]+plan.Coefs[6]+plan.Coefs[7]) /
		float64(32768+plan.Coefs[8]+plan.Coefs[9]) *
		(float64(plan.Coefs[0]) / 32768.0)
	if math.Abs(wetDC-6.0) > 0.06 {
		t.Errorf("gain=600 equivalent wet multiple = %.4f,expected 6.0 ± 0.06(= gain/100;folded multiplemustactually multiplied)",
			wetDC)
	}
	if d := 20 * math.Log10((1+wetDC)/7.0); math.Abs(d) > 0.2 {
		t.Errorf("total DC gain %+.2f dB,expected 0 ± 0.2(1 + gain/100 = 7.0)", d)
	}
}

func TestViPERBassMatchesOfficialCore(t *testing.T) {
	const coreGain = 0.998
	meas := []struct {
		gain float64
		freq float64
		rat  float64
	}{
		{50, 20, 1.2936}, {50, 30, 1.1415}, {50, 40, 1.0311},
		{50, 60, 0.9424}, {50, 100, 0.9452}, {50, 200, 0.9784},
		{200, 20, 2.3932}, {200, 30, 1.8899}, {200, 40, 1.4503},
		{200, 60, 0.9414}, {200, 100, 0.8093}, {200, 200, 0.9250},
		{600, 20, 5.5984}, {600, 30, 4.4116}, {600, 40, 3.3107},
		{600, 60, 1.7742}, {600, 100, 0.6757}, {600, 200, 0.7857},
	}
	worst, worstAt := 0.0, ""
	for _, c := range meas {
		p := viperBassDefaultParams()
		p.Gain = c.gain
		plan := viperBassPlan(t, p)

		pre := [5]int32{plan.Coefs[0], plan.Coefs[1], plan.Coefs[2], plan.Coefs[3], plan.Coefs[4]}
		lp := [5]int32{plan.Coefs[5], plan.Coefs[6], plan.Coefs[7], plan.Coefs[8], plan.Coefs[9]}
		dry := complex(float64(plan.Coefs[10])/32768.0, 0)
		wet := complex(float64(plan.Coefs[11])/32768.0, 0)
		h := biquadResponse(pre, c.freq, sampleRate) *
			biquadResponse(lp, c.freq, sampleRate)
		got := cmplx.Abs(dry + wet*h)
		want := c.rat / coreGain
		d := 20 * math.Log10(got/want)
		if math.Abs(d) > worst {
			worst, worstAt = math.Abs(d), fmtGainFreq(c.gain, c.freq)
		}
		if math.Abs(d) > 0.6 {
			t.Errorf("gain=%g f=%g Hz:this machine %.4f,official core %.4f(diff %+.2f dB,tolerance 0.6)",
				c.gain, c.freq, got, want, d)
		}
	}
	t.Logf("ViPERBass NATURAL andofficial core max deviation %.2f dB(at %s)", worst, worstAt)
}

func fmtGainFreq(gain, freq float64) string {
	return fmt.Sprintf("gain=%g f=%gHz", gain, freq)
}

func TestViPERBassChainOrder(t *testing.T) {
	saveBass, saveClarity := currentViPERBass, currentClarity
	defer func() { currentViPERBass, currentClarity = saveBass, saveClarity }()

	b := viperBassDefaultParams()
	currentViPERBass = &b
	cp := clarityDefaultParams()
	currentClarity = &cp

	nodes, err := buildChainNodesWithBass(nil, nil, nil, nil, currentViPERBass)
	if err != nil {
		t.Fatalf("compile failed:%v", err)
	}
	bassAt, clarAt := -1, -1
	for i, n := range nodes {
		if n.Kind == planKindViPERBass {
			bassAt = i
		}
		if n.Kind == planKindBiquad && clarAt < 0 && bassAt >= 0 {
			clarAt = i
		}
	}
	if bassAt < 0 {
		t.Fatal("in chainno ViPERBass node")
	}
	if clarAt < 0 {
		t.Fatal("in chainno Clarity sections")
	}
	if bassAt > clarAt {
		t.Errorf("ViPERBass must come before Clarity (V4A Fidelity Control order), got %d > %d",
			bassAt, clarAt)
	}
}

func TestViPERBassFitsRealChain(t *testing.T) {
	saveE, saveB, saveC, saveX, saveF := currentExciter, currentViPERBass,
		currentClarity, currentCrossfeed, currentFIR
	saveDS := hwDelaySlots
	defer func() {
		currentExciter, currentViPERBass, currentClarity = saveE, saveB, saveC
		currentCrossfeed, currentFIR = saveX, saveF
		hwDelaySlots = saveDS
	}()

	ex := exciterDefaultParams()
	currentExciter = &ex
	b := viperBassDefaultParams()
	b.Gain = 200
	currentViPERBass = &b
	cp := clarityDefaultParams()
	currentClarity = &cp
	currentCrossfeed = &crossfeedParams{FcutHz: 700, Feed: 60}
	currentFIR = &firParams{Taps: firTaps, Name: "probe"}

	secs := make([][5]int32, 8)
	for i := range secs {
		secs[i] = [5]int32{32767, 0, 0, 0, 0}
	}
	nodes, err := buildChainNodesWithBass(secs, &dynParams{GainDB: 6, RefDB: -25, KS: 0.75, AttMs: 5, RelMs: 200},
		currentCrossfeed, nil, currentViPERBass)
	if err != nil {
		t.Fatalf("ViPERBass + exciter + DYN + convolution + OZONE + crossfeed + 8 sections EQ shouldfit:%v", err)
	}
	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("planning failed:%v", err)
	}
	if plan.Slots > slotLimit() {
		t.Errorf("slot count %d exceeds engine %d", plan.Slots, slotLimit())
	}
	if plan.Sections > hwMaxSections {
		t.Errorf("section count %d exceeds engine %d", plan.Sections, hwMaxSections)
	}

	nBass := 0
	for _, n := range nodes {
		if n.Kind == planKindViPERBass {
			nBass++
		}
	}
	if nBass != 1 {
		t.Errorf("on chain ViPERBass node count = %d,expected 1", nBass)
	}
	bassPlan := viperBassPlan(t, b)
	if bassPlan.Slots != 2 || bassPlan.Sections != 3 {
		t.Errorf("ViPERBass itselfshould take 2 slots 3 sections,got %d slots %d sections", bassPlan.Slots, bassPlan.Sections)
	}
}

var viperBassPBPPolyphaseKernel = viperBassPBPKernel

func TestViPERBassPBPMeasuredSpec(t *testing.T) {
	k := viperBassPBPPolyphaseKernel
	sum, argmax := 0.0, 0
	for i, v := range k {
		sum += v
		if math.Abs(v) > math.Abs(k[argmax]) {
			argmax = i
		}
	}
	if argmax != 31 {
		t.Errorf("PBP dry-path main tapatitem %d,measuredisitem 31(output frame 286 frame = 255+31)", argmax)
	}
	if db := 20 * math.Log10(sum); math.Abs(db+13.85) > 0.05 {
		t.Errorf("PBP dry-path DC gain %.4f(%+.2f dB),measured −13.85 dB -- "+
			"once this number changes, the direct+delayed-wet substitute error (8.4 dB at 30 Hz) must be recomputed", sum, db)
	}
	if len(k) != viperBassPBPKernelLen || viperBassPBPKernelLen != 63 {
		t.Errorf("dry-path kernelshould be 63 taps(measured),got %d", len(k))
	}
	if viperBassPBPWetDelay != 64 {
		t.Errorf("wet delaymeasured as 64 samples(aligned RMS=0.0;63/65 are both 0.59),constant is %d",
			viperBassPBPWetDelay)
	}

	nodes, err := viperBassNodes(viperBassParams{Mode: viperBassModePBP, CutoffHz: 40, Gain: 50})
	if err != nil {
		t.Fatalf("PBP should compile now (the hardware gate is in setViPERBass/compiler): %v", err)
	}
	if len(nodes) != 1 || nodes[0].Kind != planKindViPERBassPBP {
		t.Fatalf("PBP shouldbe compiled intoone planKindViPERBassPBP node,got %+v", nodes)
	}
	if nodes[0].Len != viperBassPBPWetDelay {
		t.Errorf("PBP node wet delay = %d,measuredis %d", nodes[0].Len, viperBassPBPWetDelay)
	}
	if len(nodes[0].SFir) != sfirTaps {
		t.Fatalf("dry-path tapsshouldbe padded out to sfirTaps=%d(63kernel + h[63]=0),got %d",
			sfirTaps, len(nodes[0].SFir))
	}
	if nodes[0].SFir[63] != 0 {
		t.Errorf("tap 64must always be 0(itispadding entry,is notpart of the kernel),got %d", nodes[0].SFir[63])
	}

	for i, v := range k {
		if got := float64(nodes[0].SFir[i]) / 32768.0; math.Abs(got-v) > 0.5/32768.0 {
			t.Errorf("PBP taps %d Q3.15 andkernel mismatch:%d(%.6f)vs %.6f",
				i, nodes[0].SFir[i], got, v)
		}
	}
	t.Logf("PBP measured spec:63 taps FIR,main tap@31,DC %+.2f dB,wet delay %d samples"+
		"(Q3.15 already dispatched as: %d taps + 1 zero padding)",
		20*math.Log10(sum), viperBassPBPWetDelay, viperBassPBPKernelLen)
}

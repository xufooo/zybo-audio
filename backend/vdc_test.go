// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
)

func stdRespDB(secs []vdcSection, f float64) float64 {
	w := 2 * math.Pi * f / 48000
	acc := 1.0
	for _, s := range secs {
		b0, b1, b2, A1, A2 := s[0], s[1], s[2], s[3], s[4]
		nr := b0 + b1*math.Cos(w) + b2*math.Cos(2*w)
		ni := -(b1*math.Sin(w) + b2*math.Sin(2*w))
		dr := 1 + A1*math.Cos(w) + A2*math.Cos(2*w)
		di := -(A1*math.Sin(w) + A2*math.Sin(2*w))
		acc *= math.Hypot(nr, ni) / math.Hypot(dr, di)
	}
	return 20 * math.Log10(acc)
}

func TestParseSyntheticVDC(t *testing.T) {
	txt := "SR_44100:1.0,2.0,1.0,0.5,-0.25\nSR_48000:1.0,2.0,1.0,0.4,-0.2,0,0,0,0,0\n"
	secs, rate, err := parseVDC([]byte(txt), 48000)
	if err != nil {
		t.Fatalf("parse failed:%v", err)
	}
	if rate != 48000 {
		t.Errorf("should pick 48000 that line,actual %d", rate)
	}
	if len(secs) != 1 {
		t.Fatalf("should have only 1 sections(all-zero end marker dropped),actual %d", len(secs))
	}
	if secs[0][3] != 0.4 {
		t.Errorf("wrong coefficients:%+v", secs[0])
	}
	if _, _, err := parseVDC([]byte("not a vdc"), 48000); err == nil {
		t.Error("garbage input should fail")
	}
}

func TestQuantisationKeepsResponse(t *testing.T) {

	secs := []vdcSection{{1, 2, 1, 0.49776872571908, -0.92323067076422}}
	q, scale, err := vdcToQ315(secs)
	if err != nil {
		t.Fatalf("quantize failed:%v", err)
	}
	if scale != 1.0 {
		t.Errorf("coefficient in range should not scaling,actual %v", scale)
	}
	for _, f := range []float64{100, 1000, 5000, 10000, 15000} {
		a := vdcResponseDB(secs, f, 48000)
		b := vdcQ315ResponseDB(q, f, 48000)
		if d := math.Abs(a - b); d > 0.5 {
			t.Errorf("%g Hz at pre/post-quantize difference %.2f dB(should < 0.5)", f, d)
		}
	}
}

func TestButterworthVDCIsTenKilohertzLowpass(t *testing.T) {
	p := localDataPath("vdc", "Butterworth.vdc")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("this machine lacks that file vdc(%v)-- skipping", err)
	}
	secs, rate, err := parseVDC(b, 48000)
	if err != nil {
		t.Fatalf("parse failed:%v", err)
	}
	if rate != 48000 {
		t.Errorf("should pick 48000 that line,actual %d", rate)
	}
	t.Logf("section count %d,sample rate %d", len(secs), rate)

	ref := vdcResponseDB(secs, 1000, 48000)
	rel := func(f float64) float64 { return vdcResponseDB(secs, f, 48000) - ref }
	for _, f := range []float64{100, 1000, 3000, 6000} {
		if d := rel(f); math.Abs(d) > 0.5 {
			t.Errorf("%g Hz at passband ripple:%+.2f dB", f, d)
		}
	}
	if d := rel(10000); math.Abs(d-(-3.0)) > 1.0 {
		t.Errorf("10 kHz should be −3 dB low-pass knee,actual %+.2f dB", d)
	}
	if d := rel(16000); d > -20 {
		t.Errorf("16 kHz should already be suppressed (< -20 dB), actual: %+.2f dB", d)
	}

	q, scale, err := vdcToQ315(secs)
	if err != nil {
		t.Fatalf("quantize failed:%v", err)
	}
	t.Logf("quantize scale %.4f", scale)
	for _, f := range []float64{1000, 6000, 10000, 12000, 16000} {
		a := vdcResponseDB(secs, f, 48000)
		c := vdcQ315ResponseDB(q, f, 48000)
		if d := math.Abs(a - c); d > 0.75 {
			t.Errorf("%g Hz at Q3.15 post-quantize deviation %.2f dB", f, d)
		}
	}
}

func resetDDC() {
	ddcMu.Lock()
	ddcSections, ddcName, ddcRate = nil, "", 0
	ddcMu.Unlock()
}

func TestDDCLoadsIntoChainAndClears(t *testing.T) {
	resetDDC()
	defer resetDDC()

	txt := "SR_48000:1.0,2.0,1.0,0.49776872571908,-0.92323067076422\n" +
		"SR_44100:1.0,2.0,1.0,0.5,-0.9\n"
	n, err := setDDCFromVDC([]byte(txt), "probe.vdc")
	if err != nil {
		t.Fatalf("load failed:%v", err)
	}
	if n != 1 {
		t.Fatalf("should have only 1 sections,actual %d", n)
	}
	v := ddcView()
	if v["on"] != true || v["name"] != "probe.vdc" || v["rate"] != 48000 {
		t.Errorf("current state is wrong:%+v", v)
	}
	nodes, err := buildChainNodes(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("chain build failed:%v", err)
	}
	found := 0
	for _, nd := range nodes {
		if nd.Kind == planKindBiquad {
			found++
		}
	}
	if found != 1 {
		t.Errorf("in chain should have 1 sections DDC,actual %d(node %+v)", found, nodes)
	}

	if _, err := buildSlotPlanNodes(nodes); err != nil {
		t.Errorf("single section DDC should not fail planning:%v", err)
	}

	ddcClear()
	if v := ddcView(); v["on"] != false {
		t.Errorf("after clearing should be off:%+v", v)
	}
	if got, _ := buildChainNodes(nil, nil, nil, nil); len(got) != 0 {
		t.Errorf("after clearing in chain should no longer contain node,actual %+v", got)
	}
}

func TestDDCTwentySectionsExceedBudget(t *testing.T) {
	resetDDC()
	defer resetDDC()
	b, err := os.ReadFile(localDataPath("vdc", "Butterworth.vdc"))
	if err != nil {
		t.Skipf("this machine lacks that file vdc(%v)-- skipping", err)
	}
	n, err := setDDCFromVDC(b, "butterworth.vdc")
	if err != nil {
		t.Fatalf("load failed:%v", err)
	}
	t.Logf("Butterworth.vdc section count = %d(engine cap %d sections)", n, maxSections)
	nodes, err2 := buildChainNodes(nil, nil, nil, nil)
	if err2 != nil {
		t.Fatalf("chain build failed:%v", err2)
	}
	if n <= maxSections {
		if _, err := buildSlotPlanNodes(nodes); err != nil {
			t.Errorf("section count at within limits but planning failed:%v", err)
		}
		return
	}
	if _, err := buildSlotPlanNodes(nodes); err == nil {
		t.Error("20 sections exceeds the 16-section cap; the planner should fail loudly (no silent truncation)")
	} else {
		t.Logf("planner fails loudly as it should: %v", err)
	}
}

func TestDDCFitFromRealVDC(t *testing.T) {
	resetDDC()
	defer resetDDC()
	b, err := os.ReadFile(localDataPath("vdc", "Butterworth.vdc"))
	if err != nil {
		t.Skipf("this machine lacks that file vdc(%v)-- skipping", err)
	}
	n, fc, dev, err := fitDDCFromVDC(b, "butterworth.vdc", 4)
	if err != nil {
		t.Fatalf("fit failed:%v", err)
	}
	t.Logf("fit %d sections,−3 dB point %.1f Hz,from original file max deviation %.2f dB", n, fc, dev)
	if n != 4 {
		t.Errorf("should fit 4 sections,actual %d", n)
	}
	if math.Abs(fc-10000) > 500 {
		t.Errorf("−3 dB point should ≈10 kHz,actual %.0f Hz", fc)
	}

	q := sectionsToQ315Engine(butterworthLP(4, fc, 48000))
	ref := vdcQ315ResponseDB(q, 1000, 48000)
	rel := func(f float64) float64 { return vdcQ315ResponseDB(q, f, 48000) - ref }
	for _, f := range []float64{100, 200, 500, 2000, 6000} {
		if d := rel(f); math.Abs(d) > 0.5 {
			t.Errorf("after fitting %g Hz passband ripple:%+.2f dB", f, d)
		}
	}
	if d := rel(fc); math.Abs(d-(-3.0)) > 0.5 {
		t.Errorf("after fitting −3 dB point should falls %.0f Hz on,actual %+.2f dB", fc, d)
	}
	prev := 0.0
	for _, f := range []float64{2000, 4000, 6000, 8000, 10000, 12000, 16000} {
		v := rel(f)
		if v > prev+0.01 {
			t.Errorf("after fitting %g Hz atis not monotonic roll-off(%+.2f dB,previous gear %+.2f)", f, v, prev)
		}
		prev = v
	}
	t.Logf("reference:from original shape max deviation %.2f dB(print only,not a criterion)", dev)

	nodes, err := buildChainNodes(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("chain build failed:%v", err)
	}
	eq7 := make([]planNode, 7)
	for i := range eq7 {
		eq7[i] = planNode{Kind: planKindBiquad, Coefs: [5]int32{1 << 15, 0, 0, 0, 0}}
	}
	if _, err := buildSlotPlanNodes(append(eq7, nodes...)); err != nil {
		t.Errorf("4 sections DDC + 7 sections EQ should fit(16 sections budget):%v", err)
	}
}

func TestSixteenSectionDDCWithFIRFitsSlots(t *testing.T) {
	b, err := os.ReadFile(localDataPath("vdc", "Butterworth.vdc"))
	if err != nil {
		t.Skip("this machine lacks that file .vdc")
	}
	if _, _, _, err := fitDDCFromVDC(b, "probe16.vdc", 16); err != nil {
		t.Fatalf("16 sections fit failed:%v", err)
	}
	defer ddcClear()
	ddcMu.Lock()
	n := len(ddcSections)
	ddcMu.Unlock()
	if n != 16 {
		t.Fatalf("should hold 16 sections,actual %d", n)
	}
	nodes := append(ddcNodes(), planNode{Kind: planKindFIR, Blocks: firTaps / firMACS})
	p, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("16 sections DDC + FIR planning failed:%v", err)
	}
	if p.Slots > 8 {
		t.Errorf("needs %d slots > NSLOT=8", p.Slots)
	}
	if p.Slots != 7 {
		t.Logf("slot count = %d (expected 6 section-slots + 1 FIR = 7)", p.Slots)
	}
}

func TestDDCFitFidelityImprovesWithSections(t *testing.T) {
	b, err := os.ReadFile(localDataPath("vdc", "Butterworth.vdc"))
	if err != nil {
		t.Skip("this machine lacks that file .vdc")
	}
	src, _, err := parseVDC(b, 48000)
	if err != nil {
		t.Fatal(err)
	}
	srcRef := vdcResponseDB(src, 1000, 48000)
	src12 := vdcResponseDB(src, 12000, 48000) - srcRef
	if src12 > -60 {
		t.Fatalf("prerequisite changed:source file 12 kHz only %.1f dB,no longer is brick-wall", src12)
	}

	at12 := func(fit []vdcSection) (float64, float64) {
		q := sectionsToQ315Engine(fit)
		fr := stdRespDB(fit, 1000)
		qr := vdcQ315ResponseDB(q, 1000, 48000)
		return stdRespDB(fit, 12000) - fr, vdcQ315ResponseDB(q, 12000, 48000) - qr
	}

	fc := vdcMinus3dB(src, 48000)
	f4, _ := at12(butterworthLP(4, fc, 48000))
	f8, _ := at12(butterworthLP(8, fc, 48000))
	f16, q16 := at12(butterworthLP(16, fc, 48000))

	if !(f4 > f8 && f8 > f16) {
		t.Errorf("more sections should mean steeper: 4 sections %.1f / 8 sections %.1f / 16 sections %.1f dB", f4, f8, f16)
	}
	if f16 > -70 {
		t.Errorf("16 sections at 12 kHz only reaches %.1f dB,from brick-wall(%.1f dB)too far", f16, src12)
	}
	if d := math.Abs(q16 - f16); d > 0.1 {
		t.Errorf("Q3.15 fixed-point adds %.2f dB error at 12 kHz (should be negligible)", d)
	}

	if d := math.Abs((vdcQ315ResponseDB(sectionsToQ315Engine(butterworthLP(16, fc, 48000)), 10000, 48000) -
		vdcQ315ResponseDB(sectionsToQ315Engine(butterworthLP(16, fc, 48000)), 1000, 48000)) - (-3.0)); d > 0.3 {
		t.Errorf("16 sections fit at knee at is not −3 dB(deviation %.2f)", d)
	}
}

func TestDDCSectionCountSurvivesNamedReload(t *testing.T) {
	savedChain := currentUserChain
	defer func() { currentUserChain = savedChain; ddcClear() }()
	currentUserChain = nil

	ensureChainItem("ddc", "probe.vdc")
	setChainItemParam("ddc", "sections", 16)
	if got := chainItemParam("ddc", "sections", 4); got != 16 {
		t.Fatalf("section count in chain item should be 16,actual %v", got)
	}

	currentUserChain = nil
	if got := chainItemParam("ddc", "sections", 4); got != 4 {
		t.Errorf("no parameter should use default 4,actual %v", got)
	}

	b, err := os.ReadFile(localDataPath("vdc", "Butterworth.vdc"))
	if err != nil {
		t.Skip("this machine lacks that file .vdc")
	}
	n16, _, _, err := fitDDCFromVDC(b, "probe.vdc", 16)
	if err != nil || n16 != 16 {
		t.Fatalf("16 sections fit failed:n=%d err=%v", n16, err)
	}
	ddcMu.Lock()
	if len(ddcSections) != 16 {
		t.Errorf("after fitting should be 16 sections,actual %d", len(ddcSections))
	}
	ddcMu.Unlock()
}

func simCascade(q [][5]int32, in []int32) (out []int32, maxState int64, satOut, satState int) {
	const lim = int64(1)<<23 - 1
	x1 := make([]int64, len(q))
	x2 := make([]int64, len(q))
	y1 := make([]int64, len(q))
	y2 := make([]int64, len(q))
	out = make([]int32, len(in))
	clamp := func(v int64) int64 {
		if v > lim {
			return lim
		}
		if v < -lim-1 {
			return -lim - 1
		}
		return v
	}
	for n, xin := range in {
		x := int64(xin)
		for i, c := range q {
			acc := int64(c[0])*x + int64(c[1])*x1[i] + int64(c[2])*x2[i] -
				int64(c[3])*y1[i] - int64(c[4])*y2[i]
			y := acc >> 15
			if y > lim || y < -lim-1 {
				satOut++
			}
			y = clamp(y)
			x2[i], x1[i] = x1[i], x
			y2[i], y1[i] = y1[i], y
			for _, v := range []int64{x1[i], y1[i], y2[i]} {
				if a := v; a > maxState {
					maxState = a
				} else if -a > maxState {
					maxState = -a
				}
			}
			x = y
		}
		out[n] = int32(x)
	}
	return
}

func TestDDCCascadeDoesNotSaturate(t *testing.T) {
	b, err := os.ReadFile(localDataPath("vdc", "Butterworth.vdc"))
	if err != nil {
		t.Skip("this machine lacks that file .vdc")
	}
	src, _, _ := parseVDC(b, 48000)
	fc := vdcMinus3dB(src, 48000)
	lim := int64(1)<<23 - 1

	small := make([]int32, 24000)
	for i := range small {
		small[i] = int32(0.001 * float64(lim) * math.Sin(2*math.Pi*2000*float64(i)/48000))
	}
	q := sectionsToQ315Engine(butterworthLP(4, fc, 48000))
	out, _, satSmall, _ := simCascade(q, small)
	if satSmall != 0 {
		t.Fatalf("small signal should not saturate(%d times)", satSmall)
	}
	var peakOut float64
	for _, v := range out[len(out)/2:] {
		if a := math.Abs(float64(v)); a > peakOut {
			peakOut = a
		}
	}
	simDB := 20 * math.Log10(peakOut/(0.001*float64(lim)))
	refDB := stdRespDB(butterworthLP(4, fc, 48000), 2000) - stdRespDB(butterworthLP(4, fc, 48000), 1000)
	if d := math.Abs(simDB - refDB); d > 0.5 {
		t.Fatalf("simulated fixed-point semantics and float response mismatch(%.2f dB vs %.2f dB)-- conclusion unreliable", simDB, refDB)
	}

	for _, nsec := range []int{4, 8, 16} {
		in := make([]int32, 48000)
		for i := range in {
			v := 0.32*math.Sin(2*math.Pi*200*float64(i)/48000) +
				0.32*math.Sin(2*math.Pi*3000*float64(i)/48000) +
				0.32*math.Sin(2*math.Pi*8000*float64(i)/48000)
			in[i] = int32(v * float64(lim))
		}
		_, maxState, sat, _ := simCascade(sectionsToQ315Engine(butterworthLP(nsec, fc, 48000)), in)
		if sat != 0 {
			t.Errorf("n=%d sections saturate %d times under near-full-scale multitone (state peak %.2f FS)-- audible as clipping noise",
				nsec, sat, float64(maxState)/float64(lim))
		}
	}
}

func TestConvolutionClippingDependsOnHeadroom(t *testing.T) {
	b, err := os.ReadFile(localDataPath("irs", "thepbone-clear_bass-audio.irs"))
	if err != nil {
		t.Skip("this machine lacks that IR")
	}
	resetIRState()
	defer resetIRState()
	if _, err := loadIRInto(b, "clip-probe"); err != nil {
		t.Fatal(err)
	}
	currentFIR = &firParams{Taps: firTaps, Name: "clip-probe"}
	defer func() { currentFIR = nil }()
	coef := irBank[0]
	const FS = int64(1)<<23 - 1
	const HEADROOM = 3

	n := 24000
	base := make([]int32, n)
	for i := 0; i < n; i++ {
		base[i] = int32(math.Sin(2*math.Pi*1000*float64(i)/48000) * float64(FS))
	}
	run := func(shift uint) int {
		in := make([]int32, n)
		for i, v := range base {
			in[i] = v >> shift
		}
		sat := 0
		for i := 0; i < n; i++ {
			var acc int64
			for k := 0; k < len(coef) && k <= i; k++ {
				acc += int64(in[i-k]) * int64(coef[k])
			}
			y := acc >> 15

			if y > FS || y < -FS-1 {
				sat++
			}
		}
		return sat
	}
	noHR := run(0)
	withHR := run(HEADROOM)
	if noHR == 0 {
		t.Errorf("prerequisite no longer holds: headroom off should have clipped (measured 0)-- this IR gain changed, revisit the criterion")
	}
	if withHR != 0 {
		t.Errorf("enabled in-chain headroom(/%d)after still clipping %d times -- 18 dB cannot cover this IR(worst-case gain %+.2f dB)",
			1<<HEADROOM, withHR, firGainDBForHeadroom())
	}
	t.Logf("headroom off: clipping %d/%d samples;headroom on(/%d): clipping %d(worst-case gain %+.2f dB,headroom 18 dB)",
		noHR, n, 1<<HEADROOM, withHR, firGainDBForHeadroom())
}

func TestDDCNativeLoadsFileSections(t *testing.T) {

	secs := butterworthLP(20, 10000, 48000)
	if len(secs) != 20 {
		t.Fatalf("butterworthLP should yield 20 sections,actual %d", len(secs))
	}
	var sb strings.Builder
	sb.WriteString("SR_48000:")
	for _, s := range secs {

		fmt.Fprintf(&sb, "%.15g,%.15g,%.15g,%.15g,%.15g,", s[0], s[1], s[2], -s[3], -s[4])
	}
	sb.WriteString("\n")

	got, rate, err := parseVDC([]byte(sb.String()), 48000)
	if err != nil {
		t.Fatalf("parseVDC：%v", err)
	}
	if rate != 48000 || len(got) != 20 {
		t.Fatalf("parsed %d sections @%d Hz,expected 20 sections @48000", len(got), rate)
	}

	qNative, scale, err := vdcToQ315(got)
	if err != nil {
		t.Fatalf("vdcToQ315：%v", err)
	}
	if scale != 1.0 {
		t.Errorf("this coefficient range should not trigger scaling,actual scale=%v", scale)
	}

	qRef := sectionsToQ315Engine(secs)

	for _, f := range []float64{20, 100, 1000, 5000, 9000, 10000, 11000, 12000, 18000} {
		a := vdcQ315ResponseDB(qNative, f, 48000)
		b := vdcQ315ResponseDB(qRef, f, 48000)
		if math.Abs(a-b) > 0.05 {
			t.Errorf("%.0f Hz:as-is load %.2f dB vs direct quantization %.2f dB(diff %.2f,sign convention or quantize mismatch)",
				f, a, b, a-b)
		}
	}

	if d := vdcQ315ResponseDB(qNative, 12000, 48000) - vdcQ315ResponseDB(qNative, 1000, 48000); d > -40 {
		t.Errorf("12 kHz relative 1 kHz only suppress %.1f dB,20 sections Butterworth should not be this shallow", d)
	}
}

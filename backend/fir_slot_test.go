// SPDX-License-Identifier: GPL-2.0-only
package main

import "testing"

func TestChainTypeMapAcceptsConvolver(t *testing.T) {
	for _, k := range []string{"fir", "convolver", "ir"} {
		if got := chainTypeToSlot[k]; got != "FIR" {
			t.Errorf("%q shouldmap to FIR,actual %q", k, got)
		}
	}

	if why, ok := chainPlannedType["convolver"]; ok {
		t.Errorf("convolver is implemented and must not be in chainPlannedType (currently %q)", why)
	}
}

func TestSlotPlanEmitsFIRSlot(t *testing.T) {
	nodes := []planNode{
		{Kind: planKindBiquad, Coefs: [5]int32{1 << 15, 0, 0, 0, 0}},
		{Kind: planKindFIR},
	}
	p, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("planning failed:%v", err)
	}

	var cfg uint32
	found := false
	for _, w := range p.Words {
		if w.Addr%slotWordSize != swCfg {
			continue
		}
		if int(w.Value&0xFF) == opFIR {
			found = true
			cfg = w.Value
		}
	}
	if !found {
		t.Fatalf("noplanned FIR slot(word count %d)", len(p.Words))
	}

	wantLg := 0
	for (1 << wantLg) < firTaps/firMACS {
		wantLg++
	}
	if n := int((cfg >> 16) & 0xFF); n != wantLg {
		t.Errorf("FIR slot n should be log2(%d)=%d,actual %d(8 bitsdoes not fitblock count,mustuselog encoding)",
			firTaps/firMACS, wantLg, n)
	}
	if taps := firMACS << ((cfg >> 16) & 0xFF); taps != firTaps {
		t.Errorf("decoded as taps should be %d(firMACS=%d),actual %d", firTaps, firMACS, taps)
	}

	if len(p.Coefs) != 5 {
		t.Errorf("FIR should not occupy EQ coefficientspace:expected 5words(one biquad),actual %d", len(p.Coefs))
	}
}

func TestSlotPlanRejectsNonPowerOfTwoBlocks(t *testing.T) {
	nodes := []planNode{{Kind: planKindFIR, Blocks: 300}}
	if _, err := buildSlotPlanNodes(nodes); err == nil {
		t.Error("300 blocks is not a power of 2, should fail (slot CFG n is log2-encoded)")
	}
	nodes = []planNode{{Kind: planKindFIR, Blocks: 1 << 20}}
	if _, err := buildSlotPlanNodes(nodes); err == nil {
		t.Error("exceeds firTaps block countshouldfail")
	}
}

func TestChainWithConvolverPlansFIRSlot(t *testing.T) {
	defer func() { currentFIR = nil }()
	applyChainEffects([]ChainItem{
		{Type: "peq", Enabled: true, Freq: 1000, GainDB: 0},
		{Type: "convolver", Enabled: true, Params: map[string]float64{"taps": 4096}},
	})
	if currentFIR == nil {

		if firAvailable() {
			t.Fatal("has FIR capabilitybutnot set currentFIR")
		}
		t.Skip("this environmentno FIR capabilitybits(not on hardware/old bitstream):alreadyconfirmednotset currentFIR")
	}
	nodes, err := buildChainNodes(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("chain build failed:%v", err)
	}
	found := false
	for _, n := range nodes {
		if n.Kind == planKindFIR {
			found = true
			if got := firMACS * n.Blocks; got != firTaps {
				t.Errorf("FIR nodeshould be %d taps,actual %d", firTaps, got)
			}
		}
	}
	if !found {
		t.Error("chain has convolver but generated no FIR node")
	}

	applyChainEffects([]ChainItem{{Type: "convolver", Enabled: false}})
	if currentFIR != nil {
		t.Error("chain item disabled shouldclear currentFIR")
	}
}

func TestBuildChainNodesIncludesFIRDeterministic(t *testing.T) {
	defer func() { currentFIR = nil }()
	currentFIR = &firParams{Taps: firTaps, Name: "probe.irs"}
	nodes, err := buildChainNodes(nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("chain build failed:%v", err)
	}
	if len(nodes) != 1 || nodes[0].Kind != planKindFIR {
		t.Fatalf("should have onlyone FIR node,actual %+v", nodes)
	}
	if got := firMACS * nodes[0].Blocks; got != firTaps {
		t.Errorf("should fill by default %d taps,actual %d", firTaps, got)
	}
	p, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("planning failed:%v", err)
	}

	wantLg := 0
	for (1 << wantLg) < firTaps/firMACS {
		wantLg++
	}
	seen := false
	for _, w := range p.Words {
		if w.Addr%slotWordSize == swCfg && int(w.Value&0xFF) == opFIR {
			seen = true
			if n := int((w.Value >> 16) & 0xFF); n != wantLg {
				t.Errorf("FIR slot n should be %d(log2 %d blocks),actual %d", wantLg, firTaps/firMACS, n)
			}
		}
	}
	if !seen {
		t.Error("plan result no FIR slot")
	}
}

func TestEnsureChainItemAutoWires(t *testing.T) {
	saved := currentUserChain
	defer func() { currentUserChain = saved }()
	currentUserChain = nil

	ensureChainItem("convolver", "a.irs")
	if len(currentUserChain) != 1 || currentUserChain[0].Name != "a.irs" ||
		!currentUserChain[0].Enabled {
		t.Fatalf("gap-fill failed:%+v", currentUserChain)
	}

	ensureChainItem("fir", "b.irs")
	if len(currentUserChain) != 1 {
		t.Fatalf("already existsconvolver stagebutagaininserted an extra item:%+v", currentUserChain)
	}
	if currentUserChain[0].Name != "b.irs" {
		t.Errorf("nameshouldupdated to b.irs,actual %q", currentUserChain[0].Name)
	}

	ensureChainItem("ddc", "c.vdc")
	if len(currentUserChain) != 2 {
		t.Fatalf("DDC itemsmissing:%+v", currentUserChain)
	}
	if got := chainEffectType[normalizeChainType("vdc")]; got != "ddc" {
		t.Errorf(".vdc shouldnormalized to ddc,actual %q", got)
	}

	dropChainItem("ddc")
	if len(currentUserChain) != 1 || currentUserChain[0].Name != "b.irs" {
		t.Fatalf("after removing DDC only the convolver item should remain: %+v", currentUserChain)
	}
	dropChainItem("convolver")
	if len(currentUserChain) != 0 {
		t.Errorf("convolver itemshould beremoved:%+v", currentUserChain)
	}

	currentUserChain = []ChainItem{{Type: "peq", Enabled: true}, {Type: "crossfeed", Enabled: true}}
	dropChainItem("ddc")
	if len(currentUserChain) != 2 {
		t.Errorf("must not touchothereffect:%+v", currentUserChain)
	}
}

func TestIRUnloadClearsLevelAndChainItem(t *testing.T) {
	savedChain, savedFIR := currentUserChain, currentFIR
	defer func() { currentUserChain, currentFIR = savedChain, savedFIR }()

	currentUserChain = nil
	ensureChainItem("convolver", "x.irs")
	currentFIR = &firParams{Taps: firTaps, Name: "x.irs"}
	if len(currentUserChain) != 1 {
		t.Fatalf("precondition failed:%+v", currentUserChain)
	}

	irUnload()
	if currentFIR != nil {
		t.Error("after uninstalling currentFIR shouldis nil(otherwiseslot countalwaysshow'has convolver')")
	}
	if len(currentUserChain) != 0 {
		t.Errorf("after uninstallingin chainshould no longer containconvolver item:%+v", currentUserChain)
	}
	if got := irView()["in_chain"]; got != false {
		t.Errorf("irView().in_chain should truthfully report false,actual %v", got)
	}

	currentFIR = &firParams{Taps: firTaps, Name: "y.irs"}
	ensureChainItem("convolver", "y.irs")
	if got := irView()["in_chain"]; got != true {
		t.Errorf("after reinstalling in_chain should be true,actual %v", got)
	}
	if len(currentUserChain) != 1 || currentUserChain[0].Name != "y.irs" {
		t.Errorf("reinstalled chain itemis wrong:%+v", currentUserChain)
	}
}

func TestFIRCoefAddressSpaceFitsHardwareCidx(t *testing.T) {
	const cidxMax = 65535
	last := coefFIRBase + 2*firTaps - 1
	if last > cidxMax {
		t.Fatalf("FIR coefficientsend address %d exceedshardware CIDX cap %d(willtruncate)", last, cidxMax)
	}
	if coefFIRBase <= 128 {
		t.Errorf("coefFIRBase=%d did not avoid EQ coefficientspace(128 words),willclobber each other", coefFIRBase)
	}
	if len(irPlanCoefs()) != 0 {
		t.Errorf("with no pending IR, irPlanCoefs() should be empty (avoid moving 8192 words on every dispatch)")
	}

	bank := make([]int32, firTaps)
	for i := range bank {
		bank[i] = int32(i%7) - 3
	}
	irMu.Lock()
	irBank = [2][]int32{bank, bank}
	irMeta = &IRInfo{Taps: firTaps}
	irName = "addr-probe.irs"
	irDirty = true
	irMu.Unlock()

	got := irPlanCoefs()
	if len(got) != 2*firTaps {
		t.Fatalf("coefficientcount should be %d(2 channels x %d taps),actual %d", 2*firTaps, firTaps, len(got))
	}
	for i, v := range got {
		if int32(int16(v)) != v && v != 0 {
			t.Errorf("coefficient %d = %d exceeds the 18-bit Q3.15 representable range", i, v)
			break
		}
	}

	irMu.Lock()
	irBank, irMeta, irName, irDirty = [2][]int32{}, nil, "", false
	irMu.Unlock()
}

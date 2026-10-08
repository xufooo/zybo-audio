// SPDX-License-Identifier: GPL-2.0-only
package main

import "testing"

type decSlot struct {
	Op, N, Flags int
	Cfb, Stb     int
	In, InB, Out int
}

func decodeSlots(t *testing.T, p slotPlan) []decSlot {
	t.Helper()
	out := make([]decSlot, p.Slots)
	for _, w := range p.Words {
		slot, field := w.Addr/slotWordSize, w.Addr%slotWordSize
		if slot >= len(out) {
			t.Fatalf("descriptor written to slot %d, but plan holds only %d slots (table corrupted)", slot, len(out))
		}
		switch field {
		case swCfg:
			out[slot].Op = int(w.Value & 0xFF)
			out[slot].Flags = int((w.Value >> 8) & 0xFF)
			out[slot].N = int((w.Value >> 16) & 0xFF)
		case swCfb:
			out[slot].Cfb = int(w.Value)
		case swStb:
			out[slot].Stb = int(w.Value)
		case swInA:
			out[slot].In = int(w.Value)
		case swInB:
			out[slot].InB = int(w.Value)
		case swOutB:
			out[slot].Out = int(w.Value)
		}
	}
	return out
}

func xhifiWithDelaySlots(t *testing.T, slots int, f func()) {
	t.Helper()
	save := hwDelaySlots
	hwDelaySlots = slots
	defer func() { hwDelaySlots = save }()
	f()
}

func TestXHIFINodeNumbers(t *testing.T) {
	for _, c := range []struct {
		level float64
		gain  float64
	}{{0, 1.0}, {50, 1.5}, {100, 2.0}} {
		n, err := xhifiNode(c.level, sampleRate)
		if err != nil {
			t.Fatalf("level=%g：%v", c.level, err)
		}
		if n.Kind != planKindXHIFI {
			t.Fatalf("level=%g：kind=%q", c.level, n.Kind)
		}
		wantHP := q315Round(1.2 * c.gain)
		wantBP := q315Round(c.gain)
		if n.Mix[0] != wantHP || n.Mix[1] != wantBP {
			t.Errorf("level=%g:first stageweights (%d,%d),want (%d,%d)",
				c.level, n.Mix[0], n.Mix[1], wantHP, wantBP)
		}
		if n.MixB[0] != q315Round(1.0) || n.MixB[1] != q315Round(1.0) {
			t.Errorf("level=%g:second stageweights %v,wantall 1", c.level, n.MixB)
		}
		if n.Flags != 120 || n.Len != 240 {
			t.Errorf("level=%g:branch delay %d/%d samples,want 120/240(48k fs/400,fs/200)",
				c.level, n.Flags, n.Len)
		}
	}
}

func TestXHIFINodeRange(t *testing.T) {
	if _, err := xhifiNode(101, sampleRate); err == nil {
		t.Error("level=101 should fail")
	}
	if _, err := xhifiNode(-1, sampleRate); err == nil {
		t.Error("level=-1 should fail")
	}
}

func TestXHIFIPlanShape(t *testing.T) {
	xhifiWithDelaySlots(t, 4, func() {
		p, err := buildSlotPlanNodes([]planNode{mustXHIFINode(t, 50)})
		if err != nil {
			t.Fatalf("buildSlotPlanNodes：%v", err)
		}
		if p.Slots != 8 {
			t.Errorf("slot count = %d,want 8(3multi-section biquad + 2 delay + 1single section + 2 MIX2)", p.Slots)
		}
		if len(p.Coefs) != 70 {
			t.Errorf("coefficientwords = %d,want 70(3+3+3+1 sections x5 + 2delay lengths + 2 groupsweights x5)", len(p.Coefs))
		}
		slots := decodeSlots(t, p)

		var dl []int
		for i, sl := range slots {
			if sl.Op == opDelay {
				dl = append(dl, i)
			}
		}
		if len(dl) != 2 {
			t.Fatalf("%d delay slots, want 2", len(dl))
		}
		l0 := p.Coefs[slots[dl[0]].Cfb]
		l1 := p.Coefs[slots[dl[1]].Cfb]
		if l0 != 120 || l1 != 240 {
			t.Errorf("twobranch delay = %d/%d samples,want 120/240", l0, l1)
		}
		o0, o1 := slots[dl[0]].Stb, slots[dl[1]].Stb
		if o0 != 0 {
			t.Errorf("slot onedelay in-ring offset = %d,want 0", o0)
		}
		if o1 < int(l0) {
			t.Errorf("second delay slot offset %d overlaps the first (length %d) -- two slots will clobber each other", o1, l0)
		}
		if o1+int(l1) > hwDelayWords {
			t.Errorf("second delay slot [%d,%d) exceeds ring capacity %d", o1, o1+int(l1), hwDelayWords)
		}

		last := slots[len(slots)-1]
		if last.Out != slots[0].In+1 {
			t.Errorf("last slotoutput bus = %d,wantinput bus %d + 1", last.Out, slots[0].In)
		}

		used := map[int]bool{}
		for _, sl := range slots {
			for _, b := range []int{sl.In, sl.Out} {
				if b != 0xFFFF {
					used[b] = true
				}
			}
		}
		base := slots[0].In
		n := 0
		for b := range used {
			if b >= base {
				n++
			}
		}
		if n > 5 {
			t.Errorf("XHIFI takes %d buses (base %d), cap is 5 -- "+
				"as buses grow, the real chain (DDC+convolver+XHIFI) collides with engine reserved numbers 14/15 and refuses dispatch", n, base)
		}
	})
}

func TestXHIFIRejectedOnOldBitstream(t *testing.T) {
	xhifiWithDelaySlots(t, 1, func() {
		_, err := buildSlotPlanNodes([]planNode{mustXHIFINode(t, 50)})
		if err == nil {
			t.Fatal("hwDelaySlots=1 XHIFI(needstwodelay slots)should bereject")
		}
		if !contains(err.Error(), "delay slots") {
			t.Errorf("error message should make clear that delay slots are insufficient: %v", err)
		}
	})
}

func TestDelayOffsetAllocation(t *testing.T) {
	xhifiWithDelaySlots(t, 4, func() {
		p, err := buildSlotPlanNodes([]planNode{
			{Kind: planKindDelay, Len: 1000},
			{Kind: planKindDelay, Len: 2000},
		})
		if err != nil {
			t.Fatalf("twoshort delayshould fit:%v", err)
		}
		sl := decodeSlots(t, p)
		if sl[0].Stb != 0 || sl[1].Stb != 1000 {
			t.Errorf("offset = %d/%d,want 0/1000", sl[0].Stb, sl[1].Stb)
		}

		p1, err := buildSlotPlanNodes([]planNode{{Kind: planKindDelay, Len: hwDelayWords}})
		if err != nil {
			t.Fatalf("singleslotsuse upwhole ringshould be allowed:%v", err)
		}
		if sl1 := decodeSlots(t, p1); sl1[0].Stb != 0 {
			t.Errorf("single delay slotoffset = %d,want 0", sl1[0].Stb)
		}

		if _, err := buildSlotPlanNodes([]planNode{
			{Kind: planKindDelay, Len: hwDelayWords},
			{Kind: planKindDelay, Len: hwDelayWords},
		}); err == nil {
			t.Error("twoslotscombinedexceeds ring capacityshould reject")
		}
	})
}

func TestClarityXHIFIThroughChain(t *testing.T) {
	clrSave := currentClarity
	defer func() { currentClarity = clrSave }()
	clr := clarityParams{Mode: clarityModeXHIFI, Level: 50}
	currentClarity = &clr

	nodes, err := clarityNodes(clr, sampleRate)
	if err != nil {
		t.Fatalf("clarityNodes(xhifi)：%v", err)
	}
	if len(nodes) != 1 || nodes[0].Kind != planKindXHIFI {
		t.Fatalf("clarityNodes(xhifi) = %+v,wantone planKindXHIFI", nodes)
	}
	xhifiWithDelaySlots(t, 4, func() {
		if _, err := buildSlotPlanNodes(nodes); err != nil {
			t.Errorf("bitstream supportstwodelay slotsshould compile:%v", err)
		}
	})
	xhifiWithDelaySlots(t, 1, func() {
		if _, err := buildSlotPlanNodes(nodes); err == nil {
			t.Error("bitstream only supportsonedelay slotsshould reject")
		}
	})
}

func mustXHIFINode(t *testing.T, level float64) planNode {
	t.Helper()
	n, err := xhifiNode(level, sampleRate)
	if err != nil {
		t.Fatalf("xhifiNode：%v", err)
	}
	return n
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

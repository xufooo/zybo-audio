// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"os"
	"strings"
	"testing"
)

const (
	realChainVDCDefault = "/home/ooo/.config/jamesdsp/vdc/mh750.vdc"
	realChainIRDefault  = "/home/ooo/.config/jamesdsp/irs/thepbone-clear_bass-audio.irs"
)

func realChainNodes(t *testing.T, gear float64, bassMode int) []planNode {
	return realChainNodesEx(t, gear, bassMode, false)
}

func realChainNodesEx(t *testing.T, gear float64, bassMode int, withColorful bool) []planNode {
	t.Helper()

	vdcPath := os.Getenv("VIPERBASS_VDC")
	if vdcPath == "" {
		vdcPath = realChainVDCDefault
	}
	body, err := os.ReadFile(vdcPath)
	if err != nil {
		t.Skipf("this machine lacks %s(private asset,not in repo):skipping real-chain capacity check", vdcPath)
	}
	secs, rate, err := parseVDC(body, 48000)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", vdcPath, err)
	}
	if float64(rate) != sampleRate {
		t.Fatalf("%s sample rate = %d,expected %g(engine convention)", vdcPath, rate, sampleRate)
	}
	ddc, _, err := vdcToQ315(secs)
	if err != nil {
		t.Fatalf("failed to quantize %s: %v", vdcPath, err)
	}
	if len(ddc) != 18 {
		t.Fatalf("%s should carry 18 sections (three-axis expansion was sized for it), got %d", vdcPath, len(ddc))
	}

	irPath := os.Getenv("ZYBO_IR_FILE")
	if irPath == "" {
		irPath = realChainIRDefault
	}
	irBody, err := os.ReadFile(irPath)
	if err != nil {
		t.Skipf("this machine lacks %s(private asset,not in repo):skipping real-chain capacity check", irPath)
	}
	banks, _, err := loadIR(irBody)
	if err != nil {
		t.Fatalf("failed to load %s: %v", irPath, err)
	}
	if len(banks[0]) != irTaps || len(banks[1]) != irTaps {
		t.Fatalf("%s loaded as %d/%d taps,expected %d(= firTaps,8192)",
			irPath, len(banks[0]), len(banks[1]), irTaps)
	}

	vse, err := vseExciterParams(gear)
	if err != nil {
		t.Fatalf("vseExciterParams(%g)：%v", gear, err)
	}
	ex, err := exciterNodes(vse)
	if err != nil {
		t.Fatalf("exciterNodes：%v", err)
	}
	vb, err := viperBassNodes(viperBassParams{
		Mode: bassMode, CutoffHz: 60, Gain: 600,
	})
	if err != nil {
		t.Fatalf("viperBassNodes(mode=%d)：%v", bassMode, err)
	}
	xh, err := xhifiNode(50, sampleRate)
	if err != nil {
		t.Fatalf("xhifiNode：%v", err)
	}

	nodes := append([]planNode{}, ex...)
	nodes = append(nodes, planNode{Kind: planKindFIR, Blocks: firTaps / firMACS})
	for _, s := range ddc {
		nodes = append(nodes, planNode{Kind: planKindBiquad, Coefs: s})
	}
	nodes = append(nodes, vb...)
	nodes = append(nodes, xh)
	if withColorful {

		cn, err := colorfulNodes(colorfulDefaultParams())
		if err != nil {
			t.Fatalf("colorfulNodes：%v", err)
		}
		nodes = append(nodes, cn...)
	}
	return nodes
}

func TestRealChainPBPPlusColorfulMusicIsRejected(t *testing.T) {
	nodes := realChainNodesEx(t, 0.1, viperBassModePBP, true)
	saveSlots := hwDelaySlots
	hwDelaySlots = 8
	_, err := buildSlotPlanNodes(nodes)
	hwDelaySlots = saveSlots
	if err == nil {
		t.Fatal("PBP(23 slots)+ ColorfulMusic(2 slots)exceeds NSLOT=24,must be rejected")
	}
	if !strings.Contains(err.Error(), "Out of slots") {
		t.Errorf("rejection reason should make clear that slots are insufficient, actual: %v", err)
	}
	t.Logf("rejected as expected:%v", err)
}

func TestRealChainCapacity(t *testing.T) {

	for _, c := range []struct {
		gear     float64
		mode     int
		wantMin  int
		wantName string
		colorful bool
	}{
		{0.1, viperBassModeNatural, 21, "NATURAL", false},
		{1.0, viperBassModeNatural, 22, "NATURAL", false},
		{0.1, viperBassModePBP, 23, "PBP", false},
		{1.0, viperBassModePBP, 24, "PBP", false},

		{0.1, viperBassModeNatural, 23, "NATURAL+ColorfulMusic", true},
		{1.0, viperBassModeNatural, 24, "NATURAL+ColorfulMusic", true},
	} {
		nodes := realChainNodesEx(t, c.gear, c.mode, c.colorful)
		name := c.wantName
		if c.colorful {
			name += "(with ColorfulMusic)"
		}

		saveSlots := hwDelaySlots
		hwDelaySlots = 8
		plan, err := buildSlotPlanNodes(nodes)
		hwDelaySlots = saveSlots
		if err != nil {
			t.Fatalf("VSE gear=%g / ViPERBass %s:real chainshouldfit,yet rejected :%v",
				c.gear, name, err)
		}

		if c.colorful {
			if !plan.StereoFrame {
				t.Errorf("%s: StereoFrame not set in plan (header will not carry sf_en => joint-stereo section never executes)", name)
			}
			jsOps := 0
			for _, w := range plan.Words {
				if w.Addr%slotWordSize == swCfg {
					op := int(w.Value & 0xFF)
					if op == opJDST || op == opJ3DS {
						jsOps++
					}
				}
			}
			if jsOps != 2 {
				t.Errorf("%s:joint-stereo slotsshould have 2,got %d", name, jsOps)
			}
		}

		if plan.Slots > slotLimit() {
			t.Errorf("%s gear=%g:slot count %d > cap %d", name, c.gear, plan.Slots, slotLimit())
		}
		if plan.Sections > hwMaxSections {
			t.Errorf("%s gear=%g:section count %d > cap %d", name, c.gear, plan.Sections, hwMaxSections)
		}
		if len(plan.Coefs) > hwCoefWords {
			t.Errorf("%s gear=%g:coefficient %d words > per bank %d words",
				name, c.gear, len(plan.Coefs), hwCoefWords)
		}
		if plan.Slots < c.wantMin {
			t.Errorf("%s gear=%g: uses only %d slots (expected >=%d)-- chain is incomplete, so this fits verdict is bogus",
				name, c.gear, plan.Slots, c.wantMin)
		}

		if c.mode == viperBassModePBP && len(plan.SFirCoefs) != 2*sfirTaps {
			t.Errorf("PBP gear=%g: small-FIR coefficients should be 2x%d (one copy per channel), got %d",
				c.gear, sfirTaps, len(plan.SFirCoefs))
		}

		peak := 0
		for _, w := range plan.Words {
			if w.Addr%slotWordSize >= swInA && w.Addr%slotWordSize <= swOutB {
				b := int(w.Value)
				if b != 0xFFFF && b != crossLoBus && b != dynScratchBus && b > peak {
					peak = b
				}
			}
		}
		if peak >= crossLoBus {
			t.Errorf("%s gear=%g:bus peak %d ≥ reserved numbers %d", name, c.gear, peak, crossLoBus)
		}

		if err := checkFrameBudget(nodes); err != nil {
			t.Errorf("%s gear=%g:frame budget exceeded:%v", name, c.gear, err)
		}
		hasSFIR := c.mode == viperBassModePBP
		cost := chainFrameCost(plan.Sections, true, true, hasSFIR, c.colorful, plan.Slots)
		pct := 100 * float64(cost) / float64(frameBudgetCycles)
		if pct > 90 {
			t.Errorf("%s gear=%g:frame budget usage %.1f%% > 90%%(recommended cap),willlosesamples",
				name, c.gear, pct)
		}
		t.Logf("%s gear=%.1f:**%d slots / %d sections / %d coefficient**(cap %d / %d / %d),bus peak %d(reserved numbersstart %d);"+
			"frame budget %d/%d cycles = **%.1f%%**(conservative estimate:per section 12 cycles;small FIR %d cycles;joint-stereo section %d cycles)",
			name, c.gear, plan.Slots, plan.Sections, len(plan.Coefs),
			slotLimit(), hwMaxSections, hwCoefWords, peak, crossLoBus,
			cost, frameBudgetCycles, pct, map[bool]int{true: sfirCycles, false: 0}[hasSFIR],
			map[bool]int{true: colorfulJointCycles, false: 0}[c.colorful])
	}
}

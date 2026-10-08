// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"os"
	"testing"
)

func TestViPERBassBoardPlanBytes(t *testing.T) {
	path := os.Getenv("VIPERBASS_VDC")
	if path == "" {
		path = localDataPath("vdc", "mh750.vdc")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("missing local %s (private asset, not in repo): skipping on-board plan cross-check", path)
	}
	secs, rate, err := parseVDC(body, 48000)
	if err != nil {
		t.Fatalf("parse %s failed: %v", path, err)
	}
	if rate != 48000 {
		t.Fatalf("this .vdc sample rate = %d, expect 48000 (engine convention)", rate)
	}
	q, _, err := vdcToQ315(secs)
	if err != nil {
		t.Fatalf("quantize .vdc failed: %v", err)
	}
	if len(q) != 18 {
		t.Fatalf("mh750.vdc should have 18 sections, got %d", len(q))
	}

	var nodes []planNode
	nodes = append(nodes, planNode{Kind: planKindFIR, Blocks: firTaps / firMACS})
	for _, s := range q {
		nodes = append(nodes, planNode{Kind: planKindBiquad, Coefs: s})
	}
	bn, err := viperBassNodes(viperBassParams{Mode: viperBassModeNatural, CutoffHz: 60, Gain: 600})
	if err != nil {
		t.Fatal(err)
	}
	nodes = append(nodes, bn...)

	plan, err := buildSlotPlanNodes(nodes)
	if err != nil {
		t.Fatalf("compile on-board plan failed: %v", err)
	}

	if plan.Slots != 9 || plan.Sections != 21 || len(plan.Coefs) != 105 {
		t.Fatalf("on-board plan = %d slots / %d sections / %d coefficients, expect 9 / 21 / 105",
			plan.Slots, plan.Sections, len(plan.Coefs))
	}
	slots := decodeSlots(t, plan)
	var ops []int
	for _, s := range slots {
		ops = append(ops, int(s.Op))
	}
	wantOps := []int{5, 1, 1, 1, 1, 1, 1, 1, 3}
	if fmt.Sprint(ops) != fmt.Sprint(wantOps) {
		t.Errorf("slot order = %v, expect %v (FIR first, ViPERBass two slots last)", ops, wantOps)
	}

	vb := slots[len(slots)-2]
	if vb.Op != opBiquad || vb.N != 2 || vb.In != 7 || vb.Out != 8 || vb.Cfb != 90 || vb.Stb != 18 {
		t.Errorf("ViPERBass biquad slot = op%d n%d in%d out%d cfb%d stb%d, expect op1 n2 in7 out8 cfb90 stb18",
			vb.Op, vb.N, vb.In, vb.Out, vb.Cfb, vb.Stb)
	}
	mx := slots[len(slots)-1]
	if mx.Op != opMix2 || mx.In != 7 || mx.InB != 8 || mx.Out != 8 || mx.Cfb != 100 {
		t.Errorf("MIX2 slot = op%d dry%d wet%d out%d cfb%d, expect op3 dry 7 wet 8 out 8 cfb100",
			mx.Op, mx.In, mx.InB, mx.Out, mx.Cfb)
	}

	wantVB := []int32{1024, 0, 0, 0, 0, 24, 48, 24, -65052, 32286, 32769, 131071}
	for i, w := range wantVB {
		if plan.Coefs[90+i] != w {
			t.Errorf("coefficients [%d] = %d, expect %d (TB load is this batch words)", 90+i, plan.Coefs[90+i], w)
		}
	}
}

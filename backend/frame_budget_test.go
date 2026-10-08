// SPDX-License-Identifier: GPL-2.0-only
package main

import "testing"

func mkSectionsN(n int) [][5]int32 {
	out := make([][5]int32, n)
	for i := range out {
		out[i] = [5]int32{32767, 0, 0, 0, 0}
	}
	return out
}

func TestFrameBudgetRejectsOverBudgetChain(t *testing.T) {
	firSave := currentFIR
	defer func() { currentFIR = firSave }()
	currentFIR = &firParams{Taps: 8192}

	if _, err := buildChainNodes(mkSectionsN(18), nil, nil, nil); err != nil {
		t.Errorf("18 sections + convolution should fit, got: %v", err)
	}

	if _, err := buildChainNodes(mkSectionsN(40), nil, nil, nil); err != nil {
		t.Errorf("40 sections + convolution with convolution cut to %d cycles should fit (got: %v) -- "+
			"if this goes red, convolution frame cycles grew back; if intentional, recompute this file's numbers under the new convention",
			firCycles, err)
	}

	_, err := buildChainNodes(mkSectionsN(60), nil, nil, nil)
	if err == nil {
		t.Fatal("60 sections + convolution exceeds 1041 cycles per channel and must be rejected at compile time (or the board drops samples)")
	}
	t.Logf("Rejected as expected: %v", err)

	currentFIR = nil
	if _, err := buildChainNodes(mkSectionsN(60), nil, nil, nil); err != nil {
		t.Errorf("60 sections (no convolution) should fit, got: %v", err)
	}
}

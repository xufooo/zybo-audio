// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"sync"
	"testing"
)

func TestConcurrentDownloadSerialized(t *testing.T) {
	fakeHardware(t, 0xFFFFFFFF)
	saved := append([]int32(nil), lastPlanCoefs...)
	t.Cleanup(func() { lastPlanCoefs = saved })

	mkplan := func(v int32) slotPlan {
		nodes := []planNode{{Kind: planKindBiquad,
			Coefs: [5]int32{v, v + 1, v + 2, v + 3, v + 4}}}
		p, err := buildSlotPlanNodes(nodes)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	planA, planB := mkplan(11111), mkplan(22222)

	const rounds = 30
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); _ = dspDownloadSlotPlan(planA) }()
		go func() { defer wg.Done(); _ = dspDownloadSlotPlan(planB) }()
		wg.Wait()

		eqA, eqB := true, true
		if len(lastPlanCoefs) != len(planA.Coefs) {
			t.Fatalf("shadow length %d differs from plan length %d",
				len(lastPlanCoefs), len(planA.Coefs))
		}
		for j := range lastPlanCoefs {
			if lastPlanCoefs[j] != planA.Coefs[j] {
				eqA = false
			}
			if lastPlanCoefs[j] != planB.Coefs[j] {
				eqB = false
			}
		}
		if !eqA && !eqB {
			t.Fatalf("round %d: shadow mixes two plans (neither A nor B), downloads not serialized", i)
		}
	}
}

func TestCrossfeedEmitChecked(t *testing.T) {
	fakeHardware(t, 0xFFFFFFFF)
	withCapDelaySlots(t, 32)
	nodes := make([]planNode, 0, 26)
	for i := 0; i < 24; i++ {
		nodes = append(nodes, planNode{Kind: planKindDelay, Len: 64})
	}
	lo, hi, mix, err := crossfeedCoefs(crossfeedDefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	nodes = append(nodes, planNode{Kind: planKindCross, Coefs: lo, Hi: hi, Mix: mix})
	_, err = buildSlotPlanNodes(nodes)
	if err == nil {
		t.Fatalf("crossfeed past 24 full slots must report slots shortage, got nil (error swallowed?)")
	}
	if ce, ok := err.(*capacityError); !ok {
		t.Fatalf("must report capacityError, got %T: %v", err, err)
	} else if ce.What != "slots" {
		t.Fatalf("must report slots shortage, got %s: %v", ce.What, err)
	}
}

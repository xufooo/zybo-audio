// SPDX-License-Identifier: GPL-2.0-only
package main

import "testing"

func TestCapacityBoundaries(t *testing.T) {
	fakeHardware(t, 0xFFFFFFFF)
	withCapDelaySlots(t, 32)

	biquad := func() planNode {
		return planNode{Kind: planKindBiquad, Coefs: [5]int32{32768, 0, 0, 0, 0}}
	}
	dyn1 := func() planNode {
		c, err := dynParamsToCoefs(dynDefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		return planNode{Kind: planKindDyn, Coefs: c, Side: dynSideChainCoefs()}
	}
	mustPlan := func(nodes []planNode) (slotPlan, error) {
		t.Helper()
		return buildSlotPlanNodes(nodes)
	}
	asCapacity := func(t *testing.T, err error, what string) *capacityError {
		t.Helper()
		if err == nil {
			t.Fatalf("must report %s shortage, got nil", what)
		}
		ce, ok := err.(*capacityError)
		if !ok {
			t.Fatalf("must report capacityError(%s), got %T: %v", what, err, err)
		}
		if ce.What != what {
			t.Fatalf("What must be %s, got %s: %v", what, ce.What, err)
		}
		return ce
	}

	n40 := make([]planNode, 0, 40)
	for i := 0; i < 40; i++ {
		n40 = append(n40, biquad())
	}
	p, err := mustPlan(n40)
	if err != nil {
		t.Fatalf("40 sections (full) must pass: %v", err)
	}
	if p.Sections != 40 {
		t.Fatalf("40 sections must compile to 40, got %d", p.Sections)
	}
	n41 := append(append([]planNode(nil), n40...), biquad())
	ce := asCapacity(t, func() error { _, err := mustPlan(n41); return err }(), "sections")
	if ce.Needed != 41 || ce.Limit != hwMaxSections {
		t.Errorf("sections fields must be 41/%d, got %d/%d", hwMaxSections, ce.Needed, ce.Limit)
	}

	dyns := make([]planNode, 0, 13)
	for i := 0; i < 12; i++ {
		dyns = append(dyns, dyn1())
	}
	p, err = mustPlan(dyns)
	if err != nil {
		t.Fatalf("24 slots (full) must pass: %v", err)
	}
	if p.Slots != 24 {
		t.Fatalf("12 dyns must compile to 24 slots, got %d", p.Slots)
	}
	ce = asCapacity(t, func() error {
		_, err := mustPlan(append(append([]planNode(nil), dyns...), biquad()))
		return err
	}(), "slots")
	if ce.Needed != 25 || ce.Limit != slotLimit() {
		t.Errorf("slots fields must be 25/%d, got %d/%d", slotLimit(), ce.Needed, ce.Limit)
	}

}

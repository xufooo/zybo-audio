// SPDX-License-Identifier: GPL-2.0-only
package main

import "testing"

func TestCardApplyExactManualPreserves(t *testing.T) {
	fakeHardware(t, 0xFFFFFFFF)

	savedChain := currentUserChain
	savedSlots := currentSlots
	savedPreamp := currentPreampAppliedDB
	savedGain := chainGainDB
	savedType := selectedTypeName

	savedFIR := currentFIR
	savedDDCSections := append([][5]int32(nil), ddcSections...)
	t.Cleanup(func() {
		currentUserChain = savedChain
		currentSlots = savedSlots
		currentPreampAppliedDB = savedPreamp
		chainGainDB = savedGain
		selectedTypeName = savedType
		currentFIR = savedFIR
		ddcMu.Lock()
		ddcSections = savedDDCSections
		ddcMu.Unlock()
	})
	seed := []ChainItem{
		{Type: "peq", Enabled: true, Freq: 1000, GainDB: 0, Q: 1},
		{Type: "fir", Enabled: true},
		{Type: "ddc", Enabled: true},
	}
	hasResource := func() bool {
		for _, it := range currentUserChain {
			if kind, ok := chainEffectType[normalizeChainType(it.Type)]; ok && resourceChainKinds[kind] {
				return true
			}
		}
		return false
	}

	currentUserChain = append([]ChainItem(nil), seed...)
	rec := chainPut(t, `{"chain":[],"type":"Dynamic Punch-test"}`)
	if rec.Code != 200 {
		t.Fatalf("card-switch PUT must return 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if hasResource() {
		t.Errorf("chain still has fir/ddc after card switch (must equal card content exactly)")
	}

	currentUserChain = append([]ChainItem(nil), seed...)
	rec = chainPut(t, `{"chain":[],"type":"\u81ea\u5b9a\u4e49"}`)
	if rec.Code != 200 {
		t.Fatalf("manual-edit PUT must return 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !hasResource() {
		t.Errorf("fir/ddc lost after manual chain edit (must be preserved)")
	}

	currentUserChain = append([]ChainItem(nil), seed...)
	rec = chainPut(t, `{"chain":[]}`)
	if rec.Code != 200 {
		t.Fatalf("typeless PUT must return 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !hasResource() {
		t.Errorf("fir/ddc lost without type (must preserve like manual edits)")
	}
}

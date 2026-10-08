// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"reflect"
	"testing"
)

func TestChainPutKeepsConvolverAndDDC(t *testing.T) {
	existing := []ChainItem{
		{Type: "peq", Enabled: true, Freq: 1000, GainDB: 3, Q: 1},
		{Type: "convolver", Enabled: true, Name: "thepbone-clear_bass.irs"},
		{Type: "ddc", Enabled: true, Name: "mh750.vdc",
			Params: map[string]float64{"sections": 18}},
	}
	incoming := []ChainItem{
		{Type: "peq", Enabled: true, Freq: 200, GainDB: 1, Q: 1},
		{Type: "peq", Enabled: true, Freq: 400, GainDB: -1, Q: 1},
	}

	got := preserveResourceChainItems(incoming, existing)

	if len(got) != 4 {
		t.Fatalf("result should have 4 items(2 EQ + convolution + DDC),got %d:%+v", len(got), got)
	}

	if !reflect.DeepEqual(got[0], incoming[0]) || !reflect.DeepEqual(got[1], incoming[1]) {
		t.Errorf("EQ sections should follow the request, got %+v", got[:2])
	}

	var haveFIR, haveDDC *ChainItem
	for i := range got {
		switch normalizeChainType(got[i].Type) {
		case "convolver":
			haveFIR = &got[i]
		case "ddc":
			haveDDC = &got[i]
		}
	}
	if haveFIR == nil {
		t.Fatal("convolver chain item lost -- this is exactly the bug this test exists to catch (one panel edit would lose it)")
	}
	if haveFIR.Name != "thepbone-clear_bass.irs" {
		t.Errorf("convolution IR name not kept:%q", haveFIR.Name)
	}
	if haveDDC == nil {
		t.Fatal("DDC chain item lost -- same as above")
	}
	if haveDDC.Name != "mh750.vdc" || haveDDC.Params["sections"] != 18 {
		t.Errorf("DDC name/section count not kept:%+v", *haveDDC)
	}
}

func TestChainPutExplicitResourceItemWins(t *testing.T) {
	existing := []ChainItem{
		{Type: "convolver", Enabled: true, Name: "old.irs"},
		{Type: "ddc", Enabled: true, Name: "old.vdc"},
	}

	incoming := []ChainItem{
		{Type: "ir", Enabled: true, Name: "new.irs"},
		{Type: "peq", Enabled: true, Freq: 1000, GainDB: 1, Q: 1},
	}

	got := preserveResourceChainItems(incoming, existing)

	fir, ddc := 0, 0
	for _, it := range got {
		switch normalizeChainType(it.Type) {
		case "ir":
			fir++
			if it.Name != "new.irs" {
				t.Errorf("should use in request IR(new.irs),got %q", it.Name)
			}
		case "ddc":
			ddc++
			if it.Name != "old.vdc" {
				t.Errorf("request lacks DDC, should keep old.vdc, got %q", it.Name)
			}
		}
	}
	if fir != 1 {
		t.Errorf("convolver chain items should exactly 1(request wins),got %d", fir)
	}
	if ddc != 1 {
		t.Errorf("DDC should exactly 1(carried over from the old chain),got %d", ddc)
	}
}

func TestChainPutPreserveNoopOnEmptyExisting(t *testing.T) {
	in := []ChainItem{{Type: "peq", Enabled: true, Freq: 1000, GainDB: 1, Q: 1}}
	got := preserveResourceChainItems(in, nil)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("old chain is empty should return as-is,got %+v", got)
	}
}

func TestResourceChainKindsCoverExactlyFieldlessEffects(t *testing.T) {
	want := map[string]bool{"fir": true, "ddc": true}
	if !reflect.DeepEqual(resourceChainKinds, want) {
		t.Fatalf("resourceChainKinds = %v,expected %v -- "+
			"adding a new kind of chain-item effect,must explicitly decide it whether it has request fields:"+
			"no field (only via standalone endpoint switch)must added in,otherwise again will repeat"+
			"editing EQ silently deletes it", resourceChainKinds, want)
	}

	kinds := map[string]bool{}
	for _, k := range chainEffectType {
		kinds[k] = true
	}
	for k := range resourceChainKinds {
		if !kinds[k] {
			t.Errorf("resourceChainKinds %q is not chainEffectType valid value,never matches", k)
		}
	}

	for _, k := range []string{"crossfeed", "surround", "tube"} {
		if resourceChainKinds[k] {
			t.Errorf("%q has request fields(covered by field semantics),must not enter resourceChainKinds", k)
		}
	}
}

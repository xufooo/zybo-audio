// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"strings"
	"testing"
)

func TestChainSlotTypeMapping(t *testing.T) {
	cases := map[string]string{
		"peq": "PK", "PK": "PK", "pk": "PK", "parametric": "PK", "Modal": "PK",
		"lowshelf": "LS", "ls": "LS", "LSC": "LS",
		"highshelf": "HS", "hs": "HS", "HSC": "HS",
		"highpass": "HP", "HP": "HP", "hpq": "HPQ",
		"lowpass": "LP", "LP": "LP", "lpq": "LPQ",
		"notch": "NO", "no": "NO",
		"bandpass": "BP", "allpass": "AP", "ap": "AP",
	}
	for in, want := range cases {
		got, ok := chainSlotType(in)
		if !ok || got != want {
			t.Errorf("chainSlotType(%q) = %q,%v;expected %q,true", in, got, ok, want)
		}
	}
	if _, ok := chainSlotType("crossfeed"); ok {
		t.Error("crossfeed should not map to a hardware slot(P2 has)")
	}
}

func TestChainSixBandsFits(t *testing.T) {
	chain := []ChainItem{
		{Type: "peq", Enabled: true, Freq: 60, GainDB: 5, Q: 0.9},
		{Type: "peq", Enabled: true, Freq: 150, GainDB: 3, Q: 1.0},
		{Type: "peq", Enabled: true, Freq: 400, GainDB: -1.5, Q: 1.0},
		{Type: "peq", Enabled: true, Freq: 1000, GainDB: 1, Q: 1.0},
		{Type: "peq", Enabled: true, Freq: 3000, GainDB: 3, Q: 1.0},
		{Type: "peq", Enabled: true, Freq: 10000, GainDB: 2, Q: 0.9},
	}
	slots, n, err := chainToSlots(chain)
	if err != nil {
		t.Fatalf("6 sections should fit,but fail:%v", err)
	}
	if n != 6 {
		t.Errorf("live sections = %d,expected 6", n)
	}
	for i := 0; i < 6; i++ {
		if slots[i].Type != "PK" {
			t.Errorf("slot %d = %q,expected PK", i, slots[i].Type)
		}
	}
}

func TestChainTenBandsErrorsNotTruncates(t *testing.T) {

	chain := []ChainItem{
		{Type: "lowshelf", Enabled: true, Freq: 105, GainDB: 6.4, Q: 0.70},
		{Type: "peq", Enabled: true, Freq: 1928, GainDB: 3.5, Q: 1.28},
		{Type: "peq", Enabled: true, Freq: 176, GainDB: -2.0, Q: 0.58},
		{Type: "peq", Enabled: true, Freq: 5764, GainDB: -7.8, Q: 3.59},
		{Type: "peq", Enabled: true, Freq: 22, GainDB: -0.4, Q: 4.35},
		{Type: "highshelf", Enabled: true, Freq: 10000, GainDB: -4.2, Q: 0.70},
		{Type: "peq", Enabled: true, Freq: 3534, GainDB: 1.4, Q: 5.71},
		{Type: "peq", Enabled: true, Freq: 3643, GainDB: -0.3, Q: 4.55},
		{Type: "peq", Enabled: true, Freq: 6708, GainDB: 1.2, Q: 6.00},
		{Type: "peq", Enabled: true, Freq: 1026, GainDB: -0.2, Q: 2.62},
	}
	_, _, err := chainToSlots(chain)
	if err == nil {
		t.Fatal("10 sections on 6-section hardware must fail, not truncate silently")
	}
	msg := err.Error()
	for _, want := range []string{"10 sections", "6 sections", "NOT applied"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message should mention %q,actual:%s", want, msg)
		}
	}

	if !strings.Contains(msg, "3534") && !strings.Contains(msg, "1026") {
		t.Errorf("error message should name not should use sections,actual:%s", msg)
	}
}

func TestChainPlannedTypeExplains(t *testing.T) {
	cases := map[string]string{
		"compressor": "P2",
		"delay":      "P2",
		"reverb":     "P3",

		"analog": "P3",
	}
	for typ, stage := range cases {
		err := chainItemCheck(ChainItem{Type: typ, Enabled: true})
		if err == nil {
			t.Errorf("%s should not count as available yet", typ)
			continue
		}
		if !strings.Contains(err.Error(), stage) {
			t.Errorf("%s fail should mention stage %s,actual:%v", typ, stage, err)
		}
	}

	if err := chainItemCheck(ChainItem{Type: "no-such-type"}); err == nil ||
		!strings.Contains(err.Error(), "Unknown") {
		t.Errorf("unknown type should report an unknown-type error, actual: %v", err)
	}
}

func TestChainIgnoresDisabledAndSanitizes(t *testing.T) {
	chain := []ChainItem{
		{Type: "peq", Enabled: false, Freq: 1000, GainDB: 6, Q: 1},
		{Type: "peq", Enabled: true, Freq: 15, GainDB: 3, Q: 1},
	}
	slots, n, err := chainToSlots(chain)
	if err != nil {
		t.Fatalf("should not fail:%v", err)
	}
	if n != 1 {
		t.Errorf("live sections = %d,expected 1(disabled items take no slots)", n)
	}
	if slots[0].Freq != minBandFreq {
		t.Errorf("15 Hz should be clamped to %.0f Hz,actual %.0f", minBandFreq, slots[0].Freq)
	}
	if slots[1].Type != "off" {
		t.Errorf("remaining slots should be off,actual %q", slots[1].Type)
	}
}

func TestChainRequiresAndUnsupported(t *testing.T) {
	chain := []ChainItem{
		{Type: "peq", Enabled: true, Freq: 1000, GainDB: 1, Q: 1},
		{Type: "PEQ", Enabled: true, Freq: 3000, GainDB: 2, Q: 1},
		{Type: "crossfeed", Enabled: true},
	}
	req := chainRequires(chain)
	want := []string{"crossfeed", "peq"}
	if strings.Join(req, ",") != strings.Join(want, ",") {
		t.Errorf("chainRequires = %v,expected %v", req, want)
	}
	unsup := chainUnsupported(chain)
	if _, ok := unsup["crossfeed"]; !ok {
		t.Error("crossfeed should listed in unsupported ")
	}
	if _, ok := unsup["peq"]; ok {
		t.Error("peq must not be listed in unsupported ")
	}
}

func TestChainFromSlotsRoundTrip(t *testing.T) {
	orig := []ChainItem{
		{Type: "lowshelf", Enabled: true, Freq: 105, GainDB: 4, Q: 0.7},
		{Type: "peq", Enabled: true, Freq: 1000, GainDB: -2, Q: 1.2},
		{Type: "highshelf", Enabled: true, Freq: 8000, GainDB: 2, Q: 0.7},
	}
	slots, _, err := chainToSlots(orig)
	if err != nil {
		t.Fatalf("should not fail:%v", err)
	}
	saved := currentSlots
	currentSlots = slots
	defer func() { currentSlots = saved }()

	back := chainFromSlots()
	if len(back) != 3 {
		t.Fatalf("read back %d sections,expected 3", len(back))
	}
	slots2, _, err := chainToSlots(back)
	if err != nil {
		t.Fatalf("read back chain should persist again:%v", err)
	}
	for i := range slots {
		if slots[i] != slots2[i] {
			t.Errorf("slot %dround-trip mismatch:%+v vs %+v", i, slots[i], slots2[i])
		}
	}
}

func TestChainHeadroomOverWholeChain(t *testing.T) {

	chain := []ChainItem{
		{Type: "peq", Enabled: true, Freq: 60, GainDB: 5, Q: 0.9},
		{Type: "peq", Enabled: true, Freq: 150, GainDB: 3, Q: 1.0},
		{Type: "peq", Enabled: true, Freq: 10000, GainDB: 3, Q: 0.9},
	}
	slots, _, err := chainToSlots(chain)
	if err != nil {
		t.Fatal(err)
	}
	var coefs [maxBands][coefPerBand]int32
	for i := 0; i < maxBands; i++ {
		coefs[i] = [coefPerBand]int32{qOne, 0, 0, 0, 0}
		if isBandOff(slots[i].Type) {
			continue
		}
		b0, b1, b2, a1, a2, err := designBiquad(slots[i])
		if err != nil {
			t.Fatalf("section %ddesign failed:%v", i, err)
		}
		coefs[i] = [coefPerBand]int32{b0, b1, b2, a1, a2}
	}
	maxGain := cascadeMaxGainDB(coefs)
	if maxGain <= 0 {
		t.Fatalf("this chain has boost,cascaded max gain should > 0,actual %.2f dB", maxGain)
	}
	pre := effectivePreampDB(maxGain, 0)
	if pre >= 0 {
		t.Errorf("with boost preamp must < 0(automatic headroom),actual %.2f dB", pre)
	}
	if pre > -(maxGain+preampSafetyMarginDB)+0.01 {
		t.Errorf("preamp = %.2f no cover enough headroom(max boost %.2f + safety margin %.1f)",
			pre, maxGain, preampSafetyMarginDB)
	}

	var flat [maxBands][coefPerBand]int32
	for i := range flat {
		flat[i] = [coefPerBand]int32{qOne, 0, 0, 0, 0}
	}
	if pre := effectivePreampDB(cascadeMaxGainDB(flat), 0); pre != 0 {
		t.Errorf("perfectly flat should not eat headroom,actual %.2f dB", pre)
	}
}

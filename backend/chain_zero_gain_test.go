// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"math"
	"testing"
)

var flatBand = ChainItem{
	Type: "peq", Enabled: true,
	Freq: 219, GainDB: 0, Q: 1.4,
}

func TestZZChainItemKeepsZeroGainInJSON(t *testing.T) {
	raw, err := json.Marshal([]ChainItem{flatBand})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var back []map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(back) != 1 {
		t.Fatalf("want 1 section, got %d", len(back))
	}
	got, ok := back[0]["gain_db"]
	if !ok {
		t.Fatalf("gain_db key vanished entirely (the omitempty-eats-0 bug): %s", raw)
	}
	v, isNum := got.(float64)
	if !isNum {
		t.Fatalf("gain_db should be a number, got %T (%v) => frontend would get undefined", got, got)
	}
	if v != 0 {
		t.Errorf("gain_db should be 0, got %v", v)
	}

	for _, k := range []string{"freq", "q"} {
		if _, ok := back[0][k]; !ok {
			t.Errorf("%s key vanished: %s", k, raw)
		}
	}
}

func TestZZZeroGainBandIsRecognisableAsCard(t *testing.T) {
	raw, err := json.Marshal([]ChainItem{flatBand})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got []ChainItem
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 section, got %d", len(got))
	}

	diff := math.Abs(got[0].GainDB - flatBand.GainDB)
	if math.IsNaN(diff) || diff > 1e-6 {
		t.Errorf("read-back value mismatches what was written to the card (panel would classify the card as 'Custom'): diff %v", diff)
	}
}

func TestZZPresetKeepsZeroPreampInJSON(t *testing.T) {
	raw, err := json.Marshal(struct {
		PreampDB float64 `json:"preamp_db"`
	}{PreampDB: 0})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if _, ok := back["preamp_db"]; !ok {
		t.Fatalf("preamp_db=0 was eaten: %s (0 dB means 'no pre-cut', a meaningful value)", raw)
	}
}

// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
)

func TestLevelClassDBMatchesAudit(t *testing.T) {

	cases := []struct {
		v    float64
		want float64
		tol  float64
	}{
		{100, 0.0, 0.05}, {90, -0.92, 0.06}, {80, -1.94, 0.06}, {70, -3.10, 0.06},
		{50, -6.02, 0.06}, {30, -10.46, 0.06}, {10, -20.0, 0.06}, {5, -26.02, 0.06},
		{1, -40.0, 0.06},
	}
	for _, c := range cases {
		if got := levelClassDB(c.v); math.Abs(got-c.want) > c.tol {
			t.Errorf("level class: %g -> %.3f dB, want %.2f (+/-%.2f)", c.v, got, c.want, c.tol)
		}
	}
	if !math.IsInf(levelClassDB(0), -1) {
		t.Error("level class v=0 should be -Inf (callers must range-check first)")
	}
}

func TestGainClassDBMatchesAudit(t *testing.T) {

	cases := []struct {
		v    float64
		want float64
	}{
		{0, 0.0}, {50, 3.52}, {100, 6.02}, {150, 7.96},
	}
	for _, c := range cases {
		if got := gainClassDB(c.v); math.Abs(got-c.want) > 0.03 {
			t.Errorf("gain class: %g -> %.3f dB, want %.2f", c.v, got, c.want)
		}
	}

	if d := gainClassDB(50) - levelClassDB(50); math.Abs(d-9.54) > 0.05 {
		t.Errorf("gain/level class difference at 50 should be 9.54 dB, got %.2f -- formula or class changed?", d)
	}

	for _, v := range []float64{10, 50, 100} {
		if d := 20*math.Log10(gainClassLinear(v)) - gainClassDB(v); math.Abs(d) > 1e-9 {
			t.Errorf("gain class linear/dB spellings disagree at v=%g: %g", v, d)
		}
		if d := 20*math.Log10(levelClassLinear(v)) - levelClassDB(v); math.Abs(d) > 1e-9 {
			t.Errorf("level class linear/dB spellings disagree at v=%g: %g", v, d)
		}
	}
}

func TestCrossfeedTiersMatchAudit(t *testing.T) {
	want := []struct {
		name string
		cn   string
		fcut float64
		feed float64
	}{
		{"Slight", "Slight", 650, 95},
		{"Moderate", "Moderate", 700, 60},
		{"Extreme", "Extreme", 700, 45},
	}
	if len(crossfeedTiers) != len(want) {
		t.Fatalf("Tier count should be %d, got %d", len(want), len(crossfeedTiers))
	}
	for i, w := range want {
		got := crossfeedTiers[i]
		if got.Name != w.name || got.Cn != w.cn || got.FcutHz != w.fcut || got.Feed != w.feed {
			t.Errorf("Tier %d should be %s/%s(%g/%g), got %s/%s(%g/%g)",
				i, w.name, w.cn, w.fcut, w.feed, got.Name, got.Cn, got.FcutHz, got.Feed)
		}
	}

	if crossfeedTierIndexModerate != 1 {
		t.Errorf("Default tier index should be 1 (Moderate), got %d", crossfeedTierIndexModerate)
	}
	d := crossfeedDefaultParams()
	if i := crossfeedTierOf(d); i != crossfeedTierIndexModerate {
		t.Errorf("Default crossfeed should land on tier %d Moderate(700/60), got tier %d (%g/%g) -- "+
			"note V4A's own Cure default is tier 0 Slight(650/95)"+
			"(headset_preferences_l2.xml:374 android:defaultValue=\"0\"),"+
			"this machine follows the JamesDSP installed config tier",
			crossfeedTierIndexModerate, i, d.FcutHz, d.Feed)
	}

	if d.FcutHz != crossfeedTiers[crossfeedTierIndexModerate].FcutHz ||
		d.Feed != crossfeedTiers[crossfeedTierIndexModerate].Feed {
		t.Errorf("crossfeedDefaultParams diverged from the tier table: %+v vs %+v",
			d, crossfeedTiers[crossfeedTierIndexModerate])
	}

	tv := crossfeedTiersView()
	tiers, ok := tv["tiers"].([]map[string]any)
	if !ok || len(tiers) != len(want) {
		t.Fatalf("crossfeedTiersView three-tier table has wrong shape: %v", tv["tiers"])
	}
	defCount := 0
	for i, row := range tiers {
		if row["name_en"] != want[i].name || row["name_cn"] != want[i].cn ||
			row["fcut_hz"] != want[i].fcut || row["feed"] != want[i].feed {
			t.Errorf("Interface tier %d disagrees with the source table: %v", i, row)
		}
		if row["default"] == true {
			defCount++
			if i != crossfeedTierIndexModerate {
				t.Errorf("Interface marked tier %d as default, want %d", i, crossfeedTierIndexModerate)
			}
		}
	}
	if defCount != 1 {
		t.Errorf("Interface should mark exactly one tier as default, got %d", defCount)
	}
}

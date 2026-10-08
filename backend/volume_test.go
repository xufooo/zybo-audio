// SPDX-License-Identifier: GPL-2.0-only
package main

import "testing"

func TestPctToDB(t *testing.T) {
	cases := []struct {
		pct  int
		want float64
	}{
		{0, -60}, {1, -59.4}, {50, -30}, {80, -12}, {96, -2.4}, {100, 0},
	}
	for _, c := range cases {
		if got := pctToDB(c.pct); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("pctToDB(%d) = %.2f, want %.2f", c.pct, got, c.want)
		}
	}
}

func TestDBToPctRoundTrip(t *testing.T) {
	for _, pct := range []int{1, 10, 25, 37, 50, 80, 96, 100} {
		if got := dbToPct(pctToDB(pct)); got != pct {
			t.Errorf("dbToPct(pctToDB(%d)) = %d", pct, got)
		}
	}
}

func TestMixerGridRoundTrip(t *testing.T) {

	rawCases := map[int]int{50: 92, 62: 99, 100: 122, 1: 63}
	for pct, wantRaw := range rawCases {
		if got := mixerRawForPct(pct); got != wantRaw {
			t.Errorf("mixerRawForPct(%d) = raw %d, want raw %d", pct, got, wantRaw)
		}
	}

	for pct := 1; pct <= 100; pct++ {
		raw := mixerRawForPct(pct)
		if raw < 1 || raw > 127 {
			t.Fatalf("pct %d: raw %d out of range", pct, raw)
		}
		if db := raw - mixerZeroDBRaw; db > 0 {
			t.Errorf("pct %d: raw %d gives %+ddB, must never exceed 0dB (clipping)", pct, raw, db)
		}
		readBack := dbToPct(float64(raw - mixerZeroDBRaw))
		if readBack != quantizePct(pct) {
			t.Errorf("pct %d: read back %d, UI would show %d", pct, readBack, quantizePct(pct))
		}
		if d := readBack - pct; d > 1 || d < -1 {
			t.Errorf("pct %d: read back %d, off by more than one grid step", pct, readBack)
		}

		if got := quantizePct(readBack); got != readBack {
			t.Errorf("quantizePct not idempotent at pct %d: %d -> %d", pct, readBack, got)
		}
	}
}

func TestParseMixerDB(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want float64
		ok   bool
	}{
		{
			name: "80% = -20dB (real amixer output)",
			in: "Simple mixer control 'Master',0\n" +
				"  Capabilities: pvolume\n" +
				"  Playback channels: Front Left - Front Right\n" +
				"  Limits: Playback 0 - 127\n" +
				"  Mono:\n" +
				"  Front Left: Playback 102 [80%] [-20.00dB]\n" +
				"  Front Right: Playback 102 [80%] [-20.00dB]\n",
			want: -20, ok: true,
		},
		{

			name: "dead zone (unmapped) counts as silence",
			in:   "  Front Left: Playback 47 [37%] [-99999.99dB]\n",
			want: volumeMinDB, ok: true,
		},
		{
			name: "top of the range is +5 dB but we never ask for it",
			in:   "  Front Left: Playback 127 [100%] [5.00dB]\n",
			want: 5, ok: true,
		},
		{
			name: "garbage",
			in:   "amixer: Mixer default load error: No such device\n",
			want: 0, ok: false,
		},
	}
	for _, c := range cases {
		got, ok := parseMixerDB(c.in)
		if ok != c.ok {
			t.Errorf("%s: ok = %v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && (got < c.want-0.01 || got > c.want+0.01) {
			t.Errorf("%s: parseMixerDB() = %.2f, want %.2f", c.name, got, c.want)
		}
	}
}

func TestDeadZoneReadsAsZero(t *testing.T) {

	db, _ := parseMixerDB("  Front Left: Playback 47 [37%] [-99999.99dB]\n")
	if pct := dbToPct(db); pct != 0 {
		t.Errorf("dead zone should map to 0%%, got %d%%", pct)
	}
}

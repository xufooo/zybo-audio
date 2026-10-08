// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"os"
	"testing"
)

func resetIRState() {
	irMu.Lock()
	irBank[0], irBank[1] = nil, nil
	irMeta, irName, irDirty, irLoading = nil, "", false, false
	irMu.Unlock()
}

func TestLoadIRIntoStoresBothChannels(t *testing.T) {
	resetIRState()
	defer resetIRState()

	w := mkPCM16WAV(44100, 1, 8192, func(i, c int) float64 {
		if i == 4095 {
			return 0.5
		}
		return 0
	})
	info, err := loadIRInto(w, "probe")
	if err != nil {
		t.Fatalf("load failed:%v", err)
	}
	if info.SourceRate != 44100 {
		t.Errorf("source sample rate should recorded as 44100,actual %d", info.SourceRate)
	}
	if len(irBank[0]) != irTaps || len(irBank[1]) != irTaps {
		t.Fatalf("both channels should has %d coefficients,actual %d / %d", irTaps, len(irBank[0]), len(irBank[1]))
	}
	if !irDirty {
		t.Error("after loading should flag dirty(next dispatch needs coefficient carry down)")
	}
	coefs := irPlanCoefs()
	if len(coefs) != 2*irTaps {
		t.Fatalf("dispatch should carry 2x%d coefficients(channel 0 first),actual %d", irTaps, len(coefs))
	}
	if coefs[0] != irBank[0][0] || coefs[irTaps] != irBank[1][0] {
		t.Error("flatten order should be all of channel 0, then all of channel 1")
	}

	irClearDirty()
	if got := irPlanCoefs(); got != nil {
		t.Errorf("after clearing dirty should not carry coefficients again, actual %d", len(got))
	}
}

func TestLoadIRIntoRejectsGarbage(t *testing.T) {
	resetIRState()
	defer resetIRState()
	if _, err := loadIRInto([]byte("not a WAV"), "x"); err == nil {
		t.Error("garbage data should fail")
	}
	if _, err := loadIRInto(nil, "x"); err == nil {
		t.Error("empty data should fail")
	}

	w := mkPCM16WAV(48000, 1, 64, func(i, c int) float64 { return 0 })
	if _, err := loadIRInto(w, "x"); err == nil {
		t.Error("all-zero IR should fail")
	}
}

func TestIRNameSafe(t *testing.T) {
	cases := map[string]string{
		"clear_bass": "clear_bass.irs",
		"my ir.wav":  "my ir.wav",

		"../../etc/passwd": "_.._etc_passwd.irs",
		"/abs/path/ir.irs": "_abs_path_ir.irs",
		"":                 "imported.irs",
	}
	for in, want := range cases {
		if got := irNameSafe(in); got != want {
			t.Errorf("irNameSafe(%q) = %q,expected %q", in, got, want)
		}
	}

	for _, in := range []string{"a/b", "a\\b", "../x", "C:\\x"} {
		got := irNameSafe(in)
		for _, bad := range []string{"/", "\\", ":"} {
			for i := 0; i+len(bad) <= len(got); i++ {
				if got[i:i+len(bad)] == bad {
					t.Errorf("irNameSafe(%q) = %q still contains %q", in, got, bad)
				}
			}
		}
	}
}

func TestIRViewReportsState(t *testing.T) {
	resetIRState()
	defer resetIRState()
	v := irView()
	if v["max_taps"] != firTaps {
		t.Errorf("max_taps should report %d,actual %v", firTaps, v["max_taps"])
	}
	if _, ok := v["info"]; ok {
		t.Error("not loaded yet IR should not has info")
	}

	var peak float64
	for i := 0; i < 4096; i++ {
		peak = math.Max(peak, 0)
	}
	w := mkPCM16WAV(48000, 1, 4096, func(i, c int) float64 {
		if i == 2048 {
			return 0.25
		}
		return 0
	})
	if _, err := loadIRInto(w, "view-probe"); err != nil {
		t.Fatalf("load failed:%v", err)
	}
	v = irView()
	if v["name"] != "view-probe.irs" {
		t.Errorf("name should be normalized to view-probe.irs,actual %v", v["name"])
	}
	if v["loaded_taps"] != irTaps {
		t.Errorf("loaded_taps should report %d,actual %v", irTaps, v["loaded_taps"])
	}
	if v["dirty"] != true {
		t.Error("just loaded should report dirty=true")
	}
	_ = peak
}

func TestLoadRealJamesDSPIR(t *testing.T) {
	p := localDataPath("irs", "thepbone-clear_bass-audio.irs")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("this machine lacks that IR(%v)-- skipping", err)
	}
	resetIRState()
	defer resetIRState()
	info, err := loadIRInto(b, "thepbone-clear_bass")
	if err != nil {
		t.Fatalf("failed to load real IR: %v", err)
	}
	t.Logf("source: %d Hz / %d channels / peak at %d(%.1f ms)/ peak %.3f / scaling %.3f / zero padding %d",
		info.SourceRate, info.Channels, info.PeakIndex, float64(info.PeakIndex)/48000*1000,
		info.Peak, info.Gain, info.ZeroPad)
	if info.SourceRate != 44100 {
		t.Errorf("source sample rate should be 44100,actual %d", info.SourceRate)
	}
	if len(irBank[0]) != irTaps {
		t.Fatalf("should has %d taps,actual %d", irTaps, len(irBank[0]))
	}

	mx := int32(0)
	for _, c := range irBank[0] {
		if c > mx {
			mx = c
		}
	}
	if float64(mx) < 0.9*0.739*32768 {
		t.Errorf("peak coefficient %d too small(expected ≈ %.0f)", mx, 0.739*32768)
	}

	nonzero := 0
	for _, c := range irBank[0] {
		if c != 0 {
			nonzero++
		}
	}
	if nonzero < 64 {
		t.Errorf("valid taps only %d,does not look like a genuine IR", nonzero)
	}
}

func TestNamedIRAndDDCLoadFromDisk(t *testing.T) {
	resetIRState()
	resetDDC()
	oldIR, oldDDC := irDir, ddcDir
	irDir, ddcDir = t.TempDir(), t.TempDir()
	defer func() { irDir, ddcDir = oldIR, oldDDC; resetIRState(); resetDDC() }()

	w := mkPCM16WAV(48000, 1, 1024, func(i, c int) float64 {
		if i == 512 {
			return 0.5
		}
		return 0
	})
	if err := saveIRFile("probe-name", w); err != nil {
		t.Fatalf("persist failed:%v", err)
	}
	resetIRState()
	if err := loadIRNamed("probe-name"); err != nil {
		t.Fatalf("failed to read back IR by name: %v", err)
	}
	if got := irLoadedName(); got != "probe-name.irs" {
		t.Errorf("after read-back name should be probe-name.irs,actual %q", got)
	}
	if len(irBank[0]) != irTaps || !irDirty {
		t.Error("after read-back should carry coefficients and flag dirty(next dispatch coefficient carry down)")
	}
	if err := loadIRNamed("no-such-file"); err == nil {
		t.Error("missing file should fail (must not silently run on zero coefficients)")
	}

	vdc := []byte("SR_48000:1.0,2.0,1.0,0.49776872571908,-0.92323067076422\n")
	if err := saveDDCFile("probe.vdc", vdc); err != nil {
		t.Fatalf("DDC persist failed:%v", err)
	}
	resetDDC()
	if err := loadDDCNamed("probe.vdc", 4, false); err != nil {
		t.Fatalf("failed to read back DDC by name: %v", err)
	}
	if v := ddcView(); v["on"] != true || v["sections"] != 4 {
		t.Errorf("after read-back should be 4 sections and on:%+v", v)
	}
	if err := loadDDCNamed("no-such-file.vdc", 4, false); err == nil {
		t.Error("not found DDC should fail")
	}
}

func TestFIRGainIsVisibleToHeadroomDecision(t *testing.T) {
	resetIRState()
	defer resetIRState()
	if g := firGainDBForHeadroom(); g != 0 {
		t.Fatalf("no convolver stage should be 0,actual %.2f dB", g)
	}
	b, err := os.ReadFile(localDataPath("irs", "thepbone-clear_bass-audio.irs"))
	if err != nil {
		t.Skip("this machine lacks that IR")
	}
	if _, err := loadIRInto(b, "gain-probe"); err != nil {
		t.Fatal(err)
	}
	currentFIR = &firParams{Taps: firTaps, Name: "gain-probe"}
	defer func() { currentFIR = nil }()
	g := firGainDBForHeadroom()
	if g < 6 {
		t.Errorf("clear_bass worst case gain should be clearly above 0(measured to be ≈ +11.5 dB),actual %.2f dB -- "+
			"this would miss headroom at high volume: convolver clipping (hiss)", g)
	}
	if g > 18 {
		t.Errorf("at %.2f dB exceeds in-chain 18 dB headroom,that headroom cannot cover(needs changing preamp strategy)", g)
	}
	t.Logf("clear_bass worst case gain = %+.2f dB(in-chain headroom 18 dB can cover)", g)
}

func TestLoadRealStereoIRFromLibrary(t *testing.T) {

	cases := []struct {
		path     string
		wantSter bool
		minDiff  float64
		maxDiff  float64
		label    string
	}{
		{localDataPath("irs", "matis-FDS_v1-audio.irs"), true, 0.005, 2.0, "matis-FDS_v1(true stereo)"},
		{localDataPath("irs", "topjor-srs_2_1-audio.irs"), true, 0.005, 2.0, "topjor-srs_2_1(true stereo)"},
		{localDataPath("irs", "thepbone-clear_bass-audio.irs"), true, 0, 0.001, "clear_bass(2 channels but essentially mono)"},
	}
	loaded := 0
	for _, c := range cases {
		b, err := os.ReadFile(c.path)
		if err != nil {
			continue
		}
		loaded++
		resetIRState()
		info, err := loadIRInto(b, "stereo-probe")
		if err != nil {
			t.Fatalf("%s:load failed %v", c.label, err)
		}
		if info.Stereo != c.wantSter {
			t.Errorf("%s:sterco=%v,expected %v(channels=%d diff=%.6f)",
				c.label, info.Stereo, c.wantSter, info.Channels, info.ChannelDiff)
		}
		if info.ChannelDiff < c.minDiff {
			t.Errorf("%s:L/R coefficient difference %.6f below %.6f -- stereo information dropped ?", c.label, info.ChannelDiff, c.minDiff)
		}
		if info.ChannelDiff > c.maxDiff {
			t.Errorf("%s:L/R coefficient difference %.6f exceeds expected cap %.6f", c.label, info.ChannelDiff, c.maxDiff)
		}

		plan := irPlanCoefs()
		if len(plan) != 2*irTaps {
			t.Fatalf("%s:dispatch should carry 2x%d coefficients,actual %d", c.label, irTaps, len(plan))
		}

		same := 0
		for i := 0; i < irTaps; i++ {
			if plan[i] == plan[irTaps+i] {
				same++
			}
		}
		if c.wantSter && same == irTaps {
			t.Errorf("%s:both channels coefficients identical -- true stereo not in effect", c.label)
		}
		t.Logf("%s：channels=%d stereo=%v downmixed=%v channel_diff=%.6f taps=%d",
			c.label, info.Channels, info.Stereo, info.Downmixed, info.ChannelDiff, info.Taps)
	}
	if loaded == 0 {
		t.Skip("this machine lacks IR library")
	}
	resetIRState()
}

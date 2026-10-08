// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"strconv"
	"strings"
	"testing"
)

const hd800AutoEQ = `Preamp: -6.1 dB
Filter 1: ON LSC Fc 105 Hz Gain 6.4 dB Q 0.70
Filter 2: ON PK Fc 1928 Hz Gain 3.5 dB Q 1.28
Filter 3: ON PK Fc 176 Hz Gain -2.0 dB Q 0.58
Filter 4: ON PK Fc 5764 Hz Gain -7.8 dB Q 3.59
Filter 5: ON PK Fc 22 Hz Gain -0.4 dB Q 4.35
Filter 6: ON HSC Fc 10000 Hz Gain -4.2 dB Q 0.70
Filter 7: ON PK Fc 3534 Hz Gain 1.4 dB Q 5.71
Filter 8: ON PK Fc 3643 Hz Gain -0.3 dB Q 4.55
Filter 9: ON PK Fc 6708 Hz Gain 1.2 dB Q 6.00
Filter 10: ON PK Fc 1026 Hz Gain -0.2 dB Q 2.62
`

func TestParseFilterTypesIncludingShelves(t *testing.T) {

	pe, err := parseEqualizerAPO(hd800AutoEQ)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(pe.Chain) != 10 {
		t.Fatalf("Should parse 10 sections, got %d", len(pe.Chain))
	}
	if got := pe.Chain[0].Type; got != "lowshelf" {
		t.Errorf("LSC should map to lowshelf, got %q", got)
	}
	if got := pe.Chain[5].Type; got != "highshelf" {
		t.Errorf("HSC should map to highshelf, got %q", got)
	}
	if pe.PreampDB != -6.1 {
		t.Errorf("Preamp should be -6.1, got %.2f", pe.PreampDB)
	}
	if pe.Chain[4].Freq != 22 {
		t.Errorf("The 22 Hz section should parse (clamping happens at hardware deploy, not at parse), got %.0f", pe.Chain[4].Freq)
	}
}

func TestParseEqualizerAPOSyntaxVariants(t *testing.T) {
	cases := []struct {
		line     string
		wantType string
		wantFreq float64
		wantGain float64
		wantQ    float64
		qTol     float64
	}{
		{"Filter 1: ON PK Fc 1000 Hz Gain 3.0 dB Q 2.0", "peq", 1000, 3.0, 2.0, 0.01},
		{"Filter 1: ON PEQ Fc 100 Hz Gain 1.0 dB BW Oct 0.167", "peq", 100, 1.0, 8.63, 0.2},
		{"Filter 1: ON LS Fc 300 Hz Gain 5.0 dB", "lowshelf", 300, 5.0, 0.7, 0.01},
		{"Filter 1: ON HS Fc 1000 Hz Gain -3.0 dB", "highshelf", 1000, -3.0, 0.7, 0.01},
		{"Filter 1: ON LS 6dB Fc 50 Hz Gain 7.2 dB", "lowshelf", 50, 7.2, 0.7, 0.01},
		{"Filter 1: ON HS 12dB Fc 500 Hz Gain 5.0 dB", "highshelf", 500, 5.0, 0.7, 0.01},
		{"Filter 1: ON LP Fc 8000 Hz", "lowpass", 8000, 0, 0.7, 0.01},
		{"Filter 1: ON HPQ Fc 20 Hz Q 0.5", "highpass", 20, 0, 0.5, 0.01},
		{"Filter 1: ON NO Fc 50 Hz", "notch", 50, 0, 0.7, 0.01},
		{"Filter 1: ON AP Fc 900 Hz Q 0.707", "allpass", 900, 0, 0.707, 0.01},
		{"Filter 1: ON Modal Fc 100 Hz Gain 3.0 dB Q 5.41 T60 target 100 ms", "peq", 100, 3.0, 5.41, 0.01},
	}
	for _, c := range cases {
		pe, err := parseEqualizerAPO(c.line)
		if err != nil {
			t.Errorf("%s: parse failed %v", c.line, err)
			continue
		}
		if len(pe.Chain) != 1 {
			t.Errorf("%s: should yield 1 section, got %d", c.line, len(pe.Chain))
			continue
		}
		it := pe.Chain[0]
		if it.Type != c.wantType || it.Freq != c.wantFreq || it.GainDB != c.wantGain {
			t.Errorf("%s: got %+v, want type=%s freq=%v gain=%v", c.line, it, c.wantType, c.wantFreq, c.wantGain)
		}
		if d := it.Q - c.wantQ; d > c.qTol || d < -c.qTol {
			t.Errorf("%s: Q = %.3f, want %.3f+/-%.2f", c.line, it.Q, c.wantQ, c.qTol)
		}
	}

	pe, err := parseEqualizerAPO("Filter 1: OFF PK Fc 1000 Hz Gain 3 dB Q 1")
	if err == nil {
		t.Error("OFF-only lines should report no usable filters")
	}
	if pe == nil {
		t.Fatal("Errors should still return a readable parse result (callers need the warnings)")
	}
	if len(pe.Chain) != 0 {
		t.Errorf("OFF lines must not take effect, got %d sections", len(pe.Chain))
	}
}

func TestParseWarnsInsteadOfSilentlyDropping(t *testing.T) {
	text := `Preamp: -3 dB
Filter 1: ON PK Fc 1000 Hz Gain 2 dB Q 1
Convolution: church.wav
Copy: L=L+0.5*R
Delay: 12 ms
Channel: L
Device: Speakers
If: sampleRate == 48000
UnknownCmd: whatever
`
	pe, err := parseEqualizerAPO(text)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(pe.Chain) != 1 {
		t.Errorf("Only 1 valid filter section, got %d", len(pe.Chain))
	}
	joined := strings.Join(pe.Warnings, "\n")
	for _, want := range []string{"Convolution", "Copy", "Delay", "Channel", "Device", "expression", "unknown command"} {
		if !strings.Contains(joined, want) {
			t.Errorf("warnings should mention %q, got:\n%s", want, joined)
		}
	}

	for _, want := range []string{"P2", "P3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Hints should state which stage is needed (%s)", want)
		}
	}
}

func TestGraphicEQParsesAndFits(t *testing.T) {

	text := "GraphicEQ: 20 4; 30 3; 50 2; 80 1; 120 0.5; 200 0; 500 -0.5; 1000 -1; " +
		"2000 -2; 4000 -3; 6000 -4; 8000 -4.5; 10000 -5; 16000 -5\n"
	pe, err := parseEqualizerAPO(text)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(pe.Graphic) != 14 {
		t.Fatalf("Should parse 14 curve points, got %d", len(pe.Graphic))
	}
	p, warns, err := presetFromAPO(pe, "Graphic Curve", bandLimit())
	if err != nil {
		t.Fatalf("Assembly failed: %v", err)
	}
	if len(p.Chain) != bandLimit() {
		t.Errorf("Should fit to %d sections, got %d", bandLimit(), len(p.Chain))
	}
	if !strings.Contains(strings.Join(warns, "\n"), "RMS error") {
		t.Errorf("Must truthfully report fit error, got warnings: %v", warns)
	}

	if _, _, err := chainToSlots(p.Chain); err != nil {
		t.Errorf("Fit result should deploy to hardware: %v", err)
	}
}

func TestAutoEQTenBandsFitsIntoHardware(t *testing.T) {
	pe, err := parseEqualizerAPO(hd800AutoEQ)
	if err != nil {
		t.Fatal(err)
	}
	p, warns, err := presetFromAPO(pe, "HD 800 (AutoEQ)", bandLimit())
	if err != nil {
		t.Fatalf("Assembly failed: %v", err)
	}
	if len(p.Chain) > bandLimit() {
		t.Fatalf("Section count %d exceeds hardware %d", len(p.Chain), bandLimit())
	}
	if _, _, err := chainToSlots(p.Chain); err != nil {
		t.Fatalf("Fit result should deploy to hardware: %v", err)
	}
	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, "10 filter sections") || !strings.Contains(joined, "RMS error") {
		t.Errorf("Must state source file 10 sections fitted to N sections + error, got:\n%s", joined)
	}

	freqs := logGrid(20, 20000, 200)
	target := evalChainDB(pe.Chain, freqs)
	got := evalChainDB(p.Chain, freqs)
	rms := rmsDB(got, target)
	t.Logf("HD800 10 sections -> %d sections: RMS error %.2f dB, fit result: %s", len(p.Chain), rms, chainSummary(p.Chain))
	if rms > 3.0 {
		t.Errorf("Fit error %.2f dB too large (want < 3 dB)", rms)
	}

	at := func(f float64) float64 { return evalChainDB(p.Chain, []float64{f})[0] }
	if at(105) <= 0 {
		t.Errorf("105 Hz should be boosted (source curve +6.4 dB), got %.2f dB", at(105))
	}
	if at(5764) >= 0 {
		t.Errorf("5.7 kHz should be cut (source curve -7.8 dB), got %.2f dB", at(5764))
	}
}

func TestParseRejectsEmptyOrUselessFile(t *testing.T) {
	for _, text := range []string{"", "# comment only\n", "Preamp: -3 dB\n"} {
		if _, err := parseEqualizerAPO(text); err == nil {
			t.Errorf("Files with no usable filters should fail (input %q)", text)
		}
	}
}

func chainSummary(chain []ChainItem) string {
	parts := make([]string, 0, len(chain))
	for _, it := range chain {
		parts = append(parts, it.Type+"@"+trimFloat(it.Freq)+"Hz"+signed(it.GainDB)+"dB/Q"+trimFloat(it.Q))
	}
	return strings.Join(parts, " ")
}

func trimFloat(f float64) string {
	s := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(f, 'f', 1, 64), "0"), ".")
	return s
}

func signed(f float64) string {
	if f >= 0 {
		return "+" + strconv.FormatFloat(f, 'f', 1, 64)
	}
	return strconv.FormatFloat(f, 'f', 1, 64)
}

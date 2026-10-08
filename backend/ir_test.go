// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

func mkPCM16WAV(rate, nch int, frames int, f func(i, c int) float64) []byte {
	data := &bytes.Buffer{}
	for i := 0; i < frames; i++ {
		for c := 0; c < nch; c++ {
			v := int16(math.Round(f(i, c) * 32767))
			binary.Write(data, binary.LittleEndian, v)
		}
	}
	body := data.Bytes()
	buf := &bytes.Buffer{}
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(36+len(body)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(buf, binary.LittleEndian, uint32(16))
	binary.Write(buf, binary.LittleEndian, uint16(1))
	binary.Write(buf, binary.LittleEndian, uint16(nch))
	binary.Write(buf, binary.LittleEndian, uint32(rate))
	binary.Write(buf, binary.LittleEndian, uint32(rate*nch*2))
	binary.Write(buf, binary.LittleEndian, uint16(nch*2))
	binary.Write(buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	binary.Write(buf, binary.LittleEndian, uint32(len(body)))
	buf.Write(body)
	return buf.Bytes()
}

func TestParseWAVPCM16AndStereoAverage(t *testing.T) {

	w := mkPCM16WAV(48000, 2, 4, func(i, c int) float64 {
		if c == 0 {
			return 0.5
		}
		return -0.5
	})
	s, rate, ch, err := parseWAV(w)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if rate != 48000 || ch != 2 || len(s) != 4 {
		t.Fatalf("Wrong metadata: rate=%d ch=%d n=%d", rate, ch, len(s))
	}
	for i, v := range s {
		if math.Abs(v) > 1.0/32767 {
			t.Errorf("Sample %d should average L/R to 0, got %v", i, v)
		}
	}

	w1 := mkPCM16WAV(44100, 1, 3, func(i, c int) float64 { return 0.25 })
	s1, r1, c1, err := parseWAV(w1)
	if err != nil || r1 != 44100 || c1 != 1 {
		t.Fatalf("Mono parse failed: %v rate=%d ch=%d", err, r1, c1)
	}
	for _, v := range s1 {
		if math.Abs(v-0.25) > 1.0/32767 {
			t.Errorf("Mono sample should be 0.25, got %v", v)
		}
	}
}

func TestParseWAVRejectsGarbage(t *testing.T) {
	if _, _, _, err := parseWAV([]byte("not a wav at all, definitely not")); err == nil {
		t.Error("Garbage data should fail")
	}
	if _, _, _, err := parseWAV(nil); err == nil {
		t.Error("Empty data should fail")
	}
}

func TestResampleKeepsFrequencyAndAmplitude(t *testing.T) {
	const f0 = 1000.0

	src := make([]float64, 4410)
	for i := range src {
		src[i] = math.Sin(2 * math.Pi * f0 * float64(i) / 44100)
	}
	out := resampleSinc(src, 44100, 48000)

	if want := 4800; len(out) != want {
		t.Fatalf("Length after resampling should be %d, got %d", want, len(out))
	}

	var num, da, db float64
	for i := 100; i < len(out)-100; i++ {
		ref := math.Sin(2 * math.Pi * f0 * float64(i) / 48000)
		num += out[i] * ref
		da += out[i] * out[i]
		db += ref * ref
	}
	corr := num / math.Sqrt(da*db)
	if corr < 0.999 {
		t.Errorf("Post-resample correlation with target sine is only %.5f (want > 0.999)", corr)
	}

	peak := 0.0
	for _, v := range out[100 : len(out)-100] {
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	if peak < 0.98 || peak > 1.02 {
		t.Errorf("Post-resample peak %.3f (want ~=1.0)", peak)
	}
}

func TestIRPeakAlignmentAndQuantisation(t *testing.T) {

	x := make([]float64, irTaps)
	x[irTaps/2] = 0.5
	coefs, info, err := irToCoefs(x, irTaps)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if info.ZeroPad != 0 {
		t.Errorf("No zero-pad when peak is centered, got ZeroPad=%d", info.ZeroPad)
	}
	want := int32(math.Round(0.5 * (1 << irCoefShift)))
	if coefs[irTaps/2] != want {
		t.Errorf("Peak coefficient should be %d, got %d", want, coefs[irTaps/2])
	}
	if coefs[0] != 0 || coefs[irTaps-1] != 0 {
		t.Error("Points outside the window should be 0")
	}

	late := make([]float64, irTaps*2)
	late[irTaps+100] = 0.25
	_, info2, err := irToCoefs(late, irTaps)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if info2.PeakIndex != irTaps+100 {
		t.Errorf("Peak position should be reported truthfully, got %d", info2.PeakIndex)
	}

	early := make([]float64, irTaps)
	early[10] = 0.25
	_, info3, err := irToCoefs(early, irTaps)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if info3.ZeroPad != irTaps/2-10 {
		t.Errorf("Leading zero-pad should be %d, got %d", irTaps/2-10, info3.ZeroPad)
	}
}

func TestLoadIRResamplesAndReports(t *testing.T) {

	w := mkPCM16WAV(44100, 1, 4096, func(i, c int) float64 {
		if i == 1000 {
			return 0.75
		}
		return 0
	})
	banks, info, err := loadIR(w)
	if err != nil {
		t.Fatalf("loadIR failed: %v", err)
	}
	if len(banks[0]) != irTaps {
		t.Fatalf("Coefficient count should be %d, got %d", irTaps, len(banks[0]))
	}
	if info.SourceRate != 44100 || info.Channels != 1 {
		t.Errorf("Wrong metadata: rate=%d ch=%d", info.SourceRate, info.Channels)
	}
	wantPeak := 1000 * 48000 / 44100
	if d := info.PeakIndex - wantPeak; d < -3 || d > 3 {
		t.Errorf("Post-resample peak should be near %d (+/-3), got %d", wantPeak, info.PeakIndex)
	}

	mx := int32(0)
	for _, v := range banks[0] {
		if v > mx {
			mx = v
		}
	}
	lo := 0.5 * 0.75 * (1 << irCoefShift)
	hi := 1.05 * 0.75 * (1 << irCoefShift)
	if float64(mx) < lo || float64(mx) > hi {
		t.Errorf("Impulse peak coefficient %d outside [%.0f, %.0f]", mx, lo, hi)
	}
}

func TestLoadIRKeepsAmplitudeForBandlimitedPulse(t *testing.T) {
	const rate = 44100
	const center = 1500
	w := mkPCM16WAV(rate, 1, 4096, func(i, c int) float64 {
		d := float64(i - center)
		return 0.8 * math.Exp(-d*d/(2*12*12))
	})
	banks, info, err := loadIR(w)
	if err != nil {
		t.Fatalf("loadIR failed: %v", err)
	}
	if info.Stereo {
		t.Error("Mono IR must not be reported as true stereo")
	}
	mx := int32(0)
	for _, v := range banks[0] {
		if v > mx {
			mx = v
		}
	}
	want := 0.8 * (1 << irCoefShift)
	if float64(mx) < 0.95*want || float64(mx) > 1.02*want {
		t.Errorf("Band-limited impulse peak %d (want ~= %.0f, +/-5%%)", mx, want)
	}
	if d := info.PeakIndex - center*48000/rate; d < -3 || d > 3 {
		t.Errorf("Band-limited impulse peak should be near %d, got %d", center*48000/rate, info.PeakIndex)
	}
}

func TestLoadIRKeepsTrueStereo(t *testing.T) {
	const rate = 44100

	w := mkPCM16WAV(rate, 2, 4096, func(i, c int) float64 {
		if c == 0 {
			if i == 1000 {
				return 0.8
			}
			return 0
		}
		switch i {
		case 1000:
			return 0.4
		case 1010:
			return 0.3
		}
		return 0
	})
	banks, info, err := loadIR(w)
	if err != nil {
		t.Fatalf("loadIR failed: %v", err)
	}
	if !info.Stereo || info.Channels != 2 || info.Downmixed {
		t.Fatalf("Metadata should be true stereo: stereo=%v ch=%d downmixed=%v", info.Stereo, info.Channels, info.Downmixed)
	}
	if info.ChannelDiff < 0.1 {
		t.Errorf("Left/right coefficients should differ clearly (ChannelDiff=%.4f), but are nearly identical -- averaged to mono again?",
			info.ChannelDiff)
	}

	pk := func(b []int32) (int, int32) {
		idx, mx := 0, int32(0)
		for i, v := range b {
			if v > mx {
				mx, idx = v, i
			}
		}
		return idx, mx
	}
	i0, v0 := pk(banks[0])
	i1, v1 := pk(banks[1])
	if v0 == 0 || v1 == 0 {
		t.Fatal("Both channels should have nonzero peaks")
	}

	if r := float64(v1) / float64(v0); r < 0.4 || r > 0.6 {
		t.Errorf("Left/right peak ratio should be ~=0.5 (0.4/0.8), got %.2f -- channels may not share one gain", r)
	}

	if d := i1 - i0; d != 0 {
		t.Logf("Left peak at %d, right peak at %d (right main peak is source point 1000, so both should share the offset; secondary peak at 1020 counted separately)", i0, i1)
	}
}

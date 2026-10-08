// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestVSEGearTable(t *testing.T) {
	want := []struct {
		gear float64
		rec  int
		mix  float64
	}{
		{0.1, 56, 0.56}, {0.2, 112, 1.12}, {0.3, 168, 1.68}, {0.4, 224, 2.24},
		{0.5, 280, 2.80}, {0.6, 336, 3.36}, {0.7, 392, 3.92}, {0.8, 448, 4.48},
		{0.9, 504, 5.04}, {1.0, 560, 5.60},
	}
	tbl := vseGearTable()
	if len(tbl) != len(want) {
		t.Fatalf("level table has %d levels, panel has %d levels (arrays.xml vse_strength_values)", len(tbl), len(want))
	}
	for _, w := range want {
		r, err := vseReconstruct(w.gear)
		if err != nil {
			t.Fatalf("level %g: %v", w.gear, err)
		}
		if r != w.rec {
			t.Errorf("level %g: reconstruct = %d, want %d (round(UIx5.6x100))", w.gear, r, w.rec)
		}
		p, err := vseExciterParams(w.gear)
		if err != nil {
			t.Fatalf("level %g: %v", w.gear, err)
		}
		if math.Abs(p.Mix-w.mix) > 1e-9 {
			t.Errorf("level %g: core mix = %g, want %g (reconstruct/100)", w.gear, p.Mix, w.mix)
		}

		def := exciterDefaultParams()
		if p.Harmonics != def.Harmonics || p.HPFHz != 7600 || p.LPFHz != def.LPFHz {
			t.Errorf("level %g harmonic shape should match the factory shape, got %+v", w.gear, p)
		}
	}
}

func TestVSEGearValidation(t *testing.T) {
	for _, gear := range []float64{0.05, 0.15, 0.55, 1.1, 0, -0.1, math.NaN()} {
		if _, err := vseReconstruct(gear); err == nil {
			t.Errorf("level %v is not one of the panel 10 discrete levels, must fail", gear)
		}
	}
}

func TestVSEGearRoundTrip(t *testing.T) {
	for step := 1; step <= 10; step++ {
		gear := float64(step) * vseGearStep
		p, err := vseExciterParams(gear)
		if err != nil {
			t.Fatalf("level %g: %v", gear, err)
		}
		got, ok := vseGearOf(p)
		if !ok || math.Abs(got-gear) > 1e-9 {
			t.Errorf("level %g reverse-derives to (%g, %v)", gear, got, ok)
		}
	}
	p := exciterDefaultParams()
	p.Harmonics[1] = 0.5
	if _, ok := vseGearOf(p); ok {
		t.Error("custom harmonic shape must not count as a VSE level")
	}
}

func TestVSETopGearsReachable(t *testing.T) {
	for _, gear := range []float64{0.8, 0.9, 1.0} {
		p, err := vseExciterParams(gear)
		if err != nil {
			t.Fatalf("level %g: %v", gear, err)
		}
		if err := validateExciterMix(p); err != nil {
			t.Errorf("level %g (core mix=%.2f) should download: %v", gear, p.Mix, err)
		}
		nodes, err := exciterNodes(p)
		if err != nil {
			t.Fatalf("level %g failed to compile: %v", gear, err)
		}
		wet := nodes[0].Mix[1]
		if wet > coefMax {
			t.Errorf("level %g wet coefficient %d exceeds the Q3.15 ceiling %d (would saturate silently)", gear, wet, coefMax)
		}

		if nodes[0].MixB[0] != 0 {
			plan, err := buildSlotPlanNodes(nodes)
			if err != nil {
				t.Fatalf("level %g failed slot planning: %v", gear, err)
			}
			if plan.Slots != 5 {
				t.Errorf("level %g should expand to 5 slots (HPF/POLY/LPF/gain stage/MIX2), got %d", gear, plan.Slots)
			}
			sl := decodeSlots(t, plan)
			g := sl[3]
			if g.Op != opMix2 || g.In != g.InB || g.In != g.Out {
				t.Errorf("level %g gain stage should be MIX2 with in_a=in_b=out (in place), got op=%d in=%d in_b=%d out=%d",
					gear, g.Op, g.In, g.InB, g.Out)
			}
			if sl[4].InB != g.Out {
				t.Errorf("level %g: final-mix wet input (%d) should be the gain-stage bus (%d)", gear, sl[4].InB, g.Out)
			}
		} else if nodes[0].MixB[0] == 0 && gear > 0.3 {
			t.Errorf("level %g equivalent wet gain %.2f exceeds the MIX2 single-coefficient 4.0, should have a gain stage",
				gear, p.Mix/exciterWetScale(p))
		}

		scale := exciterWetScale(p)
		g := float64(1)
		if nodes[0].MixB[0] != 0 {
			g = float64(nodes[0].MixB[0]) / 32768.0
		}
		eff := float64(wet) / 32768.0 * g * scale
		if math.Abs(eff-p.Mix) > 0.02 {
			t.Errorf("level %g: equivalent wet gain %.3f, want %.3f (scaling must cancel out exactly)", gear, eff, p.Mix)
		}
	}
}

func vseHarmonics(t *testing.T, p exciterParams, f0, amp float64) []float64 {
	t.Helper()
	nodes, err := exciterNodes(p)
	if err != nil {
		t.Fatalf("failed to compile exciter: %v", err)
	}
	n := nodes[0]
	var c [11]float64
	for k := 0; k < 11; k++ {
		c[k] = float64(n.Poly[k]) / 32768.0
	}
	hpf := [5]int32{n.Coefs[0], n.Coefs[1], n.Coefs[2], n.Coefs[3], n.Coefs[4]}
	lpf := [5]int32{n.Hi[0], n.Hi[1], n.Hi[2], n.Hi[3], n.Hi[4]}
	dry := float64(n.Mix[0]) / 32768.0
	wet := float64(n.Mix[1]) / 32768.0
	if n.MixB[0] != 0 {
		wet *= float64(n.MixB[0]) / 32768.0
	}

	h := biquadResponse(hpf, f0, sampleRate)
	A := amp / 32768.0
	Ah := cmplx.Abs(h) * A
	phase := cmplx.Phase(h)

	const N = 8192
	xs := make([]float64, N)
	ys := make([]float64, N)
	for i := 0; i < N; i++ {
		x := Ah * math.Cos(2*math.Pi*float64(i)/N)
		xs[i] = x
		acc := 0.0
		for k := 10; k >= 0; k-- {
			acc = acc*x + c[k]
		}
		ys[i] = acc
	}
	out := make([]float64, 11)
	for m := 1; m <= 10; m++ {

		var s complex128
		for i := 0; i < N; i++ {
			s += complex(ys[i], 0) * cmplx.Exp(complex(0, -2*math.Pi*float64(m)*float64(i)/N))
		}
		Hm := 2 * s / N

		w := 2 * math.Pi * float64(m) * f0 / sampleRate
		z := cmplx.Exp(complex(0, -w))
		leaky := (1 - z) / (1 - 0.999*z)

		hm := complex(wet, 0) * Hm * cmplx.Exp(complex(0, float64(m)*phase)) * leaky *
			biquadResponse(lpf, float64(m)*f0, sampleRate) * 32768
		d := complex(0, 0)
		if m == 1 {
			d = complex(dry*amp, 0)
		}
		out[m] = cmplx.Abs(d + hm)
	}
	return out
}

func TestVSEMatchesOfficialCore(t *testing.T) {
	cases := []struct {
		name       string
		refbark    float64
		recon      int
		f0         float64
		h1, h3, h5 float64
	}{
		{"default level UI 0.1", 7600, 56, 3000, 19791.2513, 3.9619, 0.2962},
		{"full level UI 1.0", 7600, 560, 3000, 18763.4086, 38.2120, 0.5301},
		{"REFERENCE_BARK=5000", 5000, 560, 4000, 0, 1353.2531, 212.9381},
		{"REFERENCE_BARK=7600", 7600, 560, 4000, 0, 200.2537, 4.4135},
		{"REFERENCE_BARK=10000", 10000, 560, 4000, 0, 28.7178, 0.3910},
	}
	worst, worstAt := 0.0, ""
	for _, c := range cases {

		p := exciterDefaultParams()
		p.HPFHz = c.refbark
		p.Mix = float64(c.recon) / 100.0
		got := vseHarmonics(t, p, c.f0, 20000)
		for _, m := range []struct {
			order int
			meas  float64
		}{{1, c.h1}, {3, c.h3}, {5, c.h5}} {
			if m.meas == 0 {
				continue
			}

			if m.order != 1 && m.meas < 0.6 {
				continue
			}
			d := 20 * math.Log10(got[m.order]/m.meas)
			if math.Abs(d) > worst {
				worst, worstAt = math.Abs(d), c.name
			}
			if math.Abs(d) > 0.6 {
				t.Errorf("%s: harmonic %d: device %.4f, reference core %.4f (diff %+.2f dB, tolerance 0.6)",
					c.name, m.order, got[m.order], m.meas, d)
			}
		}
	}
	t.Logf("VSE max deviation vs reference core %.2f dB (at %s)", worst, worstAt)
}

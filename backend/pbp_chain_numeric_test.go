// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"math/cmplx"
	"testing"
)

func sourcePBPResponse(f, gain, cutoff, fs, delay float64) complex128 {
	hfir := complex(0, 0)
	for i, c := range viperBassPBPKernel {
		hfir += complex(c, 0) * cmplx.Exp(complex(0, -2*math.Pi*f*float64(i)/fs))
	}
	b0, b1, b2, a1, a2 := rbjLowPassFloat(cutoff, viperBassQ, fs)
	z := cmplx.Exp(complex(0, -2*math.Pi*f/fs))
	hlp := (complex(b0, 0) + complex(b1, 0)*z + complex(b2, 0)*z*z) /
		(complex(1, 0) + complex(a1, 0)*z + complex(a2, 0)*z*z)
	wet := complex(gain/100.0, 0) * hlp * cmplx.Exp(complex(0, -2*math.Pi*f*delay/fs))
	return hfir + wet
}

func chainPBPResponse(n planNode, f, fs float64) complex128 {
	q := func(v int32) float64 { return float64(v) / 32768.0 }
	z := cmplx.Exp(complex(0, -2*math.Pi*f/fs))

	hfir := complex(0, 0)
	p := complex(1, 0)
	for _, c := range n.SFir {
		hfir += complex(q(c), 0) * p
		p *= z
	}
	hlp := (complex(q(n.Coefs[0]), 0) + complex(q(n.Coefs[1]), 0)*z + complex(q(n.Coefs[2]), 0)*z*z) /
		(complex(1, 0) + complex(q(n.Coefs[3]), 0)*z + complex(q(n.Coefs[4]), 0)*z*z)
	pre := complex(q(n.Extra[0]), 0)
	return complex(q(n.Mix[0]), 0)*hfir +
		complex(q(n.Mix[1]), 0)*pre*hlp*cmplx.Exp(complex(0, -2*math.Pi*f*float64(n.Len)/fs))
}

func db(c complex128) float64 { return 20 * math.Log10(cmplx.Abs(c)) }

func TestViPERBassPBPChainNumericResponse(t *testing.T) {
	fs := float64(sampleRate)
	freqs := []float64{40, 60, 120, 200, 240, 2000}
	gains := []float64{50, 100, 150, 200, 300, 400, 600}

	worst := 0.0
	for _, fc := range []float64{60, 100} {
		for _, g := range gains {
			nodes, err := viperBassNodes(viperBassParams{Mode: viperBassModePBP, CutoffHz: fc, Gain: g})
			if err != nil {
				t.Fatalf("fc=%g gain=%g: PBP should compile: %v", fc, g, err)
			}
			if len(nodes) != 1 || nodes[0].Kind != planKindViPERBassPBP {
				t.Fatalf("fc=%g gain=%g: want one planKindViPERBassPBP node, got %+v", fc, g, nodes)
			}
			n := nodes[0]
			for _, f := range freqs {
				got := db(chainPBPResponse(n, f, fs))
				want := db(sourcePBPResponse(f, g, fc, fs, 63))
				if d := math.Abs(got - want); d > worst {
					worst = d
				}
				if math.Abs(got-want) > 1.0 {
					t.Errorf("fc=%g gain=%g f=%gHz: downloaded chain %+.2f dB vs source accounting %+.2f dB"+
						"(diff %.2f dB) -- one of scaling/MIX2 weights/lowpass coefficients disagrees with the source",
						fc, g, f, got, want, math.Abs(got-want))
				}
			}
		}
	}
	t.Logf("downloaded chain vs source accounting: %d groups (fc 2 levels x gain 7 levels x %d frequency points) max deviation %.3f dB",
		2*len(gains), len(freqs), worst)
}

func TestViPERBassPBPDipIsSourceInherent(t *testing.T) {
	fs := float64(sampleRate)
	seq := []float64{}
	for _, g := range []float64{50, 150, 300, 600} {
		nodes, err := viperBassNodes(viperBassParams{Mode: viperBassModePBP, CutoffHz: 100, Gain: g})
		if err != nil {
			t.Fatalf("gain=%g：%v", g, err)
		}
		got := db(chainPBPResponse(nodes[0], 200, fs))
		want := db(sourcePBPResponse(200, g, 100, fs, 63))
		seq = append(seq, got)
		t.Logf("cutoff=100Hz gain=%-3g -> 200Hz downloaded chain %+.2f dB / source %+.2f dB", g, got, want)
		if math.Abs(got-want) > 0.35 {
			t.Errorf("gain=%g: downloaded chain differs from source by %.2f dB", g, math.Abs(got-want))
		}
	}
	if !(seq[1] < seq[0] && seq[2] > seq[1]) {
		t.Errorf("source inherent dip missing (sequence %v) -- if the implementation changed phase/delay accounting, "+
			"this test goes red first; then recheck the source instead of changing the test", seq)
	}
}

// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
)

type vdcSection [5]float64

func vdcResponseDB(secs []vdcSection, f, fs float64) float64 {
	w := 2 * math.Pi * f / fs

	c1, s1 := math.Cos(w), -math.Sin(w)
	c2, s2 := math.Cos(2*w), -math.Sin(2*w)
	acc := 1.0
	for _, s := range secs {
		b0, b1, b2, a1, a2 := s[0], s[1], s[2], s[3], s[4]

		nr := b0 + b1*c1 + b2*c2
		ni := b1*s1 + b2*s2

		dr := 1 - a1*c1 - a2*c2
		di := -(a1*s1 + a2*s2)
		num := math.Hypot(nr, ni)
		den := math.Hypot(dr, di)
		if den < 1e-12 {
			den = 1e-12
		}
		acc *= num / den
	}
	return 20 * math.Log10(math.Max(acc, 1e-12))
}

func parseVDC(b []byte, wantRate int) (secs []vdcSection, rate int, err error) {
	lines := strings.Split(string(b), "\n")
	bestDiff := 1 << 30
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if !strings.HasPrefix(ln, "SR_") {
			continue
		}
		colon := strings.Index(ln, ":")
		if colon < 0 {
			continue
		}
		r, perr := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(ln[:colon], "SR_")))
		if perr != nil || r <= 0 {
			continue
		}
		vals := []float64{}
		for _, tok := range strings.Split(ln[colon+1:], ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			v, verr := strconv.ParseFloat(tok, 64)
			if verr != nil {
				return nil, 0, fmt.Errorf("SR_%d that line has unparsable number:%q", r, tok)
			}
			vals = append(vals, v)
		}
		if len(vals) < 5 {
			continue
		}

		var got []vdcSection
		for i := 0; i+5 <= len(vals); i += 5 {
			var s vdcSection
			copy(s[:], vals[i:i+5])

			if math.Abs(s[0])+math.Abs(s[1])+math.Abs(s[2]) < 1e-9 {
				continue
			}
			got = append(got, s)
		}
		if len(got) == 0 {
			continue
		}
		d := r - wantRate
		if d < 0 {
			d = -d
		}
		if d < bestDiff {
			bestDiff, secs, rate = d, got, r
		}
	}
	if secs == nil {
		return nil, 0, fmt.Errorf("this file has no SR_<sample rate>: coefficient lines (not a ViPER DDC?)")
	}
	return secs, rate, nil
}

func vdcToQ315(secs []vdcSection) ([][5]int32, float64, error) {
	const maxQ = 3.9999694824
	lim := 0.0
	for _, s := range secs {
		for _, v := range s {
			if a := math.Abs(v); a > lim {
				lim = a
			}
		}
	}
	if lim == 0 {
		return nil, 0, fmt.Errorf("coefficient all zero")
	}
	scale := 1.0
	if lim > maxQ {
		scale = maxQ / lim
	}
	out := make([][5]int32, 0, len(secs))
	for _, s := range secs {

		vals := [5]float64{s[0], s[1], s[2], -s[3], -s[4]}
		var q [5]int32
		for i, v := range vals {

			qv := int32(math.Round(v * scale * 32768))
			if qv > 131071 {
				qv = 131071
			}
			if qv < -131072 {
				qv = -131072
			}
			q[i] = qv
		}
		out = append(out, q)
	}
	return out, scale, nil
}

func vdcQ315ResponseDB(q [][5]int32, f, fs float64) float64 {
	w := 2 * math.Pi * f / fs
	c1, s1 := math.Cos(w), -math.Sin(w)
	c2, s2 := math.Cos(2*w), -math.Sin(2*w)
	acc := 1.0
	for _, s := range q {
		b0 := float64(s[0]) / 32768.0
		b1 := float64(s[1]) / 32768.0
		b2 := float64(s[2]) / 32768.0
		c3 := float64(s[3]) / 32768.0
		c4 := float64(s[4]) / 32768.0
		nr := b0 + b1*c1 + b2*c2
		ni := b1*s1 + b2*s2
		dr := 1 + c3*c1 + c4*c2
		di := c3*s1 + c4*s2
		num := math.Hypot(nr, ni)
		den := math.Hypot(dr, di)
		if den < 1e-12 {
			den = 1e-12
		}
		acc *= num / den
	}
	return 20 * math.Log10(math.Max(acc, 1e-12))
}

func vdcSectionsToChain(q [][5]int32) [][5]int32 { return q }

var _ = binary.LittleEndian

var (
	ddcMu       sync.Mutex
	ddcSections [][5]int32

	ddcNative bool
	ddcName   string
	ddcRate   int
)

func setDDCFromVDC(body []byte, name string) (int, error) {
	secs, rate, err := parseVDC(body, 48000)
	if err != nil {
		return 0, err
	}
	if len(secs) > maxSections {
		return 0, fmt.Errorf("this .vdc carries %d sections,exceeds engine cap %d sections(NSEC)--"+
			"either fit fewer sections with ?sections=N or expand the engine", len(secs), maxSections)
	}
	q, _, err := vdcToQ315(secs)
	if err != nil {
		return 0, err
	}
	ddcMu.Lock()
	defer ddcMu.Unlock()
	ddcNative = true
	ddcSections = q

	nm := strings.TrimSpace(name)
	if nm == "" {
		nm = "ddc.vdc"
	}
	ddcName = irNameSafe(nm)
	ddcRate = rate
	return len(q), nil
}

func ddcClear() {
	ddcMu.Lock()
	ddcSections, ddcName, ddcRate, ddcNative = nil, "", 0, false
	ddcMu.Unlock()
}

func ddcLoadedName() string {
	ddcMu.Lock()
	defer ddcMu.Unlock()
	return ddcName
}

func ddcNodes() []planNode {
	ddcMu.Lock()
	defer ddcMu.Unlock()
	if len(ddcSections) == 0 {
		return nil
	}
	out := make([]planNode, 0, len(ddcSections))
	for _, s := range ddcSections {
		out = append(out, planNode{Kind: planKindBiquad, Coefs: s})
	}
	return out
}

func ddcView() map[string]any {
	ddcMu.Lock()
	defer ddcMu.Unlock()
	v := map[string]any{
		"on":       len(ddcSections) > 0,
		"name":     ddcName,
		"rate":     ddcRate,
		"sections": len(ddcSections),

		"native": ddcNative,
	}

	if len(ddcSections) > 0 {
		ref := vdcQ315ResponseDB(ddcSections, 1000, 48000)
		resp := map[string]float64{}
		for _, f := range []float64{100, 1000, 4000, 8000, 10000, 12000, 16000} {
			resp[fmt.Sprintf("%.0f", f)] = math.Round((vdcQ315ResponseDB(ddcSections, f, 48000)-ref)*10) / 10
		}
		v["resp_db"] = resp
	}
	return v
}

func butterworthLP(n int, fc, fs float64) []vdcSection {
	out := make([]vdcSection, 0, n)
	w0 := 2 * math.Pi * fc / fs
	cs, sn := math.Cos(w0), math.Sin(w0)

	for k := n - 1; k >= 0; k-- {
		q := 1.0 / (2.0 * math.Sin((2*float64(k)+1)*math.Pi/(4*float64(n))))
		alpha := sn / (2 * q)
		b1 := 1 - cs
		b0 := b1 / 2
		b2 := b0
		a0 := 1 + alpha
		a1 := -2 * cs
		a2 := 1 - alpha
		out = append(out, vdcSection{b0 / a0, b1 / a0, b2 / a0, a1 / a0, a2 / a0})
	}
	return out
}

func sectionsToQ315Engine(secs []vdcSection) [][5]int32 {
	const maxQ = 3.9999694824
	lim := 0.0
	for _, s := range secs {
		for _, v := range s {
			if a := math.Abs(v); a > lim {
				lim = a
			}
		}
	}
	scale := 1.0
	if lim > maxQ {
		scale = maxQ / lim
	}
	out := make([][5]int32, 0, len(secs))
	for _, s := range secs {
		var q [5]int32
		for i, v := range s {
			qv := int32(math.Round(v * scale * 32768))
			if qv > 131071 {
				qv = 131071
			}
			if qv < -131072 {
				qv = -131072
			}
			q[i] = qv
		}
		out = append(out, q)
	}
	return out
}

func vdcMinus3dB(secs []vdcSection, fs float64) float64 {
	ref := vdcResponseDB(secs, 1000, fs)
	lo, hi := 1000.0, fs/2*0.999
	for i := 0; i < 60; i++ {
		mid := math.Sqrt(lo * hi)
		if vdcResponseDB(secs, mid, fs)-ref > -3.0 {
			lo = mid
		} else {
			hi = mid
		}
	}
	return math.Sqrt(lo * hi)
}

func fitDDCFromVDC(body []byte, name string, n int) (int, float64, float64, error) {
	src, _, err := parseVDC(body, 48000)
	if err != nil {
		return 0, 0, 0, err
	}
	if n < 1 {
		n = 4
	}
	fc := vdcMinus3dB(src, 48000)
	fit := butterworthLP(n, fc, 48000)
	q := sectionsToQ315Engine(fit)
	ddcMu.Lock()
	ddcSections = q
	nm := strings.TrimSpace(name)
	if nm == "" {
		nm = "ddc.vdc"
	}
	ddcName = irNameSafe(nm)
	ddcRate = 48000
	ddcMu.Unlock()

	srcRef := vdcResponseDB(src, 1000, 48000)
	fitRef := vdcQ315ResponseDB(q, 1000, 48000)
	worst := 0.0
	for _, f := range []float64{100, 200, 500, 1000, 2000, 4000, 6000, 8000, 10000, 12000} {
		d := math.Abs((vdcResponseDB(src, f, 48000) - srcRef) - (vdcQ315ResponseDB(q, f, 48000) - fitRef))
		if d > worst {
			worst = d
		}
	}
	return len(q), fc, worst, nil
}

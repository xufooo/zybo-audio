// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	irTaps      = 8192
	irCoefShift = 15
	irCoefMax   = 3.9999694824
)

type IRInfo struct {
	Taps       int `json:"taps"`
	SourceRate int `json:"source_rate"`
	Channels   int `json:"channels"`

	Stereo bool `json:"stereo"`

	Downmixed bool `json:"downmixed"`

	ChannelDiff float64 `json:"channel_diff"`
	PeakIndex   int     `json:"peak_index"`
	Peak        float64 `json:"peak"`
	Gain        float64 `json:"gain"`
	ZeroPad     int     `json:"zero_pad"`
}

func parseWAVTracks(b []byte) (tracks [][]float64, rate int, err error) {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, fmt.Errorf("Not a WAV (missing RIFF/WAVE)")
	}
	var (
		fmtTag, bits, ch int
		rateV            int
		data             []byte
	)
	for p := 12; p+8 <= len(b); {
		id := string(b[p : p+4])
		sz := int(binary.LittleEndian.Uint32(b[p+4 : p+8]))
		body := p + 8
		if sz < 0 || body+sz > len(b) {
			sz = len(b) - body
		}
		switch id {
		case "fmt ":
			if sz < 16 {
				return nil, 0, fmt.Errorf("fmt chunk too short")
			}
			fmtTag = int(binary.LittleEndian.Uint16(b[body : body+2]))
			ch = int(binary.LittleEndian.Uint16(b[body+2 : body+4]))
			rateV = int(binary.LittleEndian.Uint32(b[body+4 : body+8]))
			bits = int(binary.LittleEndian.Uint16(b[body+14 : body+16]))
		case "data":
			data = b[body : body+sz]
		}
		p = body + sz
		if sz%2 == 1 {
			p++
		}
	}
	if ch <= 0 || rateV <= 0 {
		return nil, 0, fmt.Errorf("WAV missing fmt chunk")
	}
	if data == nil {
		return nil, 0, fmt.Errorf("WAV missing data chunk")
	}
	bytesPer := bits / 8
	if bytesPer == 0 {
		return nil, 0, fmt.Errorf("Unsupported bit depth %d", bits)
	}
	sample := func(off int) (float64, error) {
		switch {
		case fmtTag == 3 && bits == 32:
			return float64(math.Float32frombits(binary.LittleEndian.Uint32(data[off : off+4]))), nil
		case bits == 16:
			return float64(int16(binary.LittleEndian.Uint16(data[off:off+2]))) / 32768.0, nil
		case bits == 24:
			v := int32(data[off]) | int32(data[off+1])<<8 | int32(data[off+2])<<16
			if v&0x800000 != 0 {
				v |= ^0xFFFFFF
			}
			return float64(v) / 8388608.0, nil
		case bits == 32:
			return float64(int32(binary.LittleEndian.Uint32(data[off:off+4]))) / 2147483648.0, nil
		}
		return 0, fmt.Errorf("Unsupported bit depth %d / format %d", bits, fmtTag)
	}
	n := len(data) / (bytesPer * ch)
	tracks = make([][]float64, ch)
	for c := range tracks {
		tracks[c] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for c := 0; c < ch; c++ {
			v, err := sample((i*ch + c) * bytesPer)
			if err != nil {
				return nil, 0, err
			}
			tracks[c][i] = v
		}
	}
	return tracks, rateV, nil
}

func parseWAV(b []byte) (samples []float64, rate, channels int, err error) {
	tracks, rateV, err := parseWAVTracks(b)
	if err != nil {
		return nil, 0, 0, err
	}
	out := make([]float64, len(tracks[0]))
	for i := range out {
		sum := 0.0
		for _, t := range tracks {
			sum += t[i]
		}
		out[i] = sum / float64(len(tracks))
	}
	return out, rateV, len(tracks), nil
}

func resampleSinc(x []float64, from, to int) []float64 {
	if from == to || len(x) == 0 {
		return append([]float64(nil), x...)
	}
	const half = 16
	nOut := int(float64(len(x)) * float64(to) / float64(from))
	out := make([]float64, nOut)
	ratio := float64(from) / float64(to)
	for i := 0; i < nOut; i++ {
		pos := float64(i) * ratio
		i0 := int(math.Floor(pos))
		var acc, wsum float64
		for k := i0 - half + 1; k <= i0+half; k++ {
			if k < 0 || k >= len(x) {
				continue
			}
			t := pos - float64(k)
			if math.Abs(t) >= float64(half) {
				continue
			}

			s := 1.0
			if math.Abs(t) > 1e-12 {
				s = math.Sin(math.Pi*t) / (math.Pi * t)
			}
			w := 0.42 + 0.5*math.Cos(math.Pi*t/float64(half)) +
				0.08*math.Cos(2*math.Pi*t/float64(half))
			acc += x[k] * s * w
			wsum += s * w
		}
		if math.Abs(wsum) > 1e-9 {
			out[i] = acc / wsum
		}
	}
	return out
}

func irQuantiseWindow(x []float64, start, taps int, gain float64) []int32 {
	coefs := make([]int32, taps)
	for i := 0; i < taps; i++ {
		idx := start + i
		if idx < 0 || idx >= len(x) {
			continue
		}
		q := int32(math.Round(x[idx] * gain * (1 << irCoefShift)))
		if q > (1<<17)-1 {
			q = (1 << 17) - 1
		}
		if q < -(1 << 17) {
			q = -(1 << 17)
		}
		coefs[i] = q
	}
	return coefs
}

func irToCoefs(x []float64, taps int) (coefs []int32, info IRInfo, err error) {
	if len(x) == 0 {
		return nil, IRInfo{}, fmt.Errorf("IR is empty")
	}
	pk, pv := 0, 0.0
	for i, v := range x {
		if a := math.Abs(v); a > pv {
			pv, pk = a, i
		}
	}
	if pv == 0 {
		return nil, IRInfo{}, fmt.Errorf("IR is all zeros")
	}
	start := pk - taps/2
	gain := 1.0
	if pv > irCoefMax {
		gain = irCoefMax / pv
	}
	coefs = irQuantiseWindow(x, start, taps, gain)
	info = IRInfo{
		Taps: taps, Channels: 1, PeakIndex: pk, Peak: pv, Gain: gain,
		ZeroPad: func() int {
			if start < 0 {
				return -start
			}
			return 0
		}(),
	}
	return coefs, info, nil
}

func loadIR(b []byte) ([2][]int32, IRInfo, error) {
	tracks, rate, err := parseWAVTracks(b)
	if err != nil {
		return [2][]int32{}, IRInfo{}, err
	}
	rs := make([][]float64, len(tracks))
	for i, t := range tracks {
		rs[i] = resampleSinc(t, rate, 48000)
	}
	var ch2 [2][]float64
	info := IRInfo{SourceRate: rate, Channels: len(rs), Taps: irTaps}
	switch len(rs) {
	case 1:
		ch2[0], ch2[1] = rs[0], rs[0]
	case 2:
		ch2[0], ch2[1] = rs[0], rs[1]
		info.Stereo = true
	default:
		n := len(rs[0])
		mono := make([]float64, n)
		for i := 0; i < n; i++ {
			sum := 0.0
			for _, t := range rs {
				sum += t[i]
			}
			mono[i] = sum / float64(len(rs))
		}
		ch2[0], ch2[1] = mono, mono
		info.Downmixed = true
	}
	pk, pv := 0, 0.0
	for _, t := range ch2 {
		for i, v := range t {
			if a := math.Abs(v); a > pv {
				pv, pk = a, i
			}
		}
	}
	if pv == 0 {
		return [2][]int32{}, IRInfo{}, fmt.Errorf("IR is all zeros")
	}
	start := pk - irTaps/2
	gain := 1.0
	if pv > irCoefMax {
		gain = irCoefMax / pv
	}
	var out [2][]int32
	for c := 0; c < 2; c++ {
		out[c] = irQuantiseWindow(ch2[c], start, irTaps, gain)
	}
	info.PeakIndex, info.Peak, info.Gain = pk, pv, gain
	if start < 0 {
		info.ZeroPad = -start
	}
	var maxd int32
	for i := range out[0] {
		d := out[0][i] - out[1][i]
		if d < 0 {
			d = -d
		}
		if d > maxd {
			maxd = d
		}
	}
	info.ChannelDiff = float64(maxd) / float64(int32(1)<<irCoefShift)
	return out, info, nil
}

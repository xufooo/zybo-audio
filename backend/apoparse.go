// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type graphicPoint struct {
	Freq   float64
	GainDB float64
}

type parsedEQ struct {
	PreampDB float64
	Chain    []ChainItem
	Graphic  []graphicPoint
	Warnings []string
}

func (p *parsedEQ) warnf(format string, args ...any) {
	p.Warnings = append(p.Warnings, fmt.Sprintf(format, args...))
}

func parseEqualizerAPO(text string) (*parsedEQ, error) {
	pe := &parsedEQ{}
	lines := strings.Split(text, "\n")
	for ln, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.Index(line, ":")
		if i < 0 {
			pe.warnf("Line %d is not in command: args form, ignored: %s", ln+1, trimForMsg(line))
			continue
		}
		cmd := strings.TrimSpace(line[:i])
		arg := strings.TrimSpace(line[i+1:])
		lower := strings.ToLower(cmd)

		switch {
		case lower == "preamp":
			v, ok := parseLeadingFloat(arg)
			if !ok {
				pe.warnf("Line %d: Preamp parse failed, ignored: %s", ln+1, trimForMsg(line))
				continue
			}

			pe.PreampDB += v
		case strings.HasPrefix(lower, "filter"):
			it, ok, err := parseFilterSpec(arg)
			if err != nil {
				pe.warnf("Line %d: %v", ln+1, err)
				continue
			}
			if !ok {
				continue
			}
			pe.Chain = append(pe.Chain, it)
		case lower == "graphiceq":
			pts, err := parseGraphicEQ(arg)
			if err != nil {
				pe.warnf("Line %d: %v", ln+1, err)
				continue
			}
			pe.Graphic = append(pe.Graphic, pts...)
		case lower == "channel" || lower == "device" || lower == "stage" || lower == "include":
			pe.warnf("Line %d: %s is a file/channel management directive, not accepted locally (stereo single-file only)", ln+1, cmd)
		case lower == "convolution":
			pe.warnf("Line %d: Convolution needs a convolver (P3), not supported locally yet", ln+1)
		case lower == "copy":
			pe.warnf("Line %d: Copy (channel matrix) needs inter-channel processing (P2), not supported locally yet", ln+1)
		case lower == "delay":
			pe.warnf("Line %d: Delay needs time-domain processing (P2), not supported locally yet", ln+1)
		case lower == "if" || lower == "elseif" || lower == "else" || lower == "endif" ||
			lower == "eval" || lower == "device":
			pe.warnf("Line %d: expression/conditional directive (%s) not supported, ignored", ln+1, cmd)
		default:
			pe.warnf("Line %d: unknown command %q, ignored", ln+1, cmd)
		}
	}
	if len(pe.Chain) == 0 && len(pe.Graphic) == 0 {

		return pe, fmt.Errorf("No usable filters in file (no Filter / GraphicEQ lines)")
	}
	return pe, nil
}

func trimForMsg(s string) string {
	if len(s) > 60 {
		return s[:60] + "..."
	}
	return s
}

func parseFilterSpec(arg string) (ChainItem, bool, error) {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		return ChainItem{}, false, nil
	}
	state := strings.ToUpper(fields[0])
	if state == "OFF" {
		return ChainItem{}, false, nil
	}
	if state != "ON" {

		fields = append([]string{"ON"}, fields...)
	}
	if len(fields) < 2 {
		return ChainItem{}, false, fmt.Errorf("Filter is missing a type: %s", trimForMsg(arg))
	}

	typ := strings.ToUpper(fields[1])
	rest := fields[2:]
	chainType := ""
	approx := ""
	switch typ {
	case "PK", "PEQ", "MODAL", "PARAMETRIC":
		chainType = "peq"
	case "LP", "LPQ":
		chainType = "lowpass"
	case "HP", "HPQ":
		chainType = "highpass"
	case "BP":
		chainType = "bandpass"
	case "NO", "NOTCH":
		chainType = "notch"
	case "AP", "ALLPASS":
		chainType = "allpass"
	case "LS", "LSC":
		chainType = "lowshelf"
	case "HS", "HSC":
		chainType = "highshelf"
	default:
		return ChainItem{}, false, fmt.Errorf("Unknown filter type %q (this line not applied)", typ)
	}

	if typ == "LS" || typ == "HS" {
		if len(rest) > 0 {
			switch strings.ToUpper(rest[0]) {
			case "6DB":
				approx = "6 dB/oct slope approximated with Q~=0.5"
				rest = rest[1:]
			case "12DB":
				approx = "12 dB/oct slope approximated with Q~=0.707"
				rest = rest[1:]
			}
		}
	}

	if (typ == "LSC" || typ == "HSC") && len(rest) >= 2 && strings.EqualFold(rest[1], "DB") {
		approx = "Shelf slope (dB/oct) approximated with Q"
	}

	var freq, gain, q, bw float64
	haveQ, haveBW := false, false
	for i := 0; i < len(rest); i++ {
		switch strings.ToUpper(rest[i]) {
		case "FC":
			if i+1 < len(rest) {
				if v, ok := parseLeadingFloat(rest[i+1]); ok {
					freq = v
				}
				i++
				if i+1 < len(rest) && strings.EqualFold(rest[i+1], "HZ") {
					i++
				}
			}
		case "GAIN":
			if i+1 < len(rest) {
				if v, ok := parseLeadingFloat(rest[i+1]); ok {
					gain = v
				}
				i++
				if i+1 < len(rest) && strings.EqualFold(rest[i+1], "DB") {
					i++
				}
			}
		case "Q":
			if i+1 < len(rest) {
				if v, ok := parseLeadingFloat(rest[i+1]); ok {
					q, haveQ = v, true
				}
				i++
			}
		case "BW":
			if i+2 < len(rest) && strings.EqualFold(rest[i+1], "OCT") {
				if v, ok := parseLeadingFloat(rest[i+2]); ok {
					bw, haveBW = v, true
				}
				i += 2
			}
		}
	}
	if freq <= 0 {
		return ChainItem{}, false, fmt.Errorf("Filter is missing a valid Fc (this line not applied)")
	}

	switch {
	case haveQ:
	case haveBW:
		q = bwToQ(bw)
	default:
		q = 0.7
	}
	it := ChainItem{Type: chainType, Enabled: true, Freq: freq, GainDB: gain, Q: q}
	if approx != "" {
		it.Params = map[string]float64{}
	}
	return it, true, nil
}

func bwToQ(bw float64) float64 {
	if bw <= 0 {
		return 0.7
	}
	return 1.0 / (2.0 * math.Sinh(math.Ln2/2.0*bw))
}

func parseGraphicEQ(arg string) ([]graphicPoint, error) {
	var out []graphicPoint
	for _, seg := range strings.Split(arg, ";") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		f := strings.Fields(seg)
		if len(f) < 2 {
			continue
		}
		freq, err1 := strconv.ParseFloat(f[0], 64)
		gain, err2 := strconv.ParseFloat(f[1], 64)
		if err1 != nil || err2 != nil || freq <= 0 {
			continue
		}
		out = append(out, graphicPoint{Freq: freq, GainDB: gain})
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("GraphicEQ needs at least two valid points (this line not applied)")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Freq < out[j].Freq })
	return out, nil
}

func sampleCurve(points []graphicPoint, freqs []float64) []float64 {
	out := make([]float64, len(freqs))
	if len(points) == 0 {
		return out
	}
	for i, f := range freqs {
		if f < points[0].Freq || f > points[len(points)-1].Freq {
			out[i] = 0
			continue
		}

		j := sort.Search(len(points), func(k int) bool { return points[k].Freq >= f })
		if j == 0 {
			out[i] = points[0].GainDB
			continue
		}
		a, b := points[j-1], points[j]
		if b.Freq == a.Freq {
			out[i] = b.GainDB
			continue
		}
		t := (math.Log(f) - math.Log(a.Freq)) / (math.Log(b.Freq) - math.Log(a.Freq))
		out[i] = a.GainDB + t*(b.GainDB-a.GainDB)
	}
	return out
}

func parseLeadingFloat(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) {
		c := s[end]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0, false
	}
	v, err := strconv.ParseFloat(s[:end], 64)
	return v, err == nil
}

func presetFromAPO(pe *parsedEQ, name string, maxBands int) (Preset, []string, error) {
	warns := append([]string{}, pe.Warnings...)
	chain := pe.Chain

	needFit := false
	var target []float64
	freqs := logGrid(20, 20000, 200)

	switch {
	case len(pe.Graphic) > 0:

		target = sampleCurve(pe.Graphic, freqs)
		if len(chain) > 0 {
			r := evalChainDB(chain, freqs)
			for i := range target {
				target[i] += r[i]
			}
		}
		needFit = true
	case len(chain) > maxBands:
		target = evalChainDB(chain, freqs)
		needFit = true
	}

	if needFit {
		fitted, rms, err := fitBandsToTarget(target, freqs, maxBands)
		if err != nil {
			return Preset{}, warns, err
		}
		if len(pe.Graphic) > 0 {
			warns = append(warns, fmt.Sprintf(
				"GraphicEQ curve (%d points) fitted to %d parametric EQ sections, RMS error %.2f dB",
				len(pe.Graphic), len(fitted), rms))
		}
		if len(chain) > 0 {
			warns = append(warns, fmt.Sprintf(
				"Source file has %d filter sections but hardware applies %d at most: overall response fitted to %d sections, RMS error %.2f dB",
				len(chain), maxBands, len(fitted), rms))
		}
		chain = fitted
	}

	if len(chain) == 0 {
		return Preset{}, warns, fmt.Errorf("Nothing applicable")
	}
	p := Preset{
		Format:   presetFormat,
		Version:  presetFormatVersion,
		Name:     name,
		Note:     "Imported from EqualizerAPO/REW text",
		Target:   "any",
		Requires: chainRequires(chain),
		Chain:    chain,
		PreampDB: pe.PreampDB,
	}
	if err := validatePreset(&p); err != nil {
		return Preset{}, warns, err
	}
	if len(p.Chain) > maxBands {
		return Preset{}, warns, fmt.Errorf("Still %d sections after fitting, exceeding hardware limit of %d sections", len(p.Chain), maxBands)
	}
	return p, warns, nil
}

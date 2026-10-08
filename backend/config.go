// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

type FilterType string

const (
	FilterPeaking   FilterType = "PK"
	FilterLowShelf  FilterType = "LS"
	FilterHighShelf FilterType = "HS"
	FilterLowPass   FilterType = "LP"
	FilterHighPass  FilterType = "HP"
	FilterNotch     FilterType = "NO"
	FilterBandPass  FilterType = "BP"
	FilterAllPass   FilterType = "AP"
	FilterLowPassQ  FilterType = "LPQ"
	FilterHighPassQ FilterType = "HPQ"
)

type EQBand struct {
	Type   FilterType `json:"type"`
	Freq   float64    `json:"freq"`
	GainDB float64    `json:"gain_db"`
	Q      float64    `json:"q"`
}

type EQPresetConfig struct {
	Name   string   `json:"name"`
	Preamp float64  `json:"preamp_db"`
	Bands  []EQBand `json:"bands"`
}

func parseREWConfig(r io.Reader) (*EQPresetConfig, error) {
	config := &EQPresetConfig{Name: "Imported", Preamp: 0.0}
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(strings.ToLower(line), "preamp:") {
			config.Preamp = parseDB(strings.TrimPrefix(line, "Preamp:"))
			continue
		}

		if strings.HasPrefix(line, "Filter") {
			band, err := parseFilterLine(line)
			if err != nil {
				continue
			}
			config.Bands = append(config.Bands, band)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return config, nil
}

func parseFilterLine(line string) (EQBand, error) {
	band := EQBand{}
	upper := strings.ToUpper(line)

	for _, ft := range []FilterType{FilterPeaking, FilterLowShelf, FilterHighShelf,
		FilterLowPass, FilterHighPass, FilterNotch, FilterBandPass, FilterAllPass,
		FilterLowPassQ, FilterHighPassQ} {
		if strings.Contains(upper, " "+string(ft)+" ") {
			band.Type = ft
			break
		}

		if strings.Contains(upper, "\t"+string(ft)+"\t") || strings.Contains(upper, " "+string(ft)+"\t") {
			band.Type = ft
			break
		}
	}
	if band.Type == "" {

		if strings.Contains(upper, " PK ") || strings.Contains(upper, "\tPK\t") {
			band.Type = FilterPeaking
		} else if strings.Contains(upper, " LS ") || strings.Contains(upper, "\tLS\t") {
			band.Type = FilterLowShelf
		} else if strings.Contains(upper, " HS ") || strings.Contains(upper, "\tHS\t") {
			band.Type = FilterHighShelf
		} else {
			return band, fmt.Errorf("unknown filter type in: %s", line)
		}
	}

	band.Freq = extractFloatAfter(line, "Fc", "Hz")
	if band.Freq <= 0 {
		band.Freq = 1000
	}

	band.GainDB = extractFloatAfter(line, "Gain", "dB")
	if band.Type == FilterLowPass || band.Type == FilterHighPass ||
		band.Type == FilterLowPassQ || band.Type == FilterHighPassQ {
		band.GainDB = 0
	}

	band.Q = extractFloatAfter(line, "Q", "")
	if band.Q <= 0 {

		bw := extractFloatAfter(line, "BW Oct", "")
		if bw > 0 {

			band.Q = math.Sqrt(math.Pow(2, bw)) / (math.Pow(2, bw) - 1)
		} else {
			band.Q = 0.707
		}
	}

	return band, nil
}

func extractFloatAfter(line, keyword, unit string) float64 {
	upper := strings.ToUpper(line)
	kw := strings.ToUpper(keyword)
	idx := strings.Index(upper, kw)
	if idx < 0 {
		return 0
	}

	rest := line[idx+len(keyword):]
	rest = strings.TrimSpace(rest)

	var numStr string
	for i, ch := range rest {
		if ch == ' ' || ch == '\t' {
			numStr = rest[:i]
			break
		}
		if i == len(rest)-1 {
			numStr = rest
		}
	}
	if numStr == "" {
		return 0
	}

	val, err := strconv.ParseFloat(strings.TrimSpace(numStr), 64)
	if err != nil {
		return 0
	}
	return val
}

func parseDB(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "dB")
	s = strings.TrimSuffix(s, "db")
	s = strings.TrimSuffix(s, "DB")
	s = strings.TrimSpace(s)
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return val
}

type autoEQPreset struct {
	Name    string         `json:"name"`
	Preamp  float64        `json:"preamp"`
	Filters []autoEQFilter `json:"filters"`
}

type autoEQFilter struct {
	Type string  `json:"type"`
	Freq float64 `json:"freq"`
	Gain float64 `json:"gain"`
	Q    float64 `json:"q"`
}

func parseAutoEQConfig(r io.Reader) (*EQPresetConfig, error) {
	var raw autoEQPreset
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}

	config := &EQPresetConfig{
		Name:   raw.Name,
		Preamp: raw.Preamp,
	}

	for _, f := range raw.Filters {
		band := EQBand{
			Type:   mapAutoEQFilterType(f.Type),
			Freq:   f.Freq,
			GainDB: f.Gain,
			Q:      f.Q,
		}
		if band.Q <= 0 {
			band.Q = 0.707
		}
		config.Bands = append(config.Bands, band)
	}
	return config, nil
}

func mapAutoEQFilterType(t string) FilterType {
	switch strings.ToLower(t) {
	case "peaking", "pk", "parametric":
		return FilterPeaking
	case "lowshelf", "ls", "low_shelf":
		return FilterLowShelf
	case "highshelf", "hs", "high_shelf":
		return FilterHighShelf
	case "lowpass", "lp":
		return FilterLowPass
	case "highpass", "hp":
		return FilterHighPass
	case "notch", "no":
		return FilterNotch
	case "bandpass", "bp":
		return FilterBandPass
	default:
		return FilterPeaking
	}
}

type jsonPreset struct {
	Name   string   `json:"name"`
	Preamp float64  `json:"preamp_db"`
	Bands  []EQBand `json:"bands"`
}

func parseOurJSON(r io.Reader) (*EQPresetConfig, error) {
	var raw jsonPreset
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}
	return &EQPresetConfig{
		Name:   raw.Name,
		Preamp: raw.Preamp,
		Bands:  raw.Bands,
	}, nil
}

const (
	FormatREW     = "rew"
	FormatAutoEQ  = "autoeq"
	FormatJSON    = "json"
	FormatUnknown = "unknown"
)

func detectFormat(data string) string {
	data = strings.TrimSpace(data)

	if strings.HasPrefix(data, "{") || strings.HasPrefix(data, "[") {

		if strings.Contains(data, "\"filters\"") {
			return FormatAutoEQ
		}

		if strings.Contains(data, "\"bands\"") {
			return FormatJSON
		}
		return FormatJSON
	}

	if strings.HasPrefix(data, "Filter") || strings.HasPrefix(data, "Preamp") {
		return FormatREW
	}
	return FormatUnknown
}

func ParseEQConfig(r io.Reader) (*EQPresetConfig, error) {

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	format := detectFormat(string(data))

	switch format {
	case FormatREW:
		return parseREWConfig(strings.NewReader(string(data)))
	case FormatAutoEQ:
		return parseAutoEQConfig(strings.NewReader(string(data)))
	case FormatJSON:
		return parseOurJSON(strings.NewReader(string(data)))
	default:
		return nil, fmt.Errorf("unknown EQ config format")
	}
}

func (cfg *EQPresetConfig) ApplyToDSP() error {
	if !dspAvailable {
		return fmt.Errorf("DSP not available")
	}
	n := len(cfg.Bands)
	if n == 0 {
		return fmt.Errorf("no bands in config")
	}

	if limit := bandLimit(); n > limit {
		n = limit
	}
	for i := 0; i < n; i++ {
		b := cfg.Bands[i]
		if b.Freq <= 0 || b.Q <= 0 {

			if err := dspSetSlot(i, slotConfig{Type: "off"}); err != nil {
				return err
			}
			continue
		}
		sc := slotConfig{
			Type:   string(b.Type),
			Freq:   b.Freq,
			Q:      b.Q,
			GainDB: b.GainDB,
		}
		if err := dspSetSlot(i, sc); err != nil {
			return err
		}
	}

	for i := n; i < bandLimit(); i++ {
		if err := dspSetSlot(i, slotConfig{Type: "off"}); err != nil {
			return err
		}
	}
	if cfg.Preamp != 0 {
		dspSetPreamp(math.Pow(10.0, cfg.Preamp/20.0))
	}
	currentPreset = cfg.Name
	return nil
}

func rbjPeakingEQ(freq, Q, gainDB, fs float64) (int32, int32, int32, int32, int32) {
	A := math.Pow(10.0, gainDB/40.0)
	omega := 2.0 * math.Pi * freq / fs
	sin := math.Sin(omega)
	cos := math.Cos(omega)
	alpha := sin / (2.0 * Q)

	a0 := 1.0 + alpha/A
	a0inv := 1.0 / a0

	b0 := (1.0 + alpha*A) * a0inv
	b1 := (-2.0 * cos) * a0inv
	b2 := (1.0 - alpha*A) * a0inv
	a1 := (-2.0 * cos) * a0inv
	a2 := (1.0 - alpha/A) * a0inv

	return floatToQ315(b0), floatToQ315(b1), floatToQ315(b2),
		floatToQ315(a1), floatToQ315(a2)
}

func rbjLowShelf(freq, Q, gainDB, fs float64) (int32, int32, int32, int32, int32) {
	A := math.Pow(10.0, gainDB/40.0)
	omega := 2.0 * math.Pi * freq / fs
	sin := math.Sin(omega)
	cos := math.Cos(omega)
	beta := math.Sqrt(A) / Q

	a0 := (A + 1.0) + (A-1.0)*cos + beta*sin
	a0inv := 1.0 / a0

	b0 := A * ((A + 1.0) - (A-1.0)*cos + beta*sin) * a0inv
	b1 := 2.0 * A * ((A - 1.0) - (A+1.0)*cos) * a0inv
	b2 := A * ((A + 1.0) - (A-1.0)*cos - beta*sin) * a0inv
	a1 := -2.0 * ((A - 1.0) + (A+1.0)*cos) * a0inv
	a2 := ((A + 1.0) + (A-1.0)*cos - beta*sin) * a0inv

	return floatToQ315(b0), floatToQ315(b1), floatToQ315(b2),
		floatToQ315(a1), floatToQ315(a2)
}

func rbjHighShelf(freq, Q, gainDB, fs float64) (int32, int32, int32, int32, int32) {
	A := math.Pow(10.0, gainDB/40.0)
	omega := 2.0 * math.Pi * freq / fs
	sin := math.Sin(omega)
	cos := math.Cos(omega)
	beta := math.Sqrt(A) / Q

	a0 := (A + 1.0) - (A-1.0)*cos + beta*sin
	a0inv := 1.0 / a0

	b0 := A * ((A + 1.0) + (A-1.0)*cos + beta*sin) * a0inv
	b1 := -2.0 * A * ((A - 1.0) + (A+1.0)*cos) * a0inv
	b2 := A * ((A + 1.0) + (A-1.0)*cos - beta*sin) * a0inv
	a1 := 2.0 * ((A - 1.0) - (A+1.0)*cos) * a0inv

	a2 := ((A + 1.0) - (A-1.0)*cos - beta*sin) * a0inv

	return floatToQ315(b0), floatToQ315(b1), floatToQ315(b2),
		floatToQ315(a1), floatToQ315(a2)
}

func rbjLowPass(freq, Q, fs float64) (int32, int32, int32, int32, int32) {
	b0, b1, b2, a1, a2 := rbjLowPassFloat(freq, Q, fs)
	return floatToQ315(b0), floatToQ315(b1), floatToQ315(b2),
		floatToQ315(a1), floatToQ315(a2)
}

func rbjLowPassFloat(freq, Q, fs float64) (b0, b1, b2, a1, a2 float64) {
	omega := 2.0 * math.Pi * freq / fs
	sin := math.Sin(omega)
	cos := math.Cos(omega)
	alpha := sin / (2.0 * Q)

	a0 := 1.0 + alpha
	a0inv := 1.0 / a0

	b0 = (1.0 - cos) / 2.0 * a0inv
	b1 = (1.0 - cos) * a0inv
	b2 = (1.0 - cos) / 2.0 * a0inv
	a1 = -2.0 * cos * a0inv
	a2 = (1.0 - alpha) * a0inv
	return
}

func rbjHighPass(freq, Q, fs float64) (int32, int32, int32, int32, int32) {
	omega := 2.0 * math.Pi * freq / fs
	sin := math.Sin(omega)
	cos := math.Cos(omega)
	alpha := sin / (2.0 * Q)

	a0 := 1.0 + alpha
	a0inv := 1.0 / a0

	b0 := (1.0 + cos) / 2.0 * a0inv
	b1 := -(1.0 + cos) * a0inv
	b2 := (1.0 + cos) / 2.0 * a0inv
	a1 := -2.0 * cos * a0inv
	a2 := (1.0 - alpha) * a0inv

	return floatToQ315(b0), floatToQ315(b1), floatToQ315(b2),
		floatToQ315(a1), floatToQ315(a2)
}

func rbjNotch(freq, Q, fs float64) (int32, int32, int32, int32, int32) {
	omega := 2.0 * math.Pi * freq / fs
	sin := math.Sin(omega)
	cos := math.Cos(omega)
	alpha := sin / (2.0 * Q)

	a0 := 1.0 + alpha
	a0inv := 1.0 / a0

	b0 := 1.0 * a0inv
	b1 := -2.0 * cos * a0inv
	b2 := 1.0 * a0inv
	a1 := -2.0 * cos * a0inv
	a2 := (1.0 - alpha) * a0inv

	return floatToQ315(b0), floatToQ315(b1), floatToQ315(b2),
		floatToQ315(a1), floatToQ315(a2)
}

func rbjBandPass(freq, Q, fs float64) (int32, int32, int32, int32, int32) {
	omega := 2.0 * math.Pi * freq / fs
	sin := math.Sin(omega)
	cos := math.Cos(omega)
	alpha := sin / (2.0 * Q)

	a0 := 1.0 + alpha
	a0inv := 1.0 / a0

	b0 := (sin / 2.0) * a0inv
	b1 := 0.0 * a0inv
	b2 := (-sin / 2.0) * a0inv
	a1 := -2.0 * cos * a0inv
	a2 := (1.0 - alpha) * a0inv

	return floatToQ315(b0), floatToQ315(b1), floatToQ315(b2),
		floatToQ315(a1), floatToQ315(a2)
}

func rbjAllPass(freq, Q, fs float64) (int32, int32, int32, int32, int32) {
	omega := 2.0 * math.Pi * freq / fs
	sin := math.Sin(omega)
	cos := math.Cos(omega)
	alpha := sin / (2.0 * Q)

	a0 := 1.0 + alpha
	a0inv := 1.0 / a0

	b0 := (1.0 - alpha) * a0inv
	b1 := -2.0 * cos * a0inv
	b2 := (1.0 + alpha) * a0inv
	a1 := -2.0 * cos * a0inv
	a2 := (1.0 - alpha) * a0inv

	return floatToQ315(b0), floatToQ315(b1), floatToQ315(b2),
		floatToQ315(a1), floatToQ315(a2)
}

func (cfg *EQPresetConfig) ExportREW() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# EQ Preset: %s\n", cfg.Name)
	if cfg.Preamp != 0 {
		fmt.Fprintf(&b, "Preamp: %.1f dB\n", cfg.Preamp)
	}
	for i, band := range cfg.Bands {
		fmt.Fprintf(&b, "Filter %2d: ON  %-4s Fc %7.0f Hz  Gain %+6.1f dB  Q %6.3f\n",
			i+1, band.Type, band.Freq, band.GainDB, band.Q)
	}
	return b.String()
}

func (cfg *EQPresetConfig) ExportJSON() (string, error) {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

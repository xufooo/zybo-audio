// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
)

type ChainItem struct {
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`

	Freq   float64            `json:"freq"`
	GainDB float64            `json:"gain_db"`
	Q      float64            `json:"q"`
	Params map[string]float64 `json:"params,omitempty"`

	Name string `json:"name,omitempty"`
}

var chainTypeToSlot = map[string]string{
	"peq": "PK", "pk": "PK", "parametric": "PK", "modal": "PK",
	"lowshelf": "LS", "ls": "LS", "lsc": "LS",
	"highshelf": "HS", "hs": "HS", "hsc": "HS",
	"highpass": "HP", "hp": "HP", "hpq": "HPQ",
	"lowpass": "LP", "lp": "LP", "lpq": "LPQ",
	"notch": "NO", "no": "NO",
	"bandpass": "BP", "bp": "BP",
	"allpass": "AP", "ap": "AP",

	"fir": "FIR", "convolver": "FIR", "ir": "FIR",
}

var chainPlannedType = map[string]string{

	"loudness":   "Loudness (P2)",
	"compressor": "Compression (P2)",
	"limiter":    "true-peak limiting (P2)",

	"delay":  "Delay (P2)",
	"copy":   "Channel matrix (P2)",
	"reverb": "Reverb (P3)",

	"analog": "Tube warmth / even harmonics (P3)",
}

func normalizeChainType(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}

var chainEffectType = map[string]string{
	"crossfeed": "crossfeed", "cure": "crossfeed", "bs2b": "crossfeed",

	"tube": "tube", "tubesimulator": "tube", "tube_simulator": "tube", "6n1j": "tube",
	"surround": "surround", "diffsurround": "surround", "widener": "surround",
	"haas": "surround",

	"fir": "fir", "convolver": "fir", "ir": "fir",

	"ddc": "ddc", "vdc": "ddc",

	"clarity": "clarity", "viperclarity": "clarity", "viper_clarity": "clarity",

	"analogx": "analogx", "viperanalogx": "analogx", "analog_x": "analogx",

	"viperbass": "viperbass", "viper_bass": "viperbass", "bassboost": "viperbass",

	"colorfulmusic": "colorfulmusic", "colorful_music": "colorfulmusic",
	"colorful": "colorfulmusic", "headphone360": "colorfulmusic",

	"speakercorrection": "speaker", "speaker_correction": "speaker",
	"speakercorr": "speaker", "speaker": "speaker",
}

func chainSlotType(t string) (string, bool) {
	st, ok := chainTypeToSlot[normalizeChainType(t)]
	return st, ok
}

var resourceChainKinds = map[string]bool{"fir": true, "ddc": true}

func preserveResourceChainItems(incoming, existing []ChainItem) []ChainItem {
	if len(existing) == 0 {
		return incoming
	}
	covered := map[string]bool{}
	for _, it := range incoming {
		if kind, ok := chainEffectType[normalizeChainType(it.Type)]; ok && resourceChainKinds[kind] {
			covered[kind] = true
		}
	}
	out := append([]ChainItem(nil), incoming...)
	for _, it := range existing {
		kind, ok := chainEffectType[normalizeChainType(it.Type)]
		if !ok || !resourceChainKinds[kind] || covered[kind] {
			continue
		}
		out = append(out, it)
		covered[kind] = true
	}
	return out
}

func chainItemCheck(it ChainItem) error {
	nt := normalizeChainType(it.Type)
	if _, ok := chainTypeToSlot[nt]; ok {
		return nil
	}

	if kind, ok := chainEffectType[nt]; ok {
		switch kind {
		case "crossfeed":
			if !crossfeedAvailable() {
				return fmt.Errorf("Effect %q needs a MIX2 slot (P2 capability: requires 0.2 bitstream)", it.Type)
			}
		case "surround":
			if !surroundAvailable() {
				return fmt.Errorf("Effect %q needs a DELAY slot (P2 capability: requires 0.2 bitstream)", it.Type)
			}
		case "clarity":
			if !clarityAvailable() {
				return fmt.Errorf("Effect %q needs slot-table engine BIQUAD (requires 0.2 bitstream)", it.Type)
			}
		case "speaker":
			if !speakerCorrectionAvailable() {
				return fmt.Errorf("Effect %q needs slot-table engine BIQUAD (requires 0.2 bitstream)", it.Type)
			}
		case "viperbass":
			if !viperBassAvailable() {
				return fmt.Errorf("Effect %q needs a MIX2 slot (P2 capability: requires 0.2 bitstream)", it.Type)
			}
		case "colorfulmusic":
			if !colorfulAvailable() {
				return fmt.Errorf("Effect %q needs a joint-stereo frame pass (CAP1 bit12):"+
					"its DepthSurround is a joint-stereo processor (needs L and R in the same frame); a per-channel engine cannot do it", it.Type)
			}
		}
		return nil
	}
	if stage, ok := chainPlannedType[nt]; ok {
		return fmt.Errorf("Effect %q needs a new engine (%s); current hardware cannot do it", it.Type, stage)
	}
	return fmt.Errorf("Unknown effect type %q", it.Type)
}

func chainToSlots(chain []ChainItem) ([maxBands]slotConfig, int, error) {
	var slots [maxBands]slotConfig

	active := make([]slotConfig, 0, len(chain))
	for i, it := range chain {
		if !it.Enabled {
			continue
		}
		if err := chainItemCheck(it); err != nil {
			return slots, 0, fmt.Errorf("Chain item %d: %w", i+1, err)
		}
		if _, isFx := chainEffectType[normalizeChainType(it.Type)]; isFx {
			continue
		}
		st, _ := chainSlotType(it.Type)
		active = append(active, slotConfig{
			Type:   st,
			Freq:   it.Freq,
			Q:      it.Q,
			GainDB: it.GainDB,
		})
	}

	if limit := bandLimit(); len(active) > limit {

		kept := make([]string, 0, limit)
		for _, sc := range active[:limit] {
			kept = append(kept, fmt.Sprintf("%s@%.0fHz", sc.Type, sc.Freq))
		}
		dropped := make([]string, 0, len(active)-limit)
		for _, sc := range active[limit:] {
			dropped = append(dropped, fmt.Sprintf("%s@%.0fHz", sc.Type, sc.Freq))
		}
		return slots, 0, newCapacityError("bands", len(active), limit,
			"Effect chain has %d sections but current hardware has only %d: first %d sections applied (%s),"+
				"the following %d sections NOT applied (disable one card first, or use fewer sections): %s",
			len(active), limit, limit, strings.Join(kept, ", "),
			len(dropped), strings.Join(dropped, ", "))
	}

	for i := 0; i < maxBands; i++ {
		if i < len(active) {
			slots[i] = sanitizeBand(active[i])
			continue
		}
		slots[i] = slotConfig{Type: "off"}
	}
	return slots, len(active), nil
}

func chainRequires(chain []ChainItem) []string {
	set := map[string]bool{}
	for _, it := range chain {
		nt := normalizeChainType(it.Type)
		if nt == "" {
			continue
		}
		set[nt] = true
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func chainUnsupported(chain []ChainItem) map[string]string {
	out := map[string]string{}
	for _, it := range chain {
		if err := chainItemCheck(it); err != nil {
			out[normalizeChainType(it.Type)] = err.Error()
		}
	}
	return out
}

func chainFromSlots() []ChainItem {
	out := make([]ChainItem, 0, maxBands)
	for i := 0; i < maxBands; i++ {
		sc := currentSlots[i]
		if isBandOff(sc.Type) {
			continue
		}
		it := ChainItem{
			Type:    strings.ToLower(sc.Type),
			Enabled: true,
			Freq:    sc.Freq,
			GainDB:  sc.GainDB,
			Q:       sc.Q,
		}

		switch sc.Type {
		case "PK":
			it.Type = "peq"
		case "LS":
			it.Type = "lowshelf"
		case "HS":
			it.Type = "highshelf"
		case "HP", "HPQ":
			it.Type = "highpass"
		case "LP", "LPQ":
			it.Type = "lowpass"
		case "NO":
			it.Type = "notch"
		case "BP":
			it.Type = "bandpass"
		case "AP":
			it.Type = "allpass"
		}
		out = append(out, it)
	}
	return out
}

type chainStateSnapshot struct {
	dyn     *dynParams
	dynbass *dynamicBassParams
	cross   *crossfeedParams
	sur     *surroundParams
	exc     *exciterParams
	bass    *viperBassParams
	tube    *tubeParams
	col     *colorfulParams
	clarity *clarityParams
	spk     *speakerCorrectionParams
	analogx *analogxParams
	chain   []ChainItem
	slots   [maxBands]slotConfig
	preamp  float64
	gain    float64

	typeName string
}

func captureChainState() chainStateSnapshot {
	return chainStateSnapshot{
		dyn: currentDynBass, dynbass: currentDynamicBass,
		cross: currentCrossfeed, sur: currentSurround,
		exc: currentExciter, bass: currentViPERBass, tube: currentTube, col: currentColorful,
		clarity: currentClarity, spk: currentSpeakerCorrection, analogx: currentAnalogX,
		chain: currentUserChain, slots: currentSlots,
		preamp: currentPreampDB, gain: chainGainDB,
		typeName: selectedTypeName,
	}
}

func (s chainStateSnapshot) restore() {
	currentDynBass, currentDynamicBass = s.dyn, s.dynbass
	currentCrossfeed, currentSurround = s.cross, s.sur
	currentExciter, currentTube = s.exc, s.tube
	currentViPERBass = s.bass
	currentColorful = s.col
	currentClarity = s.clarity
	currentSpeakerCorrection = s.spk
	currentAnalogX = s.analogx
	currentUserChain, currentSlots = s.chain, s.slots
	currentPreampDB, chainGainDB = s.preamp, s.gain
	selectedTypeName = s.typeName
}

func applyChain(chain []ChainItem, userPreampDB, userGainDB float64) error {
	snap := captureChainState()
	err := applyChainInner(chain, userPreampDB, userGainDB)
	if err != nil {
		snap.restore()
		log.Printf("Chain deploy failed, in-memory state rolled back (hardware keeps old chain): %v", err)
	}
	return err
}

func applyChainInner(chain []ChainItem, userPreampDB, userGainDB float64) error {

	applyChainEffects(chain)

	if currentFIR != nil && currentFIR.Name != "" && currentFIR.Name != irLoadedName() {
		if err := loadIRNamed(currentFIR.Name); err != nil {
			return err
		}
	}

	currentUserChain = chain
	slots, _, err := chainToSlots(withLoudness(chain))
	if err != nil {
		return err
	}
	currentSlots = slots
	currentPreampDB = userPreampDB
	if userGainDB < 0 {
		userGainDB = 0
	}
	if userGainDB > 12 {
		userGainDB = 12
	}
	chainGainDB = userGainDB
	if err := dspWriteAllBands(); err != nil {
		return err
	}

	markStateDirty()
	return nil
}

func applyChainEffects(chain []ChainItem) {
	for _, it := range chain {
		kind, ok := chainEffectType[normalizeChainType(it.Type)]
		if !ok {
			continue
		}
		if !it.Enabled {
			switch kind {
			case "crossfeed":
				currentCrossfeed = nil
			case "surround":
				currentSurround = nil
			case "fir":
				currentFIR = nil
			case "ddc":
				ddcClear()
			case "tube":
				currentTube = nil
			case "clarity":
				currentClarity = nil
			case "speaker":
				currentSpeakerCorrection = nil
			case "analogx":
				currentAnalogX = nil
			case "viperbass":
				currentViPERBass = nil
			case "colorfulmusic":
				currentColorful = nil
			}
			continue
		}
		switch kind {
		case "crossfeed":
			if currentCrossfeed == nil && crossfeedAvailable() {
				p := crossfeedDefaultParams()
				currentCrossfeed = &p
			}
		case "viperbass":

			if currentViPERBass == nil && viperBassAvailable() {
				p := viperBassDefaultParams()
				if v, ok := it.Params["mode"]; ok {
					p.Mode = int(v)
				}
				if v, ok := it.Params["cutoff_hz"]; ok {
					p.CutoffHz = v
				} else if v, ok := it.Params["freq"]; ok {
					p.CutoffHz = v
				}
				if v, ok := it.Params["gain"]; ok {
					p.Gain = v
				}
				if err := setViPERBass(&p); err != nil {
					log.Printf("Invalid ViPERBass in chain item (%v), skipping this stage", err)
				}
			}
		case "colorfulmusic":

			if currentColorful == nil && colorfulAvailable() {
				p := colorfulDefaultParams()
				if err := setColorfulMusic(&p); err != nil {
					log.Printf("Invalid ColorfulMusic in chain item (%v), skipping this stage", err)
				}
			}
		case "tube":
			if currentTube == nil && tubeAvailable() {
				currentTube = &tubeParams{}
			}
		case "analogx":
			if currentAnalogX == nil && analogxAvailable() {
				p := analogxDefaultParams()
				if v, ok := it.Params["model"]; ok {
					p.Model = int(v)
				}
				if err := setAnalogX(&p); err != nil {
					log.Printf("Invalid AnalogX in chain item (%v), skipping this stage", err)
				}
			}
		case "clarity":

			if currentClarity == nil && clarityAvailable() {
				p := clarityDefaultParams()
				if m, ok := clarityModeFromString(it.Type); ok {
					p.Mode = m
				}
				if v, ok := it.Params["mode"]; ok {
					p.Mode = int(v)
				}
				if v, ok := it.Params["level"]; ok {
					p.Level = v
				}
				if err := setClarity(&p); err != nil {
					log.Printf("Invalid Clarity in chain item (%v), skipping this stage", err)
				}
			}
		case "speaker":

			if currentSpeakerCorrection == nil && speakerCorrectionAvailable() {
				currentSpeakerCorrection = &speakerCorrectionParams{}
			}
		case "fir":

			if firAvailable() {
				p := firParams{Taps: firTaps}
				if v, ok := it.Params["taps"]; ok && v > 0 {
					p.Taps = int(v)
				}
				if nm := it.Name; nm != "" {

					p.Name = nm
				}
				currentFIR = &p
			}
		case "ddc":

			if it.Name != "" {

				sec := int(chainItemParam("ddc", "sections", 4))

				nat := chainItemParam("ddc", "native", 0) != 0

				if sec < 1 || sec > hwMaxSections {
					sec = 4
				}
				if err := loadDDCNamed(it.Name, sec, nat); err != nil {
					log.Printf("DDC %q read-back failed: %v", it.Name, err)
				}
			}
		case "surround":
			if currentSurround == nil && surroundAvailable() {
				p := surroundDefaultParams()

				if v, ok := it.Params["delay_ms"]; ok && v > 0 {
					p.DelayMs = v
				}
				currentSurround = &p
			}
		}
	}
}

// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
)

func handleDSPCapabilities(w http.ResponseWriter, r *http.Request) {
	types := make([]string, 0, len(chainTypeToSlot))
	seen := map[string]bool{}
	for _, slotType := range chainTypeToSlot {

		name := canonicalChainType(slotType)
		if !seen[name] {
			seen[name] = true
			types = append(types, name)
		}
	}
	sort.Strings(types)
	planned := map[string]string{}
	for t, why := range chainPlannedType {
		planned[t] = why
	}

	engineName := "biquad-hardwired"

	hwCaps := dspEngineCaps{}
	if dspEngineGen == 1 {
		engineName = "slot-table"
		hwCaps = dspReadEngineCaps()
	}
	caps := map[string]any{
		"engine":        engineName,
		"engine_gen":    dspEngineGen,
		"bands":         bandLimit(),
		"types":         types,
		"planned":       planned,
		"min_freq_hz":   minBandFreq,
		"max_freq_hz":   maxBandFreq,
		"sample_rate":   int(sampleRate),
		"dsp_available": dspAvailable,
		"limiter": map[string]any{
			"available": true,
			"bypass":    !dspLimiterEnabled,

			"mode": limiterModeName(),

			"truepeak_available": hwCaps.TruepeakAvailable(),

			"att_ms":     limMsFromKTP(limKFromMsTP(dspLimiterAttMs)),
			"rel_ms":     limMsFromKTP(limKFromMsTP(dspLimiterRelMs)),
			"rel_ms_max": limMaxMsTP,
			"note":       "feedback level has no lookahead; 0 dBFS material produces waveform discontinuities (about 1770 per second); truepeak level uses 2 ms lookahead + log-domain smoothing, measured count is 0",
		},
	}

	db := map[string]any{"available": dynBassAvailable(), "on": currentDynBass != nil}
	if cur := currentDynBass; cur != nil {
		db["gain_db"] = cur.GainDB
		db["cut_db"] = cur.CutDB
		db["ref_db"] = cur.RefDB
		db["ks"] = cur.KS
		db["att_ms"] = cur.AttMs
		db["rel_ms"] = cur.RelMs
	}
	caps["dyn_bass"] = db

	caps["crossfeed"] = map[string]any{
		"available": crossfeedAvailable(),
		"on":        currentCrossfeed != nil,
		"view":      crossfeedView(),

		"tiers": crossfeedTiersView(),
	}

	caps["fir"] = map[string]any{
		"available": firAvailable(),
		"taps":      firTaps,
		"coef_base": hwCoefFIRBase,
	}

	caps["tube"] = map[string]any{
		"available": tubeAvailable(),
		"on":        currentTube != nil,
		"view":      tubeView(),
	}

	caps["clarity"] = map[string]any{
		"available":   clarityAvailable(),
		"on":          currentClarity != nil,
		"view":        clarityView(),
		"modes":       []string{"natural", "ozone", "xhifi"},
		"level_range": []float64{0, 100},
		"xhifi_slots": 2,
		"delay_slots": hwDelaySlots,
		"unsupported": []string{},
	}

	caps["analogx"] = map[string]any{
		"available": analogxAvailable(),
		"on":        currentAnalogX != nil,
		"view":      analogxView(),
		"tiers":     analogxTiersView(),
	}

	caps["speaker_correction"] = map[string]any{
		"available": speakerCorrectionAvailable(),
		"on":        currentSpeakerCorrection != nil,
		"view":      speakerCorrectionView(),
		"sections":  3,
	}

	caps["fx_presets"] = fxPresetsView()

	caps["exciter"] = map[string]any{
		"available": exciterAvailable(),
		"on":        currentExciter != nil,
		"view":      exciterView(),
	}

	caps["vse"] = map[string]any{
		"available": vseAvailable(),
		"on":        vseView() != nil,
		"view":      vseView(),

		"gears": vseGearTable(),
	}

	caps["viperbass"] = map[string]any{
		"available":     viperBassAvailable(),
		"pbp_available": sfirAvailableNow(),
		"on":            currentViPERBass != nil,
		"view":          viperBassView(),
		"modes":         []string{"natural", "pure_bass_plus", "subwoofer"},

		"cutoff_hz": []float64{30, 40, 50, 60, 66, 78, 80, 100},
		"gain":      []float64{50, 100, 150, 200, 250, 300, 350, 400, 450, 500, 550, 600},
	}

	caps["surround"] = map[string]any{
		"available": surroundAvailable(),
		"on":        currentSurround != nil,
		"view":      surroundView(),
	}

	caps["colorfulmusic"] = map[string]any{
		"available": colorfulAvailable(),
		"on":        currentColorful != nil,
		"view":      colorfulView(),

		"client_widening_range": []float64{120, 200},
		"client_depth_range":    []float64{200, 800},
		"client_midimage_range": []float64{100, 200},
	}
	if dspEngineGen == 1 {
		caps["hardware"] = map[string]any{
			"version":      hwCaps.Version,
			"opcodes":      hwCaps.Opcodes,
			"num_slots":    hwCaps.NumSlots,
			"fir_max_log2": hwCaps.FirMaxLog2,
			"delay_log2":   hwCaps.DelayLog2,

			"delay_slots":  hwDelaySlots,
			"max_sections": hwMaxSections,

			"headroom_available": hwCaps.HeadroomAvailable(),
			"headroom_on":        dspHeadroomOn,
			"headroom_db":        headroomDB,
			"status":             dspReadStatus(),

			"overrun_count": (dspReadStatus() >> 24) & 0xFF,
		}
	}
	if lib, err := loadPresetLibrary(); err == nil {
		user, _ := loadUserPresets()
		caps["presets"] = map[string]int{"builtin": len(lib), "user": len(user)}
	} else {
		caps["presets"] = map[string]int{"builtin": 0, "user": 0}
		caps["preset_library_error"] = err.Error()
	}
	writeJSON(w, caps)
}

func canonicalChainType(slotType string) string {
	switch slotType {
	case "PK":
		return "peq"
	case "LS":
		return "lowshelf"
	case "HS":
		return "highshelf"
	case "HP", "HPQ":
		return "highpass"
	case "LP", "LPQ":
		return "lowpass"
	case "NO":
		return "notch"
	case "BP":
		return "bandpass"
	case "AP":
		return "allpass"
	}
	return strings.ToLower(slotType)
}

type chainRequest struct {
	Chain    []ChainItem `json:"chain"`
	PreampDB float64     `json:"preamp_db"`

	GainDB *float64 `json:"gain_db"`

	DynBass *dynBassRequest `json:"dyn_bass"`

	DynamicBass *dynamicBassRequest `json:"dynamic_bass"`

	Type string `json:"type"`

	Surround *surroundRequest `json:"surround"`

	Crossfeed *crossfeedRequest `json:"crossfeed"`

	Exciter *exciterRequest `json:"exciter"`

	Tube *tubeRequest `json:"tube"`

	Clarity *clarityRequest `json:"clarity"`

	SpeakerCorrection *speakerCorrectionRequest `json:"speaker_correction"`

	AnalogX *analogxRequest `json:"analogx"`

	VSE *vseRequest `json:"vse"`

	ViPERBass *viperBassRequest `json:"viperbass"`

	ColorfulMusic *colorfulRequest `json:"colorfulmusic"`

	FXPreset *string `json:"fx_preset"`
}

type vseRequest struct {
	Gear *float64 `json:"gear"`
	Off  *bool    `json:"off"`
}

type viperBassRequest struct {
	Mode     *int     `json:"mode"`
	CutoffHz *float64 `json:"cutoff_hz"`
	Gain     *float64 `json:"gain"`
	Off      *bool    `json:"off"`
}

type analogxRequest struct {
	Model *int  `json:"model"`
	Off   *bool `json:"off"`
}

type clarityRequest struct {
	Mode  string   `json:"mode"`
	Level *float64 `json:"level"`
	Off   *bool    `json:"off"`
}

type speakerCorrectionRequest struct {
	Off *bool `json:"off"`
}

type exciterRequest struct {
	Harmonics []float64 `json:"harmonics"`
	Mix       *float64  `json:"mix"`
	HPFHz     *float64  `json:"hpf_hz"`
	LPFHz     *float64  `json:"lpf_hz"`
	Off       *bool     `json:"off"`
}

type colorfulRequest struct {
	Depth    *int     `json:"depth"`
	Widening *float64 `json:"widening"`
	MidImage *float64 `json:"mid_image"`
	Off      *bool    `json:"off"`
}

type surroundRequest struct {
	DelayMs *float64 `json:"delay_ms"`
	Off     *bool    `json:"off"`
}

type crossfeedRequest struct {
	FcutHz *float64 `json:"fcut_hz"`
	Feed   *float64 `json:"feed"`

	PassFilter *bool `json:"pass_filter"`
	Off        *bool `json:"off"`
}

type tubeRequest struct {
	Off *bool `json:"off"`
}

type dynamicBassRequest struct {
	Coeffs *string  `json:"coeffs"`
	Bass   *float64 `json:"bass"`
	Off    *bool    `json:"off"`
}

type dynBassRequest struct {
	GainDB *float64 `json:"gain_db"`
	CutDB  *float64 `json:"cut_db"`
	RefDB  *float64 `json:"ref_db"`
	KS     *float64 `json:"ks"`
	AttMs  *float64 `json:"att_ms"`
	RelMs  *float64 `json:"rel_ms"`
	Off    *bool    `json:"off"`
}

func handleDSPChain(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"chain":     chainFromSlots(),
			"type":      selectedTypeName,
			"preamp_db": currentPreampAppliedDB,

			"gain_db":            chainGainDB,
			"bands":              bandLimit(),
			"dsp_available":      dspAvailable,
			"unsupported":        chainUnsupported(chainFromSlots()),
			"dyn_bass":           dynBassView(),
			"dynamic_bass":       dynamicBassView(),
			"crossfeed":          crossfeedView(),
			"surround":           surroundView(),
			"colorfulmusic":      colorfulView(),
			"exciter":            exciterView(),
			"vse":                vseView(),
			"viperbass":          viperBassView(),
			"tube":               tubeView(),
			"clarity":            clarityView(),
			"speaker_correction": speakerCorrectionView(),
			"analogx":            analogxView(),

			"limiter":  limiterView(),
			"loudness": loudnessView(),

			"fx_preset": fxPresetCurrent(),
		})
	case http.MethodPut:
		var req chainRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "request body is not valid JSON: "+err.Error())
			return
		}
		if len(req.Chain) == 0 {

			if req.Chain == nil {
				httpError(w, http.StatusBadRequest, "missing chain field (pass [] explicitly for an empty chain)")
				return
			}
		}

		reqSnap := captureChainState()

		ok := false
		defer func() {
			if !ok {
				reqSnap.restore()
			}
		}()

		if req.Type != "" {
			selectedTypeName = req.Type
		}

		if req.FXPreset != nil {
			if err := applyFXPreset(*req.FXPreset); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if req.DynBass != nil {

			p := dynDefaultParams()
			if cur := currentDynBass; cur != nil {
				p = *cur
			}
			r := req.DynBass
			if r.GainDB != nil {
				p.GainDB = *r.GainDB
			}
			if r.CutDB != nil {
				p.CutDB = *r.CutDB
			}
			if r.RefDB != nil {
				p.RefDB = *r.RefDB
			}
			if r.KS != nil {
				p.KS = *r.KS
			}
			if r.AttMs != nil {
				p.AttMs = *r.AttMs
			}
			if r.RelMs != nil {
				p.RelMs = *r.RelMs
			}
			if r.Off != nil && *r.Off {
				if err := setDynBass(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else if err := setDynBass(&p); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if req.DynamicBass != nil {
			p := dynamicBassDefaultParams()
			if cur := currentDynamicBass; cur != nil {
				p = *cur
			}
			r := req.DynamicBass
			if r.Coeffs != nil {
				p.Coeffs = *r.Coeffs
			}
			if r.Bass != nil {
				p.Bass = *r.Bass
			}
			if r.Off != nil && *r.Off {
				if err := setDynamicBass(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else if err := setDynamicBass(&p); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if req.Surround != nil {
			p := surroundDefaultParams()
			if cur := currentSurround; cur != nil {
				p = *cur
			}
			r := req.Surround
			if r.DelayMs != nil {
				p.DelayMs = *r.DelayMs
			}
			if r.Off != nil && *r.Off {
				if err := setSurround(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else if err := setSurround(&p); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if req.ColorfulMusic != nil {
			p := colorfulDefaultParams()
			if cur := currentColorful; cur != nil {
				p = *cur
			}
			r := req.ColorfulMusic
			if r.Depth != nil {
				p.Depth = *r.Depth
			}
			if r.Widening != nil {
				p.Widening = *r.Widening
			}
			if r.MidImage != nil {
				p.MidImage = *r.MidImage
			}
			if (r.Off != nil && *r.Off) || p.Depth == 0 {
				if err := setColorfulMusic(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else if err := setColorfulMusic(&p); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if req.Crossfeed != nil {
			p := crossfeedDefaultParams()
			if cur := currentCrossfeed; cur != nil {
				p = *cur
			}
			r := req.Crossfeed
			if r.FcutHz != nil {
				p.FcutHz = *r.FcutHz
			}
			if r.Feed != nil {
				p.Feed = *r.Feed
			}

			if r.PassFilter != nil {
				p.PassFilter = *r.PassFilter
			}
			if r.Off != nil && *r.Off {
				if err := setCrossfeed(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else if err := setCrossfeed(&p); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if req.Tube != nil {
			if req.Tube.Off != nil && *req.Tube.Off {
				if err := setTube(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else if err := setTube(&tubeParams{}); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if req.Exciter != nil {
			p := exciterDefaultParams()
			if cur := currentExciter; cur != nil {
				p = *cur
			}
			r := req.Exciter
			if len(r.Harmonics) > 0 {
				if len(r.Harmonics) != 10 {
					httpError(w, http.StatusBadRequest, "harmonics must be 10 numbers (harmonic 1..10 amplitudes)")
					return
				}
				copy(p.Harmonics[:], r.Harmonics)
			}
			if r.Mix != nil {
				p.Mix = *r.Mix
			}
			if r.HPFHz != nil {
				p.HPFHz = *r.HPFHz
			}
			if r.LPFHz != nil {
				p.LPFHz = *r.LPFHz
			}
			if r.Off != nil && *r.Off {
				if err := setExciter(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else if err := setExciter(&p); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if req.VSE != nil {
			if req.VSE.Off != nil && *req.VSE.Off {
				if err := setVSE(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else {
				g := vseGearMin
				if req.VSE.Gear != nil {
					g = *req.VSE.Gear
				}
				if err := setVSE(&g); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			}
		}

		if req.ViPERBass != nil {
			if req.ViPERBass.Off != nil && *req.ViPERBass.Off {
				if err := setViPERBass(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else {
				p := viperBassDefaultParams()
				if cur := currentViPERBass; cur != nil {
					p = *cur
				}
				if req.ViPERBass.Mode != nil {
					p.Mode = *req.ViPERBass.Mode
				}
				if req.ViPERBass.CutoffHz != nil {
					p.CutoffHz = *req.ViPERBass.CutoffHz
				}
				if req.ViPERBass.Gain != nil {
					p.Gain = *req.ViPERBass.Gain
				}
				if err := setViPERBass(&p); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			}
		}

		if req.Clarity != nil {
			if req.Clarity.Off != nil && *req.Clarity.Off {
				if err := setClarity(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else {
				p := clarityDefaultParams()
				if cur := currentClarity; cur != nil {
					p = *cur
				}
				if req.Clarity.Mode != "" {
					m, ok := clarityModeFromString(req.Clarity.Mode)
					if !ok {
						httpError(w, http.StatusBadRequest,
							"clarity.mode only accepts natural / ozone / xhifi")
						return
					}
					p.Mode = m
				}
				if req.Clarity.Level != nil {
					p.Level = *req.Clarity.Level
				}
				if err := setClarity(&p); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			}
		}

		if req.SpeakerCorrection != nil {
			if req.SpeakerCorrection.Off != nil && *req.SpeakerCorrection.Off {
				if err := setSpeakerCorrection(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else if err := setSpeakerCorrection(&speakerCorrectionParams{}); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}

		if req.AnalogX != nil {
			if req.AnalogX.Off != nil && *req.AnalogX.Off {
				if err := setAnalogX(nil); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			} else {
				p := analogxDefaultParams()
				if cur := currentAnalogX; cur != nil {
					p = *cur
				}
				if req.AnalogX.Model != nil {
					p.Model = *req.AnalogX.Model
				}
				if err := setAnalogX(&p); err != nil {
					httpError(w, http.StatusBadRequest, err.Error())
					return
				}
			}
		}

		effChain := preserveResourceChainItems(req.Chain, currentUserChain)
		_, active, err := chainToSlots(effChain)
		if err != nil {

			httpErrorCapacity(w, err, http.StatusBadRequest, "")
			return
		}

		gainDB := chainGainDB
		if req.GainDB != nil {
			gainDB = *req.GainDB
		}
		if err := applyChain(effChain, req.PreampDB, gainDB); err != nil {

			log.Printf("chain download failed, effect state changed by this request has been rolled back: %v", err)
			httpErrorCapacity(w, err, http.StatusInternalServerError, "failed to write hardware: ")
			return
		}
		ok = true
		writeJSON(w, map[string]any{
			"ok":                 true,
			"applied":            active,
			"preamp_db":          currentPreampAppliedDB,
			"dyn_bass":           dynBassView(),
			"dynamic_bass":       dynamicBassView(),
			"crossfeed":          crossfeedView(),
			"surround":           surroundView(),
			"colorfulmusic":      colorfulView(),
			"exciter":            exciterView(),
			"tube":               tubeView(),
			"clarity":            clarityView(),
			"speaker_correction": speakerCorrectionView(),
			"analogx":            analogxView(),
		})
	default:
		httpError(w, http.StatusMethodNotAllowed, "GET / PUT only")
	}
}

type presetView struct {
	Name     string   `json:"name"`
	Note     string   `json:"note,omitempty"`
	Target   string   `json:"target,omitempty"`
	Stage    string   `json:"stage,omitempty"`
	Requires []string `json:"requires,omitempty"`
	Builtin  bool     `json:"builtin"`
	Bands    int      `json:"bands"`
	Usable   bool     `json:"usable"`
	Why      []string `json:"why,omitempty"`

	Chain []ChainItem `json:"chain,omitempty"`
}

func presetToView(p Preset) presetView {
	v := presetView{
		Name:     p.Name,
		Note:     p.Note,
		Target:   p.Target,
		Stage:    p.Stage,
		Requires: p.Requires,
		Builtin:  p.Builtin,
		Bands:    len(p.Chain),
		Why:      presetUnsupported(p),
		Chain:    p.Chain,
	}
	v.Usable = len(v.Why) == 0
	return v
}

func handleDSPPresets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		lib, err := loadPresetLibrary()
		if err != nil {
			httpError(w, http.StatusInternalServerError, "failed to read built-in library: "+err.Error())
			return
		}
		user, _ := loadUserPresets()
		views := make([]presetView, 0, len(lib)+len(user))
		for _, p := range lib {
			views = append(views, presetToView(p))
		}
		for _, p := range user {
			views = append(views, presetToView(p))
		}
		writeJSON(w, map[string]any{"presets": views, "band_limit": bandLimit()})
	case http.MethodPost:

		var body struct {
			Name string `json:"name"`
			Note string `json:"note"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			httpError(w, http.StatusBadRequest, "request body is not valid JSON: "+err.Error())
			return
		}
		chain := chainFromSlots()
		if len(chain) == 0 {
			httpError(w, http.StatusBadRequest, "current chain is empty (all flat), nothing to save")
			return
		}
		p := Preset{
			Format:   presetFormat,
			Version:  presetFormatVersion,
			Name:     body.Name,
			Note:     body.Note,
			Target:   "any",
			Requires: chainRequires(chain),
			Chain:    chain,
			PreampDB: currentPreampDB,
		}
		if err := saveUserPreset(p); err != nil {
			httpError(w, http.StatusBadRequest, "save failed: "+err.Error())
			return
		}
		log.Printf("preset: saved user preset %q (%d sections)", p.Name, len(p.Chain))
		writeJSON(w, map[string]any{"ok": true, "name": p.Name})
	default:
		httpError(w, http.StatusMethodNotAllowed, "GET / POST only")
	}
}

func handleDSPPresetApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var body struct{ Name string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "request body is not valid JSON: "+err.Error())
		return
	}
	p, err := findPreset(body.Name)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if why := presetUnsupported(p); len(why) > 0 {
		httpError(w, http.StatusConflict,
			fmt.Sprintf("preset %q needs a more capable engine, this machine cannot do it yet: %s", p.Name, strings.Join(why, "; ")))
		return
	}
	if err := applyPreset(p); err != nil {
		httpError(w, http.StatusInternalServerError, "apply failed: "+err.Error())
		return
	}
	log.Printf("preset: applied %q (%d sections, preamp %.1f dB)", p.Name, len(p.Chain), currentPreampAppliedDB)
	writeJSON(w, map[string]any{"ok": true, "name": p.Name, "preamp_db": currentPreampAppliedDB})
}

func handleDSPPresetDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var body struct{ Name string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "request body is not valid JSON: "+err.Error())
		return
	}
	if err := deleteUserPreset(body.Name); err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleDSPPresetRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "request body is not valid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(body.From) == "" {
		httpError(w, http.StatusBadRequest, "missing from (which entry to rename)")
		return
	}
	to, err := renameUserPreset(body.From, body.To)
	if err != nil {

		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "built-in preset") {
			httpError(w, http.StatusNotFound, err.Error())
			return
		}
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	log.Printf("preset: renamed user preset %q -> %q", body.From, to)
	writeJSON(w, map[string]any{"ok": true, "name": to})
}

func handleDSPPresetExport(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		httpError(w, http.StatusBadRequest, "missing name parameter")
		return
	}
	p, err := findPreset(name)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	p.Builtin = false
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\"preset.json\"")
	w.Write(append(data, '\n'))
}

func handleDSPPresetImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		httpError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		httpError(w, http.StatusBadRequest, "empty request body")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))

	if strings.HasPrefix(text, "{") {
		var single Preset
		if err := json.Unmarshal(raw, &single); err == nil && single.Name != "" {
			if err := validatePreset(&single); err != nil {
				httpError(w, http.StatusBadRequest, "invalid preset: "+err.Error())
				return
			}
			if name != "" {
				single.Name = name
				if err := validatePreset(&single); err != nil {
					httpError(w, http.StatusBadRequest, "invalid preset: "+err.Error())
					return
				}
			}
			if why := presetUnsupported(single); len(why) > 0 {
				writeJSON(w, map[string]any{
					"ok":       true,
					"preset":   presetToView(single),
					"warnings": []string{"this preset uses effects not available on this machine: " + strings.Join(why, "; ")},
				})
				return
			}
			applied, applyErr := importAndApply(single)
			resp := map[string]any{"ok": true, "preset": presetToView(single), "warnings": []string{}, "applied": applied}
			if applyErr != "" {
				resp["apply_error"] = applyErr
			}
			writeJSON(w, resp)
			return
		}
	}

	pe, perr := parseEqualizerAPO(text)
	if perr != nil && pe == nil {
		httpError(w, http.StatusBadRequest, perr.Error())
		return
	}
	if name == "" {
		name = "imported config"
	}

	p, warns, err := presetFromAPO(pe, name, bandLimit())
	if perr != nil {
		warns = append(warns, perr.Error())
	}
	if err != nil {
		httpError(w, http.StatusBadRequest, "import failed: "+err.Error())
		return
	}
	applied, applyErr := importAndApply(p)
	log.Printf("preset: imported %q (%d sections, %d warnings, applied=%v)", p.Name, len(p.Chain), len(warns), applied)
	resp := map[string]any{
		"ok":        true,
		"preset":    presetToView(p),
		"warnings":  warns,
		"applied":   applied,
		"preamp_db": currentPreampAppliedDB,
	}
	if applyErr != "" {
		resp["apply_error"] = applyErr
	}
	writeJSON(w, resp)
}

func importAndApply(p Preset) (bool, string) {
	if err := saveUserPreset(p); err != nil {
		log.Printf("preset: failed to save import result: %v", err)
	}
	if err := applyPreset(p); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("failed to write response: %v", err)
	}
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func httpErrorCapacity(w http.ResponseWriter, err error, fallbackCode int, fallbackPrefix string) {
	var ce *capacityError
	if errors.As(err, &ce) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error":  ce.Error(),
			"code":   "capacity",
			"what":   ce.What,
			"needed": ce.Needed,
			"limit":  ce.Limit,
		})
		return
	}
	msg := err.Error()
	if fallbackPrefix != "" {
		msg = fallbackPrefix + msg
	}
	httpError(w, fallbackCode, msg)
}

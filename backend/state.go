// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const stateFormat = 1

var statePath = envOr("ZYBO_STATE_FILE", "/var/lib/zybo-audio/state.json")

type runtimeState struct {
	Format   int         `json:"format"`
	Version  string      `json:"version,omitempty"`
	SavedAt  string      `json:"saved_at,omitempty"`
	Type     string      `json:"type,omitempty"`
	Chain    []ChainItem `json:"chain"`
	PreampDB float64     `json:"preamp_db"`
	GainDB   float64     `json:"gain_db"`

	Bypass    bool             `json:"bypass"`
	DynBass   *dynParams       `json:"dyn_bass,omitempty"`
	Crossfeed *crossfeedParams `json:"crossfeed,omitempty"`
	Surround  *surroundParams  `json:"surround,omitempty"`
	Exciter   *exciterParams   `json:"exciter,omitempty"`

	ViPERBass *viperBassParams `json:"viperbass,omitempty"`

	Tube              *tubeParams              `json:"tube,omitempty"`
	Clarity           *clarityParams           `json:"clarity,omitempty"`
	SpeakerCorrection *speakerCorrectionParams `json:"speaker_correction,omitempty"`

	AnalogX *analogxParams `json:"analogx,omitempty"`

	DynamicBass *dynamicBassParams `json:"dynamic_bass,omitempty"`

	ColorfulMusic *colorfulParams `json:"colorfulmusic,omitempty"`
	Loudness      loudnessPersist `json:"loudness"`
	Limiter       limiterPersist  `json:"limiter"`
	Headroom      bool            `json:"headroom"`

	Volume int `json:"volume,omitempty"`
}

type loudnessPersist struct {
	On        bool    `json:"on"`
	Strength  float64 `json:"strength"`
	RefOffset float64 `json:"ref_offset_db"`
}

type limiterPersist struct {
	Enabled  bool    `json:"enabled"`
	TruePeak bool    `json:"truepeak"`
	ThrDB    float64 `json:"thr_db"`
	AttMs    float64 `json:"att_ms"`
	RelMs    float64 `json:"rel_ms"`
}

var (
	stateMu        sync.Mutex
	stateDirty     bool
	stateTimer     *time.Timer
	statePending   []byte
	stateRestoring bool

	statePathMu sync.RWMutex

	stateBootRestoreDone bool

	stateDebounce = 2 * time.Second
)

var selectedTypeName string

func snapshotRuntimeState() runtimeState {
	st := runtimeState{
		Format:   stateFormat,
		Version:  appVersion,
		SavedAt:  time.Now().Format(time.RFC3339),
		Type:     selectedTypeName,
		Chain:    currentUserChain,
		PreampDB: currentPreampDB,
		GainDB:   chainGainDB,
		Bypass:   dspBypass,
		Headroom: dspHeadroomOn,
		Volume:   currentVolume,
		Loudness: loudnessPersist{On: loudnessOn, Strength: loudnessStrength, RefOffset: loudnessRefOffsetDB},
		Limiter: limiterPersist{
			Enabled: dspLimiterEnabled, TruePeak: dspLimiterTP, ThrDB: dspLimiterThrDB,
			AttMs: dspLimiterAttMs, RelMs: dspLimiterRelMs,
		},
	}
	if currentDynBass != nil {
		p := *currentDynBass
		st.DynBass = &p
	}
	if currentDynamicBass != nil {
		p := *currentDynamicBass
		st.DynamicBass = &p
	}
	if currentCrossfeed != nil {
		p := *currentCrossfeed
		st.Crossfeed = &p
	}
	if currentSurround != nil {
		p := *currentSurround
		st.Surround = &p
	}
	if currentExciter != nil {
		p := *currentExciter
		st.Exciter = &p
	}
	if currentViPERBass != nil {
		p := *currentViPERBass
		st.ViPERBass = &p
	}
	if currentColorful != nil {
		p := *currentColorful
		st.ColorfulMusic = &p
	}
	if currentTube != nil {
		p := *currentTube
		st.Tube = &p
	}
	if currentClarity != nil {
		p := *currentClarity
		st.Clarity = &p
	}
	if currentSpeakerCorrection != nil {
		p := *currentSpeakerCorrection
		st.SpeakerCorrection = &p
	}
	if currentAnalogX != nil {
		p := *currentAnalogX
		st.AnalogX = &p
	}
	return st
}

func markStateDirty() {

	stateMu.Lock()
	restoring := stateRestoring
	timer := stateTimer

	if restoring || !stateBootRestoreDone {
		stateMu.Unlock()
		return
	}
	stateMu.Unlock()

	data, err := marshalRuntimeState()
	if err != nil {
		log.Printf("State snapshot failed, skipping write: %v", err)
		return
	}

	stateMu.Lock()
	defer stateMu.Unlock()

	if stateRestoring || !stateBootRestoreDone {
		return
	}

	statePending = data
	if timer != nil {
		timer.Reset(stateDebounce)
		return
	}
	stateTimer = time.AfterFunc(stateDebounce, func() {
		stateMu.Lock()
		d := statePending
		statePending = nil
		stateTimer = nil
		stateDirty = true
		stateMu.Unlock()

		if d != nil {
			if err := writeStateBytes(d); err != nil {
				log.Printf("State write failed: %v", err)
			}
		}
	})
}

func marshalRuntimeState() ([]byte, error) {
	st := snapshotRuntimeState()
	return json.MarshalIndent(st, "", "  ")
}

func flushPendingState() error {
	stateMu.Lock()
	d := statePending
	statePending = nil
	if stateTimer != nil {
		stateTimer.Stop()
		stateTimer = nil
	}
	stateMu.Unlock()
	if d == nil {
		return nil
	}
	stateMu.Lock()
	stateDirty = false
	stateMu.Unlock()
	return writeStateBytes(d)
}

func saveRuntimeState() error {
	stateMu.Lock()
	stateDirty = false
	stateMu.Unlock()

	data, err := marshalRuntimeState()
	if err != nil {
		return err
	}
	return writeStateBytes(data)
}

func stateFilePath() string {
	statePathMu.RLock()
	defer statePathMu.RUnlock()
	return statePath
}

func setStateFilePath(p string) {
	statePathMu.Lock()
	statePath = p
	statePathMu.Unlock()
}

func writeStateBytes(data []byte) error {
	path := stateFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-state-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func loadRuntimeState() (runtimeState, bool, error) {
	var st runtimeState
	data, err := os.ReadFile(stateFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return st, false, nil
		}
		return st, false, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, true, fmt.Errorf("State file is not valid JSON: %w", err)
	}
	if st.Format != stateFormat {
		return st, true, fmt.Errorf("State file format %d, this build only accepts %d", st.Format, stateFormat)
	}
	return st, true, nil
}

func restoreRuntimeState() {

	defer func() {
		stateMu.Lock()
		stateBootRestoreDone = true
		stateMu.Unlock()
	}()

	st, ok, err := loadRuntimeState()
	if err != nil {
		log.Printf("State file has issues, running with defaults: %v", err)
		return
	}
	if !ok {
		log.Printf("No state file (%s), running with defaults", stateFilePath())
		return
	}

	stateRestoring = true
	defer func() { stateRestoring = false }()

	applyRuntimeState(st, dynBassAvailable(), crossfeedAvailable(), surroundAvailable(),
		exciterAvailable(), colorfulAvailable())

	if err := setSystemVolume(currentVolume); err != nil {
		log.Printf("Restoring volume %d%% failed: %v", currentVolume, err)
	}
	if err := dspSetLimiter(dspLimiterEnabled, dspLimiterThrDB); err != nil {
		log.Printf("Failed to restore limiter: %v", err)
	}
	if err := dspSetLimiterTimes(dspLimiterAttMs, dspLimiterRelMs); err != nil {
		log.Printf("Failed to restore limiter time constants: %v", err)
	}
	if err := applyChain(st.Chain, st.PreampDB, st.GainDB); err != nil {
		log.Printf("Failed to restore chain (running flat): %v", err)
		if err := applyChain(preserveResourceChainItems(nil, currentUserChain), 0, 0); err != nil {
			log.Printf("Even the flat chain failed to deploy: %v", err)
		}
		return
	}

	if err := dspSetBypass(st.Bypass); err != nil {
		log.Printf("Failed to restore bypass state: %v", err)
	}
	log.Printf("Restored last settings: volume=%d%%, type=%q, %d sections chain, bypass=%s, dynamic bass=%s, V4A dynamic bass=%s, crossfeed=%s, surround=%s, loudness=%s, limiting=%s@%.1fdBFS",
		currentVolume,
		nonEmpty(st.Type, "(not recorded)"), len(st.Chain), onOff(dspBypass), onOff(currentDynBass != nil),
		onOff(currentDynamicBass != nil),
		onOff(currentCrossfeed != nil), onOff(currentSurround != nil),
		onOff(loudnessOn), limiterModeName(), dspLimiterThrDB)
}

func applyRuntimeState(st runtimeState, dynOK, mixOK, dlyOK, polyOK, jsOK bool) {
	if st.Type != "" {
		selectedTypeName = st.Type
	}

	currentUserChain = st.Chain
	currentPreampDB = st.PreampDB
	chainGainDB = st.GainDB
	loudnessOn = st.Loudness.On
	if st.Loudness.Strength > 0 {
		loudnessStrength = st.Loudness.Strength
	}
	if st.Loudness.RefOffset >= 0 {
		loudnessRefOffsetDB = st.Loudness.RefOffset
	}
	dspLimiterEnabled = st.Limiter.Enabled
	dspLimiterTP = st.Limiter.TruePeak
	if st.Limiter.ThrDB != 0 || st.Limiter.Enabled {
		dspLimiterThrDB = st.Limiter.ThrDB
	}
	if st.Limiter.AttMs > 0 {
		dspLimiterAttMs = st.Limiter.AttMs
	}
	if st.Limiter.RelMs > 0 {
		dspLimiterRelMs = st.Limiter.RelMs
	}
	dspHeadroomOn = st.Headroom

	if st.Volume > 0 && st.Volume <= 100 {
		currentVolume = st.Volume
	}

	if st.DynBass != nil {
		if dynOK {
			p := *st.DynBass
			currentDynBass = &p
		} else {
			log.Printf("State has dynamic bass, but this bitstream has no DYN slot => not enabling it this time")
			currentDynBass = nil
		}
	} else {
		currentDynBass = nil
	}

	if st.DynamicBass != nil {
		if dynamicBassAvailable() {
			p := *st.DynamicBass
			if _, err := dynamicBassValidate(p); err != nil {
				log.Printf("DynamicBass params in state invalid (%v) => not enabling it this time", err)
				currentDynamicBass = nil
			} else {
				currentDynamicBass = &p
			}
		} else {
			log.Printf("State has V4A DynamicBass, but this bitstream has no BIQUAD+MIX2 slots => not enabling it this time")
			currentDynamicBass = nil
		}
	} else {
		currentDynamicBass = nil
	}

	if st.Crossfeed != nil {
		if mixOK {
			p := *st.Crossfeed
			currentCrossfeed = &p
		} else {
			log.Printf("State has crossfeed, but this bitstream has no MIX2 slot => not enabling it this time")
			currentCrossfeed = nil
		}
	} else {
		currentCrossfeed = nil
	}

	if st.Surround != nil {
		if dlyOK {
			p := *st.Surround
			currentSurround = &p
		} else {
			log.Printf("State has surround, but this bitstream has no DELAY slot => not enabling it this time")
			currentSurround = nil
		}
	} else {
		currentSurround = nil
	}

	if st.ColorfulMusic != nil {
		if jsOK {
			if p, err := colorfulValidate(*st.ColorfulMusic); err == nil {
				currentColorful = &p
			} else {
				log.Printf("ColorfulMusic params in state invalid (%v) => not enabling it this time", err)
				currentColorful = nil
			}
		} else {
			log.Printf("State has ColorfulMusic, but this bitstream has no joint-stereo frame pass => not enabling it this time")
			currentColorful = nil
		}
	} else {
		currentColorful = nil
	}

	if st.Exciter != nil {
		if polyOK {
			p := *st.Exciter
			currentExciter = &p
		} else {
			log.Printf("State has harmonic exciter, but this bitstream has no POLY slot => not enabling it this time")
			currentExciter = nil
		}
	} else {
		currentExciter = nil
	}

	if st.ViPERBass != nil && viperBassAvailable() {
		p := *st.ViPERBass
		if err := setViPERBass(&p); err != nil {
			log.Printf("ViPERBass in state invalid (%v) => not enabling it this time", err)
			currentViPERBass = nil
		}
	} else {
		currentViPERBass = nil
	}

	if st.Tube != nil && tubeAvailable() {
		currentTube = &tubeParams{}
	} else {
		currentTube = nil
	}
	if st.Clarity != nil && clarityAvailable() {
		p := *st.Clarity

		if err := setClarity(&p); err != nil {
			log.Printf("Clarity in state invalid (%v) => not enabling it this time", err)
			currentClarity = nil
		}
	} else {
		currentClarity = nil
	}
	if st.SpeakerCorrection != nil && speakerCorrectionAvailable() {
		currentSpeakerCorrection = &speakerCorrectionParams{}
	} else {
		currentSpeakerCorrection = nil
	}
	if st.AnalogX != nil && analogxAvailable() {
		p := *st.AnalogX
		if err := setAnalogX(&p); err != nil {
			log.Printf("AnalogX in state invalid (%v) => not enabling it this time", err)
			currentAnalogX = nil
		}
	} else {
		currentAnalogX = nil
	}
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

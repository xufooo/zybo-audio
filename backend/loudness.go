// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
)

const (
	loudnessRefDB = 0.0

	loudnessPerDB = 0.35

	loudnessMaxBoostDB = 10.0

	loudnessTrebleFrac = 0.30
)

var (
	loudnessOn       = true
	loudnessStrength = 0.5

	loudnessRefOffsetDB = 10.0
)

func loudnessAmount() float64 {
	if !loudnessOn {
		return 0
	}
	db := systemVolumeDB(volumePct)
	need := (loudnessRefDB - loudnessRefOffsetDB) - db
	if need <= 0 {
		return 0
	}
	v := need * loudnessPerDB * loudnessStrength
	if v > loudnessMaxBoostDB {
		v = loudnessMaxBoostDB
	}
	if v < 0 {
		v = 0
	}
	return v
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

func loudnessCurve() []ChainItem {
	l := loudnessAmount()
	if l < 0.25 {
		return nil
	}
	return []ChainItem{
		{Type: "peq", Enabled: true, Freq: 60, GainDB: round1(l), Q: 0.7},
		{Type: "peq", Enabled: true, Freq: 150, GainDB: round1(l * 0.75), Q: 0.8},
		{Type: "peq", Enabled: true, Freq: 400, GainDB: round1(l * 0.30), Q: 0.8},
		{Type: "peq", Enabled: true, Freq: 10000, GainDB: round1(l * loudnessTrebleFrac), Q: 0.8},
	}
}

func withLoudness(chain []ChainItem) []ChainItem {
	curve := loudnessCurve()
	if len(curve) == 0 {
		return chain
	}
	out := make([]ChainItem, 0, len(chain)+len(curve))
	out = append(out, chain...)
	for _, c := range curve {
		merged := false
		for i := range out {
			if math.Abs(out[i].Freq-c.Freq) < 1 {
				out[i].GainDB = round1(out[i].GainDB + c.GainDB)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, c)
		}
	}
	return out
}

func handleDSPLoudness(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, loudnessState())
	case http.MethodPost:
		var body struct {
			Enabled  *bool    `json:"enabled"`
			Strength *float64 `json:"strength"`

			OffsetDB *float64 `json:"offset_db"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
			httpError(w, http.StatusBadRequest, "Request body is not valid JSON:"+err.Error())
			return
		}
		if body.Enabled != nil {
			loudnessOn = *body.Enabled
		}
		if body.OffsetDB != nil {
			v := *body.OffsetDB
			if v < 0 {
				v = 0
			}
			if v > 15 {
				v = 15
			}
			loudnessRefOffsetDB = v
		}
		if body.Strength != nil {
			s := *body.Strength
			if s < 0 {
				s = 0
			}
			if s > 1 {
				s = 1
			}
			loudnessStrength = s
		}

		if err := reapplyCurrentChain(); err != nil {
			httpError(w, http.StatusInternalServerError, "Redeploy failed:"+err.Error())
			return
		}
		writeJSON(w, loudnessState())
	default:
		httpError(w, http.StatusMethodNotAllowed, "Only GET / POST supported")
	}
}

func loudnessState() map[string]any {
	return map[string]any{
		"enabled":    loudnessOn,
		"strength":   loudnessStrength,
		"amount_db":  round1(loudnessAmount()),
		"volume_db":  round1(systemVolumeDB(volumePct)),
		"ref_db":     loudnessRefDB - loudnessRefOffsetDB,
		"ref_offset": loudnessRefOffsetDB,
		"max_boost":  loudnessMaxBoostDB,
		"per_db":     loudnessPerDB,
		"curve":      loudnessCurve(),
	}
}

func reapplyCurrentChain() error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	if err := compileCurrentChain(); err != nil {
		return err
	}
	return dspWriteAllBands()
}

func compileCurrentChain() error {
	slots, _, err := chainToSlots(withLoudness(currentUserChain))
	if err != nil {
		return fmt.Errorf("Failed to recompile chain: %w", err)
	}
	currentSlots = slots
	return nil
}

func loudnessView() map[string]any {
	return map[string]any{
		"on":        loudnessOn,
		"strength":  loudnessStrength,
		"offset_db": loudnessRefOffsetDB,
	}
}

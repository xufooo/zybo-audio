// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
)

type budgetEffectCost struct {
	ID       string `json:"id"`
	Slots    int    `json:"slots"`
	Sections int    `json:"sections"`
	Coefs    int    `json:"coefs"`
}

func budgetErrorBody(err error) map[string]any {
	var ce *capacityError
	if errors.As(err, &ce) {
		return map[string]any{
			"error":  ce.Error(),
			"code":   "capacity",
			"what":   ce.What,
			"needed": ce.Needed,
			"limit":  ce.Limit,
		}
	}
	return map[string]any{"error": err.Error()}
}

func handleDSPBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "Only GET /api/dsp/budget supported")
		return
	}

	frame := map[string]any{"budget": frameBudgetCycles}
	out := map[string]any{
		"limit":      slotLimit(),
		"per_effect": budgetPerEffect(),
		"frame":      frame,
	}

	saveHeadroom := dspHeadroomOn
	coefs, _ := dspFinalCoeffs()
	dspHeadroomOn = saveHeadroom

	nodes, err := buildChainNodes(coefs[:dspActiveBands()], currentDynBass, currentCrossfeed, currentSurround)
	if err != nil {
		out["used_error"] = budgetErrorBody(err)

		var ce *capacityError
		if errors.As(err, &ce) && ce.What == "frame" {
			frame["cost"] = ce.Needed
		}
		writeJSON(w, out)
		return
	}
	frame["cost"] = frameUsageOf(nodes).Cost

	plan, perr := buildSlotPlanNodes(nodes)
	if perr != nil {
		out["used_error"] = budgetErrorBody(perr)
		writeJSON(w, out)
		return
	}
	out["used"] = plan.Slots
	writeJSON(w, out)
}

func budgetPerEffect() []budgetEffectCost {
	type entry struct {
		id    string
		nodes func() ([]planNode, error)

		always bool
	}
	var out []budgetEffectCost
	add := func(id string, nodes []planNode) {
		if len(nodes) == 0 {
			out = append(out, budgetEffectCost{ID: id})
			return
		}
		plan, perr := buildSlotPlanNodes(nodes)
		if perr != nil {

			budgetSkipLog(id, perr)
			return
		}
		out = append(out, budgetEffectCost{
			ID: id, Slots: plan.Slots, Sections: plan.Sections, Coefs: len(plan.Coefs),
		})
	}

	for _, e := range []entry{

		{id: "viperbass", nodes: func() ([]planNode, error) { return viperBassNodes(viperBassDefaultParams()) }},
		{id: "viperbass_pbp", nodes: func() ([]planNode, error) {
			p := viperBassDefaultParams()
			p.Mode = viperBassModePBP
			return viperBassNodes(p)
		}},
		{id: "dynamic_bass", nodes: func() ([]planNode, error) {
			return dynamicBassNodes(dynamicBassDefaultParams())
		}},
		{id: "dyn_bass", nodes: func() ([]planNode, error) {
			c, err := dynParamsToCoefs(dynDefaultParams())
			if err != nil {
				return nil, err
			}
			return []planNode{{
				Kind: planKindDyn, Coefs: c, Side: dynSideChainCoefs(),
			}}, nil
		}},

		{id: "clarity", nodes: func() ([]planNode, error) { return clarityNodes(clarityDefaultParams(), sampleRate) }},
		{id: "clarity_natural", nodes: func() ([]planNode, error) {
			p := clarityDefaultParams()
			p.Mode = clarityModeNatural
			return clarityNodes(p, sampleRate)
		}},
		{id: "clarity_ozone", nodes: func() ([]planNode, error) {
			p := clarityDefaultParams()
			p.Mode = clarityModeOzone
			return clarityNodes(p, sampleRate)
		}},
		{id: "clarity_xhifi", nodes: func() ([]planNode, error) {
			p := clarityDefaultParams()
			p.Mode = clarityModeXHIFI
			return clarityNodes(p, sampleRate)
		}},
		{id: "vse", nodes: func() ([]planNode, error) { return exciterNodes(exciterDefaultParams()) }},
		{id: "analogx", nodes: func() ([]planNode, error) { return analogxNodes(analogxDefaultParams(), sampleRate) }},
		{id: "tube", nodes: func() ([]planNode, error) { return tubeNodes(), nil }},
		{id: "speaker_correction", nodes: func() ([]planNode, error) {
			return speakerCorrectionNodes(sampleRate), nil
		}},
		{id: "colorfulmusic", nodes: func() ([]planNode, error) { return colorfulNodes(colorfulDefaultParams()) }},

		{id: "crossfeed", nodes: func() ([]planNode, error) {
			lo, hi, mix, err := crossfeedCoefs(crossfeedDefaultParams())
			if err != nil {
				return nil, err
			}
			return []planNode{{Kind: planKindCross, Coefs: lo, Hi: hi, Mix: mix}}, nil
		}},
		{id: "surround", nodes: func() ([]planNode, error) {
			n := surroundDelaySamples(surroundDefaultParams())
			if n <= 0 {
				return nil, nil
			}
			return []planNode{{Kind: planKindDelay, Len: n, Flags: flDlyRight}}, nil
		}},

		{id: "eq", nodes: budgetCurrentEQNodes, always: true},
		{id: "loudness", nodes: budgetLoudnessNodes, always: true},
	} {
		nodes, err := e.nodes()
		if err != nil {
			budgetSkipLog(e.id, err)
			continue
		}

		if len(nodes) == 0 && !e.always {
			continue
		}
		add(e.id, nodes)
	}
	return out
}

func budgetSkipLog(id string, err error) {
	budgetSkipMu.Lock()
	defer budgetSkipMu.Unlock()
	if budgetSkip[id] {
		return
	}
	budgetSkip[id] = true
	log.Printf("budget: %s cannot be compiled (this bitstream cannot fit it; it will have no per_effect entry): %v", id, err)
}

var (
	budgetSkipMu sync.Mutex
	budgetSkip   = map[string]bool{}
)

func budgetCurrentEQNodes() ([]planNode, error) {
	var nodes []planNode
	for i := 0; i < dspActiveBands(); i++ {
		sc := currentSlots[i]
		if isBandOff(sc.Type) {
			continue
		}
		b0, b1, b2, a1, a2, err := designBiquad(sanitizeBand(sc))
		if err != nil {
			budgetSkipLog(fmt.Sprintf("eq#%d(%.0fHz)", i, sc.Freq), err)
			continue
		}
		nodes = append(nodes, planNode{Kind: planKindBiquad, Coefs: [5]int32{b0, b1, b2, a1, a2}})
	}
	return nodes, nil
}

func budgetLoudnessNodes() ([]planNode, error) {
	var nodes []planNode
	for _, it := range loudnessCurve() {
		st, ok := chainSlotType(it.Type)
		if !ok {
			continue
		}
		sc := sanitizeBand(slotConfig{Type: st, Freq: it.Freq, Q: it.Q, GainDB: it.GainDB})
		b0, b1, b2, a1, a2, err := designBiquad(sc)
		if err != nil {
			budgetSkipLog(fmt.Sprintf("loudness#%.0fHz", it.Freq), err)
			continue
		}
		nodes = append(nodes, planNode{Kind: planKindBiquad, Coefs: [5]int32{b0, b1, b2, a1, a2}})
	}
	return nodes, nil
}

// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

const (
	dynDetectFreq = 2200.0
	dynDetectQ    = 0.33

	dynEnvShift = 8
)

const dynamicBassSimpleBranchMaxX1 = 120.0

const dynamicBassDefaultPresetX1 = 100.0

func dynamicBassBranchOf(x1 float64) string {
	if x1 <= dynamicBassSimpleBranchMaxX1 {
		return "simple"
	}
	return "full"
}

func dynamicBassBranchNote() map[string]any {
	return map[string]any{
		"implemented":              currentDynamicBass != nil,
		"branch_of_default_preset": dynamicBassBranchOf(dynamicBassDefaultPresetX1),
		"default_preset_x1":        dynamicBassDefaultPresetX1,
		"simple_branch_max_x1":     dynamicBassSimpleBranchMaxX1,
		"source": "refs/viperfx-re/src/viper/utils/DynamicBass.cpp:21-28 (simple branch)/:29-41 (full branch); " +
			"default preset x1=100 refs/viper4android_fx/android_4.x/res/xml/headset_preferences_l2.xml:274",
		"decision": "Done: **simple branch** (x1 <= 120; the official default 100;5600;40;80;50;50 qualifies)," +
			"implemented in dynbass.go, API field `dynamic_bass`;" +
			"full branch (x1 > 120) **not done and explicitly rejected**:" + dynamicBassFullBranchBlocker(),
		"needs_owner_decision": false,
		"why": "On-device DYN is 'sidechain detection + level-following slow gain' (~= V4A PlaybackGain)," +
			"a different algorithm from V4A DynamicBass => this adds a **new stage** rather than changing a branch;" +
			"the simple branch' s 'same-tick L+R' can only do on this unit: 'R pass same-frame, L pass late 1 sample'" +
			"(TB measurement, see fpga/src/tb/tb_engine_xphase.v)",
		"side_channel_lag_samples": dynamicBassSideLagSmp,
		"api_field":                "dynamic_bass",
	}
}

type dynParams struct {
	GainDB float64 `json:"gain_db"`
	CutDB  float64 `json:"cut_db"`
	RefDB  float64 `json:"ref_db"`
	KS     float64 `json:"ks"`
	AttMs  float64 `json:"att_ms"`
	RelMs  float64 `json:"rel_ms"`
}

func dynDefaultParams() dynParams {

	return dynParams{GainDB: 6, CutDB: 0, RefDB: -25, KS: 0.75, AttMs: 5, RelMs: 200}
}

func dbToQ315(db float64) int32 {
	return int32(math.Round(math.Pow(10, db/20) * qOne))
}

func shiftFromMs(ms float64) int32 {
	if ms <= 0 {
		return 0
	}
	tau := ms / 1000.0
	s := math.Log2(tau * sampleRate)
	v := int32(math.Round(s))
	if v < 0 {
		v = 0
	}
	if v > 31 {
		v = 31
	}
	return v
}

func dynParamsToCoefs(p dynParams) ([5]int32, error) {
	var c [5]int32
	if p.GainDB < 0 {
		p.GainDB = 0
	}

	if p.GainDB > 12 {
		p.GainDB = 12
	}
	if p.CutDB < 0 {
		p.CutDB = 0
	}
	if p.CutDB > 12 {
		p.CutDB = 12
	}
	if p.RefDB > 0 {
		p.RefDB = 0
	}
	if p.RefDB < -60 {
		p.RefDB = -60
	}
	gmax := dbToQ315(p.GainDB)
	gmin := dbToQ315(-p.CutDB)
	ref := dbToQ315(p.RefDB)

	ks := int32(math.Round(p.KS * qOne))
	for _, v := range []struct {
		n string
		v int32
	}{{"gmax", gmax}, {"gmin", gmin}, {"ref", ref}, {"ks", ks}} {
		if v.v > coefMax || v.v < coefMin {
			return c, fmt.Errorf("DYN param %s=%d out of Q3.15 range", v.n, v.v)
		}
	}
	att := shiftFromMs(p.AttMs)
	rel := shiftFromMs(p.RelMs)
	c[0], c[1], c[2], c[3] = gmax, gmin, ref, ks
	c[4] = (rel << 5) | att
	return c, nil
}

func dynSideChainCoefs() [5]int32 {
	b0, b1, b2, a1, a2 := rbjBandPass(dynDetectFreq, dynDetectQ, sampleRate)
	return [5]int32{b0, b1, b2, a1, a2}
}

var currentDynBass *dynParams

func dynBassAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().DynAvailable()
}

func setDynBass(p *dynParams) error {
	if p == nil {
		currentDynBass = nil
		markStateDirty()
		return nil
	}
	if !dynBassAvailable() {
		return fmt.Errorf("This hardware has no DYN stage (CAP1 bit11 not set), dynamic bass cannot be deployed")
	}
	if _, err := dynParamsToCoefs(*p); err != nil {
		return err
	}
	q := *p
	currentDynBass = &q
	markStateDirty()
	return nil
}

func buildChainNodes(sections [][5]int32, dyn *dynParams, cross *crossfeedParams,
	surround *surroundParams) ([]planNode, error) {
	return buildChainNodesWithBass(sections, dyn, cross, surround, currentViPERBass)
}

func buildChainNodesWithBass(sections [][5]int32, dyn *dynParams, cross *crossfeedParams,
	surround *surroundParams, bass *viperBassParams) ([]planNode, error) {
	nodes := make([]planNode, 0, len(sections)+5)

	if currentExciter != nil {
		ex, err := exciterNodes(*currentExciter)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, ex...)
	}

	if dyn != nil {
		c, err := dynParamsToCoefs(*dyn)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, planNode{Kind: planKindDyn, Coefs: c, Side: dynSideChainCoefs()})
	}

	if currentDynamicBass != nil {
		dbn, err := dynamicBassNodes(*currentDynamicBass)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, dbn...)
	}
	for _, s := range sections {
		nodes = append(nodes, planNode{Kind: planKindBiquad, Coefs: s})
	}

	if currentFIR != nil {
		nodes = append(nodes, planNode{Kind: planKindFIR, Blocks: firParamsBlocks(currentFIR)})
	}

	nodes = append(nodes, ddcNodes()...)

	if bass != nil {
		bn, err := viperBassNodes(*bass)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, bn...)
	}

	if currentClarity != nil {
		cn, err := clarityNodes(*currentClarity, sampleRate)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, cn...)
	}

	if cross != nil {
		lo, hi, mix, err := crossfeedCoefs(*cross)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, planNode{Kind: planKindCross, Coefs: lo, Hi: hi, Mix: mix})

		if cross.PassFilter {
			for _, c := range curePassFilterSections(sampleRate) {
				nodes = append(nodes, planNode{Kind: planKindBiquad, Coefs: c})
			}
		}
	}
	if surround != nil {
		if n := surroundDelaySamples(*surround); n > 0 {
			nodes = append(nodes, planNode{
				Kind: planKindDelay, Len: n, Flags: flDlyRight,
			})
		}
	}

	if currentColorful != nil {
		if !colorfulAvailable() {
			return nil, fmt.Errorf("Chain has ColorfulMusic, but this bitstream has no joint-stereo frame pass" +
				"(CAP1 bit12) - not deploying (silently dropping it would make users think it is on)")
		}
		cn, err := colorfulNodes(*currentColorful)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, cn...)
	}

	if currentSpeakerCorrection != nil {
		nodes = append(nodes, speakerCorrectionNodes(sampleRate)...)
	}

	if currentTube != nil {
		nodes = append(nodes, tubeNodes()...)
	}

	if currentAnalogX != nil {
		an, err := analogxNodes(*currentAnalogX, sampleRate)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, an...)
	}

	if err := checkFrameBudget(nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

type frameUsage struct {
	Sections int
	Slots    int
	Cost     int
	HasFIR   bool
	HasPOLY  bool
	HasSFIR  bool
	HasJoint bool
}

func frameUsageOf(nodes []planNode) frameUsage {
	var u frameUsage

	slotExtra := 0
	for _, n := range nodes {
		switch n.Kind {
		case planKindBiquad:
			u.Sections++
		case planKindCross:
			u.Sections += 2
		case planKindDyn:
			u.Sections += 2
		case planKindExciter:
			u.Sections += 3
			if n.MixB[0] != 0 {
				u.Sections++
			}
			u.HasPOLY = true
		case planKindViPERBass:
			u.Sections += 3
		case planKindViPERBassPBP:

			u.Sections += 3
			u.HasSFIR = true
		case planKindColorfulMusic:

			u.Sections += jointStateReserve
			u.HasJoint = true
		case planKindFIR:
			u.HasFIR = true
		case planKindDynBass:
			u.Sections += dynamicBassSections
			slotExtra += dynamicBassSlots - 1
		}
		u.Slots++
	}
	u.Slots += slotExtra

	u.Cost = chainFrameCost(u.Sections, u.HasFIR, u.HasPOLY, u.HasSFIR, u.HasJoint, u.Slots)
	return u
}

func checkFrameBudget(nodes []planNode) error {
	u := frameUsageOf(nodes)
	if u.Cost <= frameBudgetCycles {
		return nil
	}
	return newCapacityError("frame", u.Cost, frameBudgetCycles,
		"Chain does not fit the per-frame budget: %d sections%s%s ~= %d cycles > per channel %d cycles (%s)"+
			"- over budget **drops samples** (sounds like occasional clicks). Drop a section, turn off convolution, or split the effects",
		u.Sections, map[bool]string{true: " + convolution", false: ""}[u.HasFIR],
		map[bool]string{true: " + small FIR", false: ""}[u.HasSFIR],
		u.Cost, frameBudgetCycles, fmt.Sprintf("per section %d cycles", costPerSectionRuntime()))
}

func dynBassView() any {
	if currentDynBass == nil {
		return nil
	}
	p := *currentDynBass
	return map[string]any{
		"gain_db": p.GainDB, "cut_db": p.CutDB, "ref_db": p.RefDB,
		"ks": p.KS, "att_ms": p.AttMs, "rel_ms": p.RelMs,
		"detect_hz": dynDetectFreq, "detect_q": dynDetectQ,
		"available": dynBassAvailable(),

		"v4a_dynamic_bass": dynamicBassBranchNote(),
	}
}

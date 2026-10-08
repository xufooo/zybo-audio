// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const capBitBiquad = 1 << 1

func withCapSections(t *testing.T, n int) {
	t.Helper()
	save := hwMaxSections
	hwMaxSections = n
	t.Cleanup(func() { hwMaxSections = save })
}

func withCapDelaySlots(t *testing.T, n int) {
	t.Helper()
	save := hwDelaySlots
	hwDelaySlots = n
	t.Cleanup(func() { hwDelaySlots = save })
}

func peqChain(n int) []ChainItem {
	out := make([]ChainItem, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, ChainItem{
			Type: "peq", Enabled: true,
			Freq: 60 + float64(i)*120, GainDB: 1, Q: 0.7,
		})
	}
	return out
}

func asCapacity(t *testing.T, err error, what string) *capacityError {
	t.Helper()
	if err == nil {
		t.Fatalf("should return a capacity error, got nil")
	}
	var ce *capacityError
	if !errors.As(err, &ce) {
		t.Fatalf("error should be *capacityError, got %T: %v", err, err)
	}
	if ce.What != what {
		t.Errorf("What = %q, want %q (panel uses it to pick the hint column)", ce.What, what)
	}
	if ce.Needed <= ce.Limit {
		t.Errorf("Needed(%d) must be greater than Limit(%d), otherwise this is not over the limit at all", ce.Needed, ce.Limit)
	}
	if ce.Error() == "" {
		t.Error("user-facing message must not be empty (this is what the user reads)")
	}
	return ce
}

func TestCapacityErrorAtEveryProductionSite(t *testing.T) {
	fakeHardware(t, capBitBiquad|capOpcodePoly)

	t.Run("bands/chainToSlots", func(t *testing.T) {
		withCapSections(t, 6)
		_, _, err := chainToSlots(peqChain(8))
		ce := asCapacity(t, err, "bands")
		if ce.Needed != 8 || ce.Limit != 6 {
			t.Errorf("needed/limit = %d/%d, want 8/6", ce.Needed, ce.Limit)
		}
		if !strings.Contains(ce.Error(), "NOT applied") {
			t.Errorf("user-facing message must say which sections were left unapplied: %q", ce.Error())
		}
	})

	t.Run("sections/buildSlotPlan", func(t *testing.T) {
		withCapSections(t, 24)
		secs := make([][5]int32, 25)
		_, err := buildSlotPlan(secs)
		ce := asCapacity(t, err, "sections")
		if ce.Needed != 25 || ce.Limit != 24 {
			t.Errorf("needed/limit = %d/%d, want 25/24", ce.Needed, ce.Limit)
		}
	})

	t.Run("sections/buildSlotPlanNodes", func(t *testing.T) {
		withCapSections(t, 24)
		nodes := make([]planNode, 25)
		for i := range nodes {
			nodes[i] = planNode{Kind: planKindBiquad, Coefs: [5]int32{qOne, 0, 0, 0, 0}}
		}
		_, err := buildSlotPlanNodes(nodes)
		ce := asCapacity(t, err, "sections")
		if ce.Needed != 25 || ce.Limit != 24 {
			t.Errorf("needed/limit = %d/%d, want 25/24", ce.Needed, ce.Limit)
		}
	})

	t.Run("slots/emitF", func(t *testing.T) {

		c, err := dynParamsToCoefs(dynDefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		nodes := make([]planNode, 13)
		for i := range nodes {
			nodes[i] = planNode{Kind: planKindDyn, Coefs: c, Side: dynSideChainCoefs()}
		}
		_, err = buildSlotPlanNodes(nodes)
		ce := asCapacity(t, err, "slots")
		if ce.Limit != slotLimit() {
			t.Errorf("limit = %d, want slotLimit() = %d", ce.Limit, slotLimit())
		}
		if ce.Needed != ce.Limit+1 {
			t.Errorf("needed = %d, want limit+1 = %d (writing slot N is rejected => N slots needed)",
				ce.Needed, ce.Limit+1)
		}
		if !strings.Contains(ce.Error(), "wrap and clobber earlier slots") {
			t.Errorf("user-facing message must still say going over wraps around and overwrites earlier slots: %q", ce.Error())
		}
	})

	t.Run("frame/checkFrameBudget", func(t *testing.T) {

		nodes := make([]planNode, 90)
		for i := range nodes {
			nodes[i] = planNode{Kind: planKindBiquad, Coefs: [5]int32{qOne, 0, 0, 0, 0}}
		}
		err := checkFrameBudget(nodes)
		ce := asCapacity(t, err, "frame")
		if ce.Limit != frameBudgetCycles {
			t.Errorf("limit = %d, want frameBudgetCycles = %d", ce.Limit, frameBudgetCycles)
		}
		if want := frameUsageOf(nodes).Cost; ce.Needed != want {
			t.Errorf("needed = %d, want %d from the same source as frameUsageOf", ce.Needed, want)
		}
		if !strings.Contains(ce.Error(), "drops samples") {
			t.Errorf("user-facing message must still name dropped samples as the consequence: %q", ce.Error())
		}
	})
}

func TestFrameUsageOfAgreesWithCheckFrameBudget(t *testing.T) {
	cases := [][]planNode{
		{},
		{{Kind: planKindBiquad, Coefs: [5]int32{qOne, 0, 0, 0, 0}}},
		{{Kind: planKindCross, Coefs: [5]int32{qOne, 0, 0, 0, 0}, Hi: [5]int32{qOne, 0, 0, 0, 0}}},
		{{Kind: planKindFIR, Blocks: firTaps / firMACS}},
		{{Kind: planKindColorfulMusic}},
		{{Kind: planKindDynBass, Coefs: [5]int32{qOne, 0, 0, 0, 0}}},
	}
	for i, nodes := range cases {
		u := frameUsageOf(nodes)
		var ce *capacityError
		if err := checkFrameBudget(nodes); err != nil {
			if !errors.As(err, &ce) {
				t.Fatalf("case %d: over-limit error must be typed, got %T", i, err)
			}
			if ce.Needed != u.Cost || ce.Limit != frameBudgetCycles {
				t.Errorf("case %d: ce(%d/%d) accounting disagrees with frameUsageOf.Cost=%d",
					i, ce.Needed, ce.Limit, u.Cost)
			}
		}
	}
}

func TestRejectedChainPutRollsBackTypeAndEffects(t *testing.T) {
	fakeHardware(t, capBitBiquad|capOpcodePoly)
	withStateFile(t)

	savedType := selectedTypeName
	savedSlots, savedChain := currentSlots, currentUserChain
	savedPreamp, savedGain := currentPreampDB, chainGainDB
	savedTube := currentTube
	savedDyn := currentDynBass
	t.Cleanup(func() {
		selectedTypeName = savedType
		currentSlots, currentUserChain = savedSlots, savedChain
		currentPreampDB, chainGainDB = savedPreamp, savedGain
		currentTube, currentDynBass = savedTube, savedDyn
	})
	selectedTypeName = "Headphones"
	currentTube = nil
	if slots, _, err := chainToSlots(peqChain(3)); err != nil {
		t.Fatal(err)
	} else {
		currentSlots = slots
	}
	currentUserChain = peqChain(3)
	currentPreampDB, chainGainDB = 0, 0

	withCapSections(t, 4)
	rec := chainPut(t, `{"type":"rejected-type","tube":{},"chain":[
		{"type":"peq","enabled":true,"freq":60,"gain_db":1,"q":0.7},
		{"type":"peq","enabled":true,"freq":180,"gain_db":1,"q":0.7},
		{"type":"peq","enabled":true,"freq":300,"gain_db":1,"q":0.7},
		{"type":"peq","enabled":true,"freq":420,"gain_db":1,"q":0.7},
		{"type":"peq","enabled":true,"freq":540,"gain_db":1,"q":0.7},
		{"type":"peq","enabled":true,"freq":660,"gain_db":1,"q":0.7}]}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("too big should be 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error  string `json:"error"`
		Code   string `json:"code"`
		What   string `json:"what"`
		Needed int    `json:"needed"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Code != "capacity" || body.What != "bands" {
		t.Errorf("code/what = %q/%q, want capacity/bands", body.Code, body.What)
	}
	if body.Needed != 6 || body.Limit != 4 {
		t.Errorf("needed/limit = %d/%d, want 6/4", body.Needed, body.Limit)
	}
	if body.Error == "" {
		t.Error("error field must carry a user-facing message (panel shows it directly)")
	}

	recGet := httptest.NewRecorder()
	handleDSPChain(recGet, httptest.NewRequest(http.MethodGet, "/api/dsp/chain", nil))
	var after struct {
		Type  string `json:"type"`
		Tube  any    `json:"tube"`
		Chain []struct {
			Freq float64 `json:"freq"`
		} `json:"chain"`
	}
	if err := json.Unmarshal(recGet.Body.Bytes(), &after); err != nil {
		t.Fatalf("failed to parse GET read-back: %v", err)
	}
	if after.Type != "Headphones" {
		t.Errorf("rejected request left type as %q, want back \"Headphones\" (0.3 section 4.3 old bug found by measurement)", after.Type)
	}
	if after.Tube != nil {
		t.Errorf("rejected request left tube in memory: %v", after.Tube)
	}
	if len(after.Chain) != 3 {
		t.Errorf("rejected request changed the chain to %d sections, want still 3 sections", len(after.Chain))
	}
}

func TestChainSnapshotRestoresTypeName(t *testing.T) {
	save := selectedTypeName
	t.Cleanup(func() { selectedTypeName = save })
	selectedTypeName = "Headphones"

	snap := captureChainState()
	selectedTypeName = "other"
	snap.restore()

	if selectedTypeName != "Headphones" {
		t.Errorf("snapshot did not cover type: after restore = %q, want \"Headphones\"", selectedTypeName)
	}
}

func TestAcceptedChainPutKeepsType(t *testing.T) {
	fakeHardware(t, capBitBiquad|capOpcodePoly|capOpcodeMix2)
	withStateFile(t)

	saveType := selectedTypeName
	saveSlots, saveChain := currentSlots, currentUserChain
	savePreamp, saveGain := currentPreampDB, chainGainDB
	t.Cleanup(func() {
		selectedTypeName = saveType
		currentSlots, currentUserChain = saveSlots, saveChain
		currentPreampDB, chainGainDB = savePreamp, saveGain
	})
	selectedTypeName = "old-type"

	rec := chainPut(t, `{"type":"new-type","chain":[
		{"type":"peq","enabled":true,"freq":60,"gain_db":1,"q":0.7}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("within 3 sections should be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if selectedTypeName != "new-type" {
		t.Errorf("successful request must not roll back: type = %q, want \"new-type\"", selectedTypeName)
	}
}

type budgetResp struct {
	Limit     int                `json:"limit"`
	Used      *int               `json:"used"`
	UsedError map[string]any     `json:"used_error"`
	PerEffect []budgetEffectCost `json:"per_effect"`
	Frame     struct {
		Cost   *int `json:"cost"`
		Budget int  `json:"budget"`
	} `json:"frame"`
}

func getBudget(t *testing.T) budgetResp {
	t.Helper()
	rec := httptest.NewRecorder()
	handleDSPBudget(rec, httptest.NewRequest(http.MethodGet, "/api/dsp/budget", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("budget should be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var out budgetResp
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("budget response is not valid JSON: %v (%s)", err, rec.Body.String())
	}
	return out
}

func TestBudgetContract(t *testing.T) {
	out := getBudget(t)

	if out.Limit != slotLimit() {
		t.Errorf("limit = %d, want slotLimit() = %d", out.Limit, slotLimit())
	}
	if out.Frame.Budget != frameBudgetCycles {
		t.Errorf("frame.budget = %d, want frameBudgetCycles = %d", out.Frame.Budget, frameBudgetCycles)
	}
	if out.Frame.Cost == nil {
		t.Error("frame.cost must be present (empty chain still computes 0)")
	}
	if (out.Used == nil) == (out.UsedError == nil) {
		t.Errorf("exactly one of used and used_error must be present: used=%v used_error=%v", out.Used, out.UsedError)
	}
	if len(out.PerEffect) == 0 {
		t.Fatal("per_effect must not be empty (panel builds the budget bar from it)")
	}

	rec := httptest.NewRecorder()
	handleDSPBudget(rec, httptest.NewRequest(http.MethodPost, "/api/dsp/budget", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/dsp/budget should be 405, got %d", rec.Code)
	}
}

func TestPerEffectCoversEveryCardCap(t *testing.T) {
	withCapDelaySlots(t, 8)
	out := getBudget(t)

	have := map[string]budgetEffectCost{}
	for _, e := range out.PerEffect {
		if _, dup := have[e.ID]; dup {
			t.Errorf("per_effect has %q twice", e.ID)
		}
		have[e.ID] = e
	}
	for _, id := range []string{

		"dyn_bass", "eq", "crossfeed", "colorfulmusic", "viperbass", "clarity",
		"vse", "dynamic_bass", "surround", "tube", "analogx", "loudness",
	} {
		if _, ok := have[id]; !ok {
			t.Errorf("per_effect missing card caps key %q (panel looks up cost by it)", id)
		}
	}

	for _, id := range []string{"viperbass_pbp", "clarity_natural", "clarity_ozone", "clarity_xhifi"} {
		if _, ok := have[id]; !ok {
			t.Errorf("per_effect missing level variant %q", id)
		}
	}

	if _, ok := have["speaker_correction"]; !ok {
		t.Error("per_effect missing speaker_correction (panel CAPS lists it)")
	}
}

func TestPerEffectCostsAreReal(t *testing.T) {
	withCapDelaySlots(t, 8)
	out := getBudget(t)
	have := map[string]budgetEffectCost{}
	for _, e := range out.PerEffect {
		have[e.ID] = e
	}

	cases := []struct {
		id       string
		slots    int
		sections int
		why      string
	}{
		{"viperbass", 2, 3, "BIQUAD n=2 packed + MIX2 (viperbass.go:1456-1474)"},
		{"viperbass_pbp", 4, 3, "DELAY + BIQUAD n=2 + SFIR + MIX2（dsp_slot.go:1504-1559）"},
		{"dynamic_bass", 4, 3, "NOP forwarding + MIX2 + BIQUAD + MIX2 (dsp_slot.go:1415-1440)"},
		{"dyn_bass", 2, 2, "detection bandpass occupies one slot + DYN slot (dsp_slot.go:1664-1683)"},
		{"clarity_natural", 1, 1, "one biquad node"},
		{"clarity_ozone", 1, 1, "one biquad node"},
		{"clarity_xhifi", 8, 12, "HP3+BP-LP3+BP-HP3+delay+LP+delay+MIX2+MIX2 (dsp_slot.go:1172-1247)"},
		{"vse", 4, 3, "HPF + POLY + LPF + MIX2（dsp_slot.go:1318-1364）"},
		{"analogx", 5, 5, "HP + POLY + MIX2 + LP + Peak（dsp_slot.go:1260-1305）"},
		{"tube", 1, 1, "one first-order stage"},
		{"speaker_correction", 1, 3, "3 sections exactly fill one slot (secPerSlot=3)"},
		{"crossfeed", 3, 2, "lo and hi must each occupy one slot + MIX2; MIX2 takes no state sections (dsp_slot.go:1639-1654)"},
		{"surround", 1, 0, "DELAY goes through the private dl_ring, takes no EQ sections (dsp_slot.go:1603-1628)"},
		{"colorfulmusic", 2, 2, "JDST + J3DS two slots, sections only count the 2 joint-state words"},
	}
	for _, c := range cases {
		got, ok := have[c.id]
		if !ok {
			t.Errorf("%s did not compile (%s)", c.id, c.why)
			continue
		}
		if got.Slots != c.slots || got.Sections != c.sections {
			t.Errorf("%s = %d slots / %d sections, want %d / %d (%s)",
				c.id, got.Slots, got.Sections, c.slots, c.sections, c.why)
		}
		if got.Coefs <= 0 {
			t.Errorf("%s coefficient word count = %d, should be > 0", c.id, got.Coefs)
		}
	}
}

func TestBudgetUsedTracksCurrentChain(t *testing.T) {
	saveSlots, saveChain := currentSlots, currentUserChain
	t.Cleanup(func() { currentSlots, currentUserChain = saveSlots, saveChain })

	slots, _, err := chainToSlots(peqChain(6))
	if err != nil {
		t.Fatal(err)
	}
	currentSlots = slots
	if got := getBudget(t); got.Used == nil || *got.Used != 2 {
		t.Errorf("6-section EQ used = %v, want 2", got.Used)
	}

	slots, _, err = chainToSlots(peqChain(3))
	if err != nil {
		t.Fatal(err)
	}
	currentSlots = slots
	if got := getBudget(t); got.Used == nil || *got.Used != 1 {
		t.Errorf("3-section EQ used = %v, want 1", got.Used)
	}

	slots, _, err = chainToSlots(nil)
	if err != nil {
		t.Fatal(err)
	}
	currentSlots = slots
	if got := getBudget(t); got.Used == nil || *got.Used != 0 {
		t.Errorf("empty-chain used = %v, want 0 (must not be a missing field)", got.Used)
	}
}

func TestBudgetGetDoesNotMutateHeadroomState(t *testing.T) {
	saveSlots := currentSlots
	saveHeadroom := dspHeadroomOn
	t.Cleanup(func() { currentSlots, dspHeadroomOn = saveSlots, saveHeadroom })

	slots, _, err := chainToSlots(peqChain(6))
	if err != nil {
		t.Fatal(err)
	}
	currentSlots = slots

	for _, on := range []bool{false, true} {
		dspHeadroomOn = on
		_ = getBudget(t)
		if dspHeadroomOn != on {
			t.Errorf("GET /budget changed dspHeadroomOn from %v to %v (this changes the limiter-threshold accounting)", on, dspHeadroomOn)
		}
	}
}

func TestBudgetUsedErrorCarriesCapacityFields(t *testing.T) {
	fakeHardware(t, capBitBiquad|capOpcodePoly)
	saveSlots := currentSlots
	saveSpk, saveTube := currentSpeakerCorrection, currentTube
	t.Cleanup(func() {
		currentSlots = saveSlots
		currentSpeakerCorrection, currentTube = saveSpk, saveTube
	})

	withCapSections(t, 24)
	slots, _, err := chainToSlots(peqChain(21))
	if err != nil {
		t.Fatal(err)
	}
	currentSlots = slots
	currentSpeakerCorrection = &speakerCorrectionParams{}
	currentTube = &tubeParams{}

	out := getBudget(t)
	if out.Used != nil {
		t.Fatalf("when sections are over the limit, used must be absent, got %d", *out.Used)
	}
	if out.UsedError == nil {
		t.Fatal("when sections are over the limit, used_error is required")
	}
	if out.UsedError["code"] != "capacity" || out.UsedError["what"] != "sections" {
		t.Errorf("used_error code/what = %v/%v, want capacity/sections",
			out.UsedError["code"], out.UsedError["what"])
	}
	if got, _ := out.UsedError["limit"].(float64); int(got) != 24 {
		t.Errorf("used_error.limit = %v, want 24", out.UsedError["limit"])
	}

	if out.Frame.Cost == nil {
		t.Error("even when sections do not fit, frame.cost must still be given (it is an independent constraint)")
	}
}

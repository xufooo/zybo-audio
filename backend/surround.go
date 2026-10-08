// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"math"
)

type surroundParams struct {
	DelayMs float64 `json:"delay_ms"`
}

const surroundMinMs = 0.0

func surroundMaxMs() float64 {
	return float64(hwDelayWords) / sampleRate * 1000.0
}

func surroundDefaultParams() surroundParams {
	return surroundParams{DelayMs: 2.5}
}

func surroundDelaySamples(p surroundParams) int {
	if p.DelayMs <= surroundMinMs {
		return 0
	}
	n := int(math.Round(p.DelayMs / 1000.0 * sampleRate))
	if n < 1 {
		n = 1
	}
	if n > hwDelayWords {
		n = hwDelayWords
	}
	return n
}

var currentSurround *surroundParams

func surroundAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().DelayAvailable()
}

func setSurround(p *surroundParams) error {
	if p == nil {
		currentSurround = nil
		markStateDirty()
		return nil
	}
	if !surroundAvailable() {
		return fmt.Errorf("this hardware has no DELAY slot (CAP1 bit4 not set), surround cannot dispatch")
	}
	q := clampSurroundMs(*p)
	currentSurround = &q
	markStateDirty()
	return nil
}

func clampSurroundMs(p surroundParams) surroundParams {
	if p.DelayMs < surroundMinMs {
		p.DelayMs = surroundMinMs
	}
	if p.DelayMs > surroundMaxMs() {
		p.DelayMs = surroundMaxMs()
	}
	return p
}

func surroundView() any {
	if currentSurround == nil {
		return nil
	}
	p := *currentSurround
	return map[string]any{
		"delay_ms":  p.DelayMs,
		"samples":   surroundDelaySamples(p),
		"available": surroundAvailable(),
		"max_ms":    surroundMaxMs(),
		"note": "V4A DiffSurround replica: delays the whole right channel by this many ms (Haas), " +
			"widens perception so vocals no longer crowd the center. Only 1-5 ms gives a widening effect (this unit ring caps at 5.3 ms), " +
			"any longer starts sounding like an echo." +
			"range is self-calibrated (decompiled sources only carry delayTime; the UI slider cap in the Java layer was not recovered)",
	}
}

// SPDX-License-Identifier: GPL-2.0-only
package main

type tubeParams struct{}

var currentTube *tubeParams

func tubeAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().BiquadAvailable()
}

func setTube(p *tubeParams) error {
	if p == nil {
		currentTube = nil
		markStateDirty()
		return nil
	}
	if !tubeAvailable() {
		return errDSPUnavailable
	}
	currentTube = p
	markStateDirty()
	return nil
}

func tubeView() any {
	if currentTube == nil {
		return nil
	}
	return map[string]any{"on": true, "coef": [5]int32{tubeB0, 0, 0, tubeA1, 0}}
}

const (
	tubeB0 = 16384
	tubeA1 = -16384
)

func tubeNodes() []planNode {
	return []planNode{{
		Kind:  planKindBiquad,
		Coefs: [5]int32{tubeB0, 0, 0, tubeA1, 0},
	}}
}

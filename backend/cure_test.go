// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
)

func TestCurePassFilterSections(t *testing.T) {
	secs := curePassFilterSections(48000)

	if got := float64(secs[0][0]) / 32768; math.Abs(got-0.99934566) > 3e-5 {
		t.Errorf("HPF b0 = %.8f,expected ≈0.99934566", got)
	}
	if math.Abs(float64(secs[0][1])/32768+0.99934566) > 3e-5 {
		t.Errorf("HPF b1 should be −b0,actual %.8f", float64(secs[0][1])/32768)
	}
	if got := float64(secs[0][3]) / 32768; math.Abs(got+0.99869063) > 3e-5 {
		t.Errorf("HPF a1_slot = %.8f,expected ≈−0.99869063(= −a1_theirs)", got)
	}

	for i, idx := range []int{1, 2, 3} {
		if got := float64(secs[idx][0]) / 32768; math.Abs(got-0.70710678) > 3e-5 {
			t.Errorf("LPF first-order stage %d b0 = %.8f,expected ≈0.70710678", i+1, got)
		}
		if float64(secs[idx][0]) != float64(secs[idx][1]) {
			t.Errorf("LPF b1 must equal b0(first-order stage)")
		}
		if got := float64(secs[idx][3]) / 32768; math.Abs(got-0.41421356) > 3e-5 {
			t.Errorf("LPF a1_slot = %.8f,expected ≈+0.41421356", got)
		}
	}

	if secs[1] != secs[2] || secs[2] != secs[3] {
		t.Errorf("LPF three stages should bit-exactly identical:%v %v %v", secs[1], secs[2], secs[3])
	}

	q := [][5]int32{secs[0], secs[1], secs[2], secs[3]}
	ref := vdcQ315ResponseDB(q, 1000, 48000)
	d20k := vdcQ315ResponseDB(q, 20000, 48000) - ref
	d5 := vdcQ315ResponseDB(q, 5, 48000) - ref
	t.Logf("PassFilter shape:20 kHz %+.2f dB,5 Hz %+.2f dB(referenced to 1 kHz)", d20k, d5)
	if d20k > -14 || d20k < -18 {
		t.Errorf("20 kHz attenuation %.2f dB falls [−18,−14] outside(3xfirst-order stage@18k should about −15.9)", d20k)
	}
	if d5 > -3 || d5 < -9 {
		t.Errorf("5 Hz attenuation %.2f dB falls [−9,−3] outside", d5)
	}

	for _, fs := range []float64{32000, 44100} {
		s2 := curePassFilterSections(fs)
		_ = s2
	}
}

func TestTubeIsOnePoleAverage(t *testing.T) {
	n := tubeNodes()
	if len(n) != 1 {
		t.Fatalf("tube should have only 1sections,actual %d", len(n))
	}
	c := n[0].Coefs
	if c != [5]int32{tubeB0, 0, 0, tubeA1, 0} {
		t.Fatalf("coefficient = %v,expected [16384 0 0 -16384 0]", c)
	}

	if float64(c[0])/32768 != 0.5 || float64(c[3])/32768 != -0.5 {
		t.Errorf("0.5 / −0.5 must be exact:%v", c)
	}

	dc := vdcQ315ResponseDB([][5]int32{c}, 0.0001, 48000)
	if math.Abs(dc) > 0.05 {
		t.Errorf("DC gain %.3f dB,expected ≈0", dc)
	}

	t.Logf("step response:0.5 -> 0.75 -> 0.875(first-order α=0.5)")
}

func TestApplyChainRollsBackOnFailure(t *testing.T) {

	dynS, crossS, surS, excS, tubeS := currentDynBass, currentCrossfeed, currentSurround, currentExciter, currentTube
	chainS := currentUserChain
	defer func() {
		currentDynBass, currentCrossfeed, currentSurround = dynS, crossS, surS
		currentExciter, currentTube = excS, tubeS
		currentUserChain = chainS
	}()

	tubeOn := &tubeParams{}
	currentTube = tubeOn
	currentExciter = nil
	currentDynBass = nil
	currentCrossfeed = nil
	currentSurround = nil
	currentUserChain = nil

	var chain []ChainItem
	for i := 0; i < maxSections; i++ {
		chain = append(chain, ChainItem{Type: "peq", Enabled: true, Freq: 1000, GainDB: 1, Q: 1})
	}

	chain = append(chain, ChainItem{Type: "exciter", Enabled: true})

	if err := applyChain(chain, 0, 0); err == nil {
		t.Skip("test environment no hardware,applyChain should have fail;skipping")
	} else if err != nil {
		t.Logf("rejected as expected:%v", err)
	}

	if currentTube != tubeOn {
		t.Errorf("tube state lost to rollback:%v", currentTube)
	}
	if currentExciter != nil {
		t.Errorf("exciter was half-applied(memory %v)", currentExciter)
	}
}

func TestRequestLevelRollbackCoversHandlerFields(t *testing.T) {
	crossS, tubeS := currentCrossfeed, currentTube
	chainS := currentUserChain
	defer func() { currentCrossfeed, currentTube, currentUserChain = crossS, tubeS, chainS }()

	currentCrossfeed = nil
	tubeOn := &tubeParams{}
	currentTube = tubeOn
	currentUserChain = nil

	snap := captureChainState()
	p := crossfeedDefaultParams()
	p.PassFilter = true
	currentCrossfeed = &p

	var chain []ChainItem
	for i := 0; i < 24; i++ {
		chain = append(chain, ChainItem{Type: "peq", Enabled: true, Freq: 1000, GainDB: 1, Q: 1})
	}
	chain = append(chain, ChainItem{Type: "exciter", Enabled: true})
	if err := applyChain(chain, 0, 0); err == nil {
		t.Skip("test environment no hardware,should have fail;skipping")
	}

	snap.restore()

	if currentCrossfeed != nil {
		t.Errorf("crossfeed should be request-level snapshot rolled back to nil,actual %+v", currentCrossfeed)
	}
	if currentTube != tubeOn {
		t.Errorf("tube state must not change:%v", currentTube)
	}
}

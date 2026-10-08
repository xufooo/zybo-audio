// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

type antiPopWrite struct {
	idx int
	val int32
}

type antiPopTestRig struct {
	mu      sync.Mutex
	writes  []antiPopWrite
	slept   time.Duration
	nSleep  int
	nWrites int
	failAt  int
}

func (r *antiPopTestRig) record() []antiPopWrite {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]antiPopWrite(nil), r.writes...)
}

func (r *antiPopTestRig) stats() (nSleep int, slept time.Duration, nWrites int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.nSleep, r.slept, r.nWrites
}

func newAntiPopTestRig(t *testing.T, steps int) *antiPopTestRig {
	t.Helper()
	antiPopCancelRamp()

	r := &antiPopTestRig{}
	oldSteps, oldSleep, oldWrite := antiPopStepsN, antiPopSleeper, antiPopWriteActive
	oldWasOn := antiPopBassWasOn
	antiPopStepsN = steps
	antiPopSleeper = func(d time.Duration) {
		r.mu.Lock()
		r.slept += d
		r.nSleep++
		r.mu.Unlock()
	}
	antiPopWriteActive = func(idx int, v int32) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.nWrites++
		if r.failAt > 0 && r.nWrites == r.failAt {
			return fmt.Errorf("injected failed to write(attempt %d)", r.failAt)
		}
		r.writes = append(r.writes, antiPopWrite{idx, v})
		return nil
	}
	antiPopBassWasOn = false

	t.Cleanup(func() {
		antiPopCancelRamp()
		antiPopStepsN, antiPopSleeper, antiPopWriteActive = oldSteps, oldSleep, oldWrite
		antiPopBassWasOn = oldWasOn
	})
	return r
}

func antiPopWaitIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		antiPopMu.Lock()
		busy := antiPopActive != nil
		antiPopMu.Unlock()
		if !busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("antiPop ramp did not finish within 2 s (background goroutine stuck?)")
}

func TestAntiPopScaleShape(t *testing.T) {
	const steps = 200
	const v = 32768

	if got := antiPopScale(v, 0, steps); got != 0 {
		t.Errorf("step 0should be exact 0(official first frame times 0),actual %d", got)
	}
	if got := antiPopScale(v, steps, steps); got != v {
		t.Errorf("final step should equal compiled value %d bit-exactly, actual %d", v, got)
	}
	if got := antiPopScale(v, 1, 2); got != v/2 {
		t.Errorf("midpoint = %d,expected %d", got, v/2)
	}

	prev := int32(-1)
	for k := 0; k <= steps; k++ {
		got := antiPopScale(v, k, steps)
		if got < prev {
			t.Fatalf("step %dwent backward :%d < previous step %d", k, got, prev)
		}
		if got < 0 || got > v {
			t.Fatalf("step %d %d exceeds [0,%d]", k, got, v)
		}
		prev = got
	}

	step := antiPopScale(v, 1, steps)
	if step <= 0 {
		t.Fatalf("per-step increment should be positive,actual %d(%d too few steps,ramp would quantize into silence)", step, steps)
	}
}

func TestAntiPopRampStepsDurationAndEndpoints(t *testing.T) {
	const steps = 40
	const c0, c1 = 32768, 16384
	rig := newAntiPopTestRig(t, steps)

	if err := startAntiPopRamp(antiPopRamp{coefIdx: 37, c0: c0, c1: c1}); err != nil {
		t.Fatalf("start ramp:%v", err)
	}
	antiPopWaitIdle(t)

	nSleep, slept, nWrites := rig.stats()
	if nSleep != steps {
		t.Errorf("advanced %d steps, expected %d steps (1 s / 5 ms per step convention)", nSleep, steps)
	}
	if slept != time.Second {
		t.Errorf("total duration = %v,expected exactly %v(antiPopSeconds=1)", slept, time.Second)
	}
	if want := 2 * (steps + 1); nWrites != want {
		t.Errorf("register writes = %d,expected %d(initial 2 times + per step 2 times)", nWrites, want)
	}

	ws := rig.record()
	if len(ws) != 2*(steps+1) {
		t.Fatalf("recorded writes = %d, expected %d", len(ws), 2*(steps+1))
	}

	if ws[0] != (antiPopWrite{37, 0}) || ws[1] != (antiPopWrite{38, 0}) {
		t.Errorf("start should be (37,0),(38,0),actual %v %v", ws[0], ws[1])
	}
	if last := ws[len(ws)-2]; last != (antiPopWrite{37, c0}) {
		t.Errorf("end c0 = %v,expected (37,%d) -- end must restore bit-exactly", last, c0)
	}
	if last := ws[len(ws)-1]; last != (antiPopWrite{38, c1}) {
		t.Errorf("end c1 = %v,expected (38,%d)", last, c1)
	}

	for k := 0; k <= steps; k++ {
		got0 := ws[2*k]
		got1 := ws[2*k+1]
		if got0.idx != 37 || got1.idx != 38 {
			t.Fatalf("step %dwrote to wrong coefficients on:%v %v", k, got0, got1)
		}
		if got0.val != antiPopScale(c0, k, steps) || got1.val != antiPopScale(c1, k, steps) {
			t.Fatalf("step %d = (%d,%d),expected (%d,%d)", k, got0.val, got1.val,
				antiPopScale(c0, k, steps), antiPopScale(c1, k, steps))
		}
	}

	seen := map[int]bool{}
	for _, w := range ws {
		seen[w.idx] = true
	}
	if len(seen) != 2 || !seen[37] || !seen[38] {
		t.Errorf("ramp touched coefficient index = %v,expected only {37,38}", seen)
	}
}

func TestAntiPopRampMidwayFailureRollsBack(t *testing.T) {
	const steps = 40
	const c0, c1 = 32768, 16384
	rig := newAntiPopTestRig(t, steps)
	rig.failAt = 5

	if err := startAntiPopRamp(antiPopRamp{coefIdx: 37, c0: c0, c1: c1}); err != nil {
		t.Fatalf("start ramp:%v", err)
	}
	antiPopWaitIdle(t)

	nSleep, _, _ := rig.stats()
	if nSleep >= steps {
		t.Errorf("after failure should not advance further:only slept %d steps(of %d steps)", nSleep, steps)
	}
	ws := rig.record()
	if len(ws) < 2 {
		t.Fatalf("recorded writes = %d, too few", len(ws))
	}

	if last := ws[len(ws)-2]; last != (antiPopWrite{37, c0}) {
		t.Errorf("after failure final wrote is %v,expected restore to (37,%d)", last, c0)
	}
	if last := ws[len(ws)-1]; last != (antiPopWrite{38, c1}) {
		t.Errorf("after failure final wrote is %v,expected restore to (38,%d)", last, c1)
	}
	antiPopMu.Lock()
	left := antiPopActive
	antiPopMu.Unlock()
	if left != nil {
		t.Errorf("a live ramp %+v is still left after failure (it would race the next full-table dispatch for coefficients)", left)
	}
}

func TestAntiPopStartFailureRollsBack(t *testing.T) {
	const steps = 40
	const c0, c1 = 32768, 16384
	rig := newAntiPopTestRig(t, steps)
	rig.failAt = 2

	err := startAntiPopRamp(antiPopRamp{coefIdx: 37, c0: c0, c1: c1})
	if err == nil {
		t.Fatal("when the initial write fails, startAntiPopRamp should fail")
	}
	antiPopWaitIdle(t)

	ws := rig.record()

	if _, _, nWrites := rig.stats(); nWrites != 4 {
		t.Errorf("write attempts = %d,expected 4(initial 2 times + restore 2 times)", nWrites)
	}
	if len(ws) != 3 {
		t.Fatalf("successful persisted writes = %d, expected 3 (only item 1 of the initial pair made it)", len(ws))
	}
	if ws[0] != (antiPopWrite{37, 0}) {
		t.Errorf("initial attempt one wrote = %v,expected (37,0)", ws[0])
	}
	if last := ws[len(ws)-2]; last != (antiPopWrite{37, c0}) {
		t.Errorf("after failed start, last write is %v, expected restore to (37,%d) (must not leave dry gone while wet remains)", last, c0)
	}
	if last := ws[len(ws)-1]; last != (antiPopWrite{38, c1}) {
		t.Errorf("after failed start final wrote is %v,expected restore to (38,%d)", last, c1)
	}
	antiPopMu.Lock()
	left := antiPopActive
	antiPopMu.Unlock()
	if left != nil {
		t.Errorf("after failed start should not leave live ramp %+v", left)
	}
}

func TestAntiPopCancelRestoresAndStops(t *testing.T) {
	const steps = 40
	const c0, c1 = 32768, 16384
	rig := newAntiPopTestRig(t, steps)

	if err := startAntiPopRamp(antiPopRamp{coefIdx: 37, c0: c0, c1: c1}); err != nil {
		t.Fatalf("start ramp:%v", err)
	}
	antiPopCancelRamp()
	ws := rig.record()
	if len(ws) < 4 {
		t.Fatalf("on cancel one restoring write should already be issued, %d writes recorded", len(ws))
	}
	if last := ws[len(ws)-2]; last != (antiPopWrite{37, c0}) {
		t.Errorf("on cancel final wrote is %v,expected restore to (37,%d)", last, c0)
	}
	if last := ws[len(ws)-1]; last != (antiPopWrite{38, c1}) {
		t.Errorf("on cancel final wrote is %v,expected restore to (38,%d)", last, c1)
	}

	before := len(rig.record())
	antiPopWaitIdle(t)
	time.Sleep(20 * time.Millisecond)
	if after := len(rig.record()); after != before {
		t.Errorf("after cancel the ramp wrote %d more times (must be 0)-- those would land in the newly dispatched table", after-before)
	}
}

func TestAntiPopTriggerOnlyOnBassOffToOn(t *testing.T) {
	if antiPopShouldStart(true, true) {
		t.Error("on->on(steady state)should not start a ramp")
	}
	if antiPopShouldStart(false, false) || antiPopShouldStart(true, false) {
		t.Error("no ViPERBass table should not start a ramp")
	}
	if !antiPopShouldStart(false, true) {
		t.Error("off->on must start ramp")
	}

	defer func() { antiPopBassWasOn = false }()
	antiPopBassWasOn = false
	seq := []struct {
		hasBass   bool
		wantStart bool
		why       string
	}{
		{false, false, "off->off"},
		{true, true, "off->on: should start"},
		{true, false, "on->on(steady-state dispatch):once should not start"},
		{true, false, "on->on: same as above"},
		{false, false, "on->off"},
		{false, false, "off->off"},
		{true, true, "re-enable: official SetEnable(true) resets every time => start another ramp"},
		{true, false, "steady state"},
	}
	for i, s := range seq {
		if got := antiPopNotePlan(s.hasBass); got != s.wantStart {
			t.Errorf("attempt %ddispatch(%s)start ramp = %v,expected %v", i, s.why, got, s.wantStart)
		}
	}
}

func TestAntiPopBassMixIndexPointsAtMixSlot(t *testing.T) {
	for _, tc := range []struct {
		name       string
		p          viperBassParams
		wantSlots  int
		wantSecs   int
		mixSlotIdx int
	}{
		{"NATURAL", viperBassParams{Mode: viperBassModeNatural, CutoffHz: 60, Gain: 50}, 2, 3, 1},
		{"PBP", viperBassParams{Mode: viperBassModePBP, CutoffHz: 60, Gain: 50}, 4, 3, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {

			p := pbpPlan(t, tc.p)

			if p.Slots != tc.wantSlots || p.Sections != tc.wantSecs {
				t.Errorf("slots/sections = %d/%d,expected %d/%d -- antiPop should not change chain size",
					p.Slots, p.Sections, tc.wantSlots, tc.wantSecs)
			}
			if !p.HasBassMix {
				t.Fatal("carry ViPERBass table must report HasBassMix(otherwise antiPop not found should which two words to patch)")
			}
			if p.BassMixCoef < 0 || p.BassMixCoef+1 >= len(p.Coefs) {
				t.Fatalf("BassMixCoef = %d out of range(Coefs has %d)", p.BassMixCoef, len(p.Coefs))
			}

			slots := decodeSlots(t, p)
			if got := slots[tc.mixSlotIdx].Op; got != opMix2 {
				t.Errorf("slot %d opcode = %d,expected %d(MIX2)", tc.mixSlotIdx, got, opMix2)
			}
			if got := slots[tc.mixSlotIdx].Cfb; got != p.BassMixCoef {
				t.Errorf("slot %d coefficient base = %d,and BassMixCoef = %d -- both must equal",
					tc.mixSlotIdx, got, p.BassMixCoef)
			}

			if p.Coefs[p.BassMixCoef+2] != 0 || p.Coefs[p.BassMixCoef+3] != 0 || p.Coefs[p.BassMixCoef+4] != 0 {
				t.Errorf("MIX2 slots coefficients 3..5must be 0,actual %v",
					p.Coefs[p.BassMixCoef+2:p.BassMixCoef+5])
			}
		})
	}
}

func TestAntiPopSteadyStateEqualsCompiledCoefs(t *testing.T) {
	rig := newAntiPopTestRig(t, 50)

	p, err := buildSlotPlanNodes(mustBassNodes(t, viperBassParams{
		Mode: viperBassModeNatural, CutoffHz: 60, Gain: 50,
	}))
	if err != nil {
		t.Fatalf("buildSlotPlanNodes：%v", err)
	}
	if err := dspAntiPopAfterDownload(p); err != nil {
		t.Fatalf("dspAntiPopAfterDownload：%v", err)
	}
	antiPopWaitIdle(t)

	ws := rig.record()
	if len(ws) < 2 {
		t.Fatalf("ramp wrote nothing(%d)", len(ws))
	}
	if last0, last1 := ws[len(ws)-2], ws[len(ws)-1]; last0.val != p.Coefs[p.BassMixCoef] || last1.val != p.Coefs[p.BassMixCoef+1] {
		t.Errorf("ramp end = (%d,%d),and compiled values = (%d,%d) -- steady state must bit-exactly equal",
			last0.val, last1.val, p.Coefs[p.BassMixCoef], p.Coefs[p.BassMixCoef+1])
	}
	if last0 := ws[len(ws)-2]; last0.idx != p.BassMixCoef {
		t.Errorf("ramp end landed at index %d,expected %d", last0.idx, p.BassMixCoef)
	}

	if got := p.Coefs[p.BassMixCoef]; got != q315Round(1.0) {
		t.Errorf("dry weight = %d,expected %d(Q3.15 1.0)", got, q315Round(1.0))
	}
	if got, want := p.Coefs[p.BassMixCoef+1], q315Round(0.5); got != want {
		t.Errorf("wet weight = %d,expected %d(gain 50 => 0.5)", got, want)
	}
}

func TestAntiPopSteadyStateDoesNotWrite(t *testing.T) {
	rig := newAntiPopTestRig(t, 50)
	p, err := buildSlotPlanNodes(mustBassNodes(t, viperBassDefaultParams()))
	if err != nil {
		t.Fatalf("buildSlotPlanNodes：%v", err)
	}
	if err := dspAntiPopAfterDownload(p); err != nil {
		t.Fatalf("attempt one:%v", err)
	}
	antiPopWaitIdle(t)
	before := len(rig.record())

	for i := 0; i < 5; i++ {
		if err := dspAntiPopAfterDownload(p); err != nil {
			t.Fatalf("attempt %dsteady-state dispatch:%v", i, err)
		}
	}
	antiPopWaitIdle(t)
	if after := len(rig.record()); after != before {
		t.Errorf("steady-state dispatch wrote %d more registers (must be 0)-- steady state must never be touched by the ramp", after-before)
	}
}

func TestAntiPopNoBassNoRamp(t *testing.T) {
	rig := newAntiPopTestRig(t, 50)
	p, err := buildSlotPlanNodes([]planNode{{Kind: planKindBiquad, Coefs: [5]int32{32768, 0, 0, 0, 0}}})
	if err != nil {
		t.Fatalf("buildSlotPlanNodes：%v", err)
	}
	if p.HasBassMix {
		t.Fatal("no ViPERBass table should not report HasBassMix")
	}
	if err := dspAntiPopAfterDownload(p); err != nil {
		t.Fatalf("off should not fail:%v", err)
	}
	antiPopWaitIdle(t)
	if n := len(rig.record()); n != 0 {
		t.Errorf("off wrote %dregister,expected 0", n)
	}

	if err := dspAntiPopAfterDownload(mustPlanWithBass(t)); err != nil {
		t.Fatalf("off->on:%v", err)
	}
	antiPopWaitIdle(t)
	if n := len(rig.record()); n == 0 {
		t.Error("off->on after never wrote -- ramp never started")
	}
}

func mustBassNodes(t *testing.T, p viperBassParams) []planNode {
	t.Helper()
	n, err := viperBassNodes(p)
	if err != nil {
		t.Fatalf("viperBassNodes(%+v)：%v", p, err)
	}
	return n
}

func mustPlanWithBass(t *testing.T) slotPlan {
	t.Helper()
	p, err := buildSlotPlanNodes(mustBassNodes(t, viperBassDefaultParams()))
	if err != nil {
		t.Fatalf("buildSlotPlanNodes：%v", err)
	}
	return p
}

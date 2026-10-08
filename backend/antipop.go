// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

const (
	antiPopSeconds = 1.0

	antiPopSteps = 200
)

type antiPopRamp struct {
	coefIdx int
	c0, c1  int32
}

var (
	antiPopMu sync.Mutex

	antiPopActive *antiPopRamp
	antiPopDone   chan struct{}

	antiPopBassWasOn bool

	antiPopStepsN      = antiPopSteps
	antiPopSleeper     = time.Sleep
	antiPopWriteActive = dspWriteActiveCoef
)

func antiPopScale(v int32, k, steps int) int32 {
	if k <= 0 {
		return 0
	}
	if k >= steps {
		return v
	}
	return int32(math.Round(float64(v) * float64(k) / float64(steps)))
}

func antiPopWritePair(r antiPopRamp, c0, c1 int32) error {
	if err := antiPopWriteActive(r.coefIdx, c0); err != nil {
		return err
	}
	if err := antiPopWriteActive(r.coefIdx+1, c1); err != nil {
		return err
	}
	return nil
}

func antiPopShouldStart(prevHadBass, nowHasBass bool) bool {
	return nowHasBass && !prevHadBass
}

func antiPopNotePlan(hasBass bool) bool {
	start := antiPopShouldStart(antiPopBassWasOn, hasBass)
	antiPopBassWasOn = hasBass
	return start
}

func antiPopCancelRamp() {
	antiPopMu.Lock()
	defer antiPopMu.Unlock()
	if antiPopDone != nil {
		close(antiPopDone)
		antiPopDone = nil
	}
	if antiPopActive != nil {
		r := *antiPopActive
		antiPopActive = nil
		if err := antiPopWritePair(r, r.c0, r.c1); err != nil {
			log.Printf("ViPERBass antiPop failed to restore coefficients on cancel (next full-table download will overwrite): %v", err)
		}
	}
}

func startAntiPopRamp(r antiPopRamp) error {
	antiPopCancelRamp()

	antiPopMu.Lock()
	defer antiPopMu.Unlock()
	if err := antiPopWritePair(r, 0, 0); err != nil {

		_ = antiPopWritePair(r, r.c0, r.c1)
		return fmt.Errorf("ViPERBass antiPop start (zeroing gain) failed: %w", err)
	}
	done := make(chan struct{})
	antiPopActive, antiPopDone = &r, done
	go antiPopRun(r, done)
	return nil
}

func antiPopRun(r antiPopRamp, done chan struct{}) {
	steps := antiPopStepsN
	if steps < 1 {
		steps = 1
	}
	interval := time.Duration(float64(antiPopSeconds) * float64(time.Second) / float64(steps))
	for k := 1; k <= steps; k++ {
		antiPopSleeper(interval)
		antiPopMu.Lock()
		select {
		case <-done:

			antiPopMu.Unlock()
			return
		default:
		}
		err := antiPopWritePair(r, antiPopScale(r.c0, k, steps), antiPopScale(r.c1, k, steps))
		if err != nil {
			_ = antiPopWritePair(r, r.c0, r.c1)
			antiPopActive, antiPopDone = nil, nil
			antiPopMu.Unlock()
			log.Printf("ViPERBass antiPop ramp failed at step %d/%d, gain restored to compiled values: %v",
				k, steps, err)
			return
		}
		if k == steps {
			antiPopActive, antiPopDone = nil, nil
		}
		antiPopMu.Unlock()
	}
}

func dspAntiPopAfterDownload(p slotPlan) error {
	if !antiPopNotePlan(p.HasBassMix) {
		return nil
	}
	if p.BassMixCoef < 0 || p.BassMixCoef+1 >= len(p.Coefs) {
		return fmt.Errorf("ViPERBass antiPop: compiler-reported MIX2 coefficient index %d out of range (only %d Coefs)"+
			"-- ramp not downloaded (better to keep the one click than to patch a guessed address)",
			p.BassMixCoef, len(p.Coefs))
	}
	return startAntiPopRamp(antiPopRamp{
		coefIdx: p.BassMixCoef,
		c0:      p.Coefs[p.BassMixCoef],
		c1:      p.Coefs[p.BassMixCoef+1],
	})
}

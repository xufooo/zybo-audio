// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"testing"
)

func TestSurroundMaxFollowsRingDepth(t *testing.T) {
	saveDly, saveCur := hwDelayWords, currentSurround
	defer func() { hwDelayWords, currentSurround = saveDly, saveCur }()

	hwDelayWords = 8192
	if got := surroundMaxMs(); math.Abs(got-170.67) > 0.1 {
		t.Errorf("With an 8192-word ring the cap should be 170.7 ms, got %.2f", got)
	}

	got := clampSurroundMs(surroundParams{DelayMs: 100})
	if math.Abs(got.DelayMs-100) > 0.01 {
		t.Errorf("100 ms got clamped to %v -- is the cap hardcoded again?", got.DelayMs)
	}
	if n := surroundDelaySamples(got); n != 4800 {
		t.Errorf("100 ms should convert to 4800 samples, got %d", n)
	}

	hwDelayWords = 256
	if got := surroundMaxMs(); math.Abs(got-5.33) > 0.05 {
		t.Errorf("With a 256-word ring the cap should be 5.33 ms, got %.2f", got)
	}
	if got := clampSurroundMs(surroundParams{DelayMs: 100}); got.DelayMs > 5.4 {
		t.Errorf("With only a 256-word ring 100 ms should clamp to <=5.33, got %v", got.DelayMs)
	}
}

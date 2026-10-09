// SPDX-License-Identifier: GPL-2.0-only
package main

import "testing"

func TestCoefReadbackReachesWholeBank(t *testing.T) {
	fakeHardware(t, 0xFFFFFFFF)
	saved := hwCoefWords
	hwCoefWords = coefWordsPerBank
	t.Cleanup(func() { hwCoefWords = saved })

	const sentinel = uint32(28737)
	regWrite(regCdat, sentinel)
	for _, idx := range []int{199, 200, 239} {
		if got := dspReadCoeff(idx); got != int32(sentinel) {
			t.Errorf("readback %d must hit hardware (whole bank addressable), got %d (old guard returned 0 directly)",
				idx, got)
		}
	}

	if got := dspReadCoeff(240); got != 0 {
		t.Errorf("readback 240 (out of range) must return 0, got %d", got)
	}
	if got := dspReadCoeff(-1); got != 0 {
		t.Errorf("readback -1 (out of range) must return 0, got %d", got)
	}
}
